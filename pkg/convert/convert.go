/*
Copyright 2019 Google, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package convert provides methods to translate GCB API request messages to
// Tekton TaskRun custom resource definitions.
package convert

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/compat/pkg/constants"
	"github.com/GoogleCloudPlatform/compat/pkg/server/errorutil"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

var (
	defaultResources = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("3.75Gi"),
	}
	resourceMapping = map[string]corev1.ResourceList{
		"N1_HIGHCPU_8": corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("8"),
			corev1.ResourceMemory: resource.MustParse("7.2Gi"),
		},
		"N1_HIGHCPU_32": corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("32"),
			corev1.ResourceMemory: resource.MustParse("28.8Gi"),
		},
	}

	// volumeMounts provides volume mounts to share a Docker socket with a Docker-in-Docker sidecar.
	volumeMounts = []corev1.VolumeMount{{
		Name:      "dind-storage",
		MountPath: "/var/lib/docker",
	}, {
		Name:      "dind-socker",
		MountPath: "var/run/",
	}}
)

// ToTaskRun returns the on-cluster representation of the given Build proto message,
// or errorsutil.Invalid if the build is not compatible with on-cluster execution.
func ToTaskRun(b *gcb.Build) (*v1alpha1.TaskRun, error) {
	if len(b.Secrets) != 0 {
		return nil, errorutil.Invalid("Incompatible build: .secrets is not supported")
	}
	out := &v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:        b.Id,
			Namespace:   constants.Namespace,
			Annotations: map[string]string{},
		},
		Spec: v1alpha1.TaskRunSpec{
			ServiceAccountName: constants.ServiceAccountName, // Run as the Workload Identity KSA/GSA
			TaskSpec:           &v1alpha1.TaskSpec{},
		},
	}

	if b.Timeout != "" {
		d, err := time.ParseDuration(b.Timeout)
		if err != nil {
			return nil, err
		}
		out.Spec.Timeout = &metav1.Duration{d}
	}

	resources := corev1.ResourceRequirements{Requests: defaultResources}
	if b.Options != nil {
		if b.Options.MachineType != "" {
			rq, found := resourceMapping[b.Options.MachineType]
			if !found {
				return nil, errorutil.Invalid("Incompatible build: .machineType %q is not supported", b.Options.MachineType)
			}
			resources = corev1.ResourceRequirements{Requests: rq}
		}
		if b.Options.DiskSizeGb != 0 {
			resources.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse(fmt.Sprintf("%dGi", b.Options.DiskSizeGb))
		}
	}

	for idx, s := range b.Steps {
		// These features are not supported.
		if len(s.WaitFor) != 0 || len(s.SecretEnv) != 0 || s.Timeout != "" {
			return nil, errorutil.Invalid("Incompatible build: step %d cannot specify waitFor, secretEnv or timeout", idx)
		}

		dockerRunArgs := []string{
			"run", // Invoke "docker run"

			// Remove the container when it's done. This probably
			// doesn't matter since the whole Docker daemon only
			// exists in a sidecar for the duration of the build,
			// but it's easy to do just in case.
			"--rm",

			// Mount the home volume and point $HOME to it. If a
			// user requests env vars over this, their request will
			// take precedence. This matches GCB behavior.
			"--volume", "homevol", "/builder/home",
			"--env", "HOME=/builder/home",
		}
		// Specify the user's requested environment variables.
		for _, e := range s.Env {
			dockerRunArgs = append(dockerRunArgs, "--env", e)
		}
		// Specify the user's requested entrypoint.
		if s.Entrypoint != "" {
			dockerRunArgs = append(dockerRunArgs, "--entrypoint", s.Entrypoint)
		}
		// Specify the user's requested working dir.
		if s.Dir != "" {
			var workingDir string
			if path.IsAbs(s.Dir) {
				workingDir = s.Dir
			} else if b.Source != nil {
				workingDir = path.Join("/workspace", "source", s.Dir)
			}
			dockerRunArgs = append(dockerRunArgs, "--workdir", workingDir) // TODO: include source dir?
		}
		// Mount the user's requested volumes. These will all be
		// ephemeral volumes, matching GCB's support.
		for _, v := range s.Volumes {
			dockerRunArgs = append(dockerRunArgs, "--volume", fmt.Sprintf("%s:%s", v.Name, v.Path))
		}
		// Unconditionally mount the Docker socket last, overriding
		// even if the user requested that mountPath.
		dockerRunArgs = append(dockerRunArgs, "--volume", "/var/run/docker:/var/run/docker")
		dockerRunArgs = append(dockerRunArgs, s.Name)    // Run the user's requested image.
		dockerRunArgs = append(dockerRunArgs, s.Args...) // Pass the user's requested args.

		// Run the user's "docker run" command, connected to the dind
		// sidecar by volumes.
		out.Spec.TaskSpec.Steps = append(out.Spec.TaskSpec.Steps, v1alpha1.Step{Container: corev1.Container{
			Image:        "docker",
			Name:         s.Id,
			Command:      []string{"docker"},
			Args:         dockerRunArgs,
			Resources:    resources,
			VolumeMounts: volumeMounts, // Mount the volumes shared with the Docker-in-Docker sidecar.
		}})
	}

	// Run a sidecar container to host an ephemeral Docker daemon.
	out.Spec.TaskSpec.Sidecars = []corev1.Container{{
		Name:         "docker:dind",
		VolumeMounts: volumeMounts,
	}}

	if b.Source != nil {
		if b.Source.StorageSource == nil {
			return nil, errorutil.Invalid("Incompatible build: only .source.storageSource is supported")
		}
		out.Spec.TaskSpec.Inputs = &v1alpha1.Inputs{
			Resources: []v1alpha1.TaskResource{{ResourceDeclaration: v1alpha1.ResourceDeclaration{
				Name: "source",
				Type: "storage",
			}}},
		}
		out.Spec.Inputs = v1alpha1.TaskRunInputs{
			Resources: []v1alpha1.TaskResourceBinding{{PipelineResourceBinding: v1alpha1.PipelineResourceBinding{
				Name: "source",
				ResourceSpec: &v1alpha1.PipelineResourceSpec{
					Type: v1alpha1.PipelineResourceTypeStorage,
					Params: []v1alpha1.ResourceParam{{
						Name: "location",
						Value: fmt.Sprintf("gs://%s/%s",
							b.Source.StorageSource.Bucket,
							b.Source.StorageSource.Object),
						// TODO: generation
					}, {
						Name:  "artifactType",
						Value: string(v1alpha1.GCSTarGzArchive),
					}, {
						Name:  "type",
						Value: "build-gcs",
					}},
				},
			}}},
		}
	}

	return out, nil
}

// ToBuild returns the proto representation of a build in the on-cluster
// representation, or an error if conversion failed.
func ToBuild(tr v1alpha1.TaskRun) (*gcb.Build, error) {
	out := &gcb.Build{
		Id:         tr.ObjectMeta.Name,
		ProjectId:  constants.ProjectID,
		Results:    &gcb.Results{},
		LogsBucket: fmt.Sprintf("gs://%s", constants.LogsBucket()),
	}

	if tr.Spec.TaskSpec == nil {
		return nil, errorutil.Invalid("Incompatible taskRun: .spec.taskSpec is required")
	}

	for idx, s := range tr.Spec.TaskSpec.Steps {
		var env []string
		for _, e := range s.Env {
			env = append(env, fmt.Sprintf("%s=%s", e.Name, e.Value))
		}
		var vols []*gcb.Volume
		for _, v := range s.VolumeMounts {
			vols = append(vols, &gcb.Volume{
				Name: v.Name,
				Path: v.MountPath,
			})
		}
		var ep string
		var args []string
		if epa, found := tr.Annotations[fmt.Sprintf("cloudbuild.googleapis.com/entrypoint-%d", idx)]; found {
			ep = epa
			args = s.Command[1:]
		} else {
			args = s.Command
		}

		out.Steps = append(out.Steps, &gcb.BuildStep{
			Name:       s.Image,
			Id:         s.Name,
			Env:        env,
			Args:       args,
			Entrypoint: ep,
			Dir:        s.WorkingDir,
			Volumes:    vols,
		})
	}

	if len(tr.Spec.Inputs.Resources) > 0 {
		if r := tr.Spec.Inputs.Resources[0].ResourceSpec; r != nil && r.Type == v1alpha1.PipelineResourceTypeStorage {
			parts := strings.Split(tr.Spec.Inputs.Resources[0].ResourceSpec.Params[0].Value, "/")
			bucket, object := parts[2], strings.Join(parts[3:], "/")
			var generation int64
			if strings.Contains(object, "#") {
				parts = strings.Split(object, "#")
				object = parts[0]
				generation, _ = strconv.ParseInt(parts[1], 10, 64)
			}
			out.Source = &gcb.Source{StorageSource: &gcb.StorageSource{
				Bucket:     bucket,
				Object:     object,
				Generation: generation,
			}}

		}

	}

	cond := tr.Status.GetCondition(apis.ConditionSucceeded)
	switch {
	case cond == nil:
		out.Status = QUEUED
	case cond.Status == corev1.ConditionUnknown:
		if cond.Reason == "ExceededNodeResources" {
			// TaskRun is queued due to insufficient cluster
			// resources. This corresponds to GCB's "QUEUED" state
			// when the build is being concurrency-capped.
			out.Status = QUEUED
		} else {
			out.Status = WORKING
		}
	case cond.Status == corev1.ConditionFalse:
		if _, found := tr.Annotations["cloudbuild.googleapis.com/logs-copied"]; found {
			out.Status = FAILURE
		} else {
			out.Status = WORKING
		}
	case cond.Status == corev1.ConditionTrue:
		if _, found := tr.Annotations["cloudbuild.googleapis.com/logs-copied"]; found {
			out.Status = SUCCESS
		} else {
			out.Status = WORKING
		}
	}
	if _, found := tr.Annotations["cloudbuild.googleapis.com/cancelled"]; found {
		out.Status = CANCELLED
	}

	if !tr.ObjectMeta.CreationTimestamp.IsZero() {
		out.CreateTime = tr.ObjectMeta.CreationTimestamp.Time.Format(time.RFC3339)
	}
	if !tr.Status.StartTime.IsZero() {
		out.StartTime = tr.Status.StartTime.Time.Format(time.RFC3339)
	}
	if !tr.Status.CompletionTime.IsZero() {
		out.FinishTime = tr.Status.CompletionTime.Time.Format(time.RFC3339)
	}

	out.Timing = map[string]gcb.TimeSpan{}
	if len(tr.Status.Steps) > len(tr.Spec.TaskSpec.Steps) {
		// Collect FETCHSOURCE timing.
		if len(tr.Status.Steps) > 2 &&
			strings.HasPrefix(tr.Status.Steps[1].ContainerName, "storage-fetch-source-") {
			ts := gcb.TimeSpan{}
			if term := tr.Status.Steps[1].Terminated; term != nil {
				ts.StartTime = term.StartedAt.Time.Format(time.RFC3339)
				ts.EndTime = term.FinishedAt.Time.Format(time.RFC3339)
			} else if run := tr.Status.Steps[1].Running; run != nil {
				ts.StartTime = run.StartedAt.Time.Format(time.RFC3339)
			}
			out.Timing["FETCHSOURCE"] = ts
		}
		tr.Status.Steps = tr.Status.Steps[2:]
	}

	for i, state := range tr.Status.Steps {
		if term := state.Terminated; term != nil {
			if term.ExitCode == 0 {
				out.Steps[i].Status = SUCCESS
			} else {
				out.Steps[i].Status = FAILURE
			}

			// TODO(jasonhall): Build step timeout? Cancelled?

			out.Steps[i].Timing = &gcb.TimeSpan{
				StartTime: term.StartedAt.Time.Format(time.RFC3339),
				EndTime:   term.FinishedAt.Time.Format(time.RFC3339),
			}
		} else if run := state.Running; run != nil {
			out.Steps[i].Status = WORKING
			out.Steps[i].Timing = &gcb.TimeSpan{
				StartTime: run.StartedAt.Time.Format(time.RFC3339),
			}
		}

		out.Results.BuildStepImages = append(out.Results.BuildStepImages, getImageDigest(state.ImageID))

	}

	if podName := tr.Status.PodName; podName != "" {
		out.LogUrl = fmt.Sprintf(logURLFmt, constants.ProjectID, constants.ProjectID, podName)
	}

	return out, nil
}

func getImageDigest(imageID string) string {
	if strings.HasPrefix(imageID, "docker-pullable://") &&
		strings.Contains(imageID, "@") {
		return imageID[strings.LastIndex(imageID, "@")+1:]
	}
	return ""
}

const (
	logURLFmt = `https://console.cloud.google.com/logs/viewer?project=%s&advancedFilter=resource.type%%3D"container"%%0Aresource.labels.namespace_id%%3D"gcb-compat"%%0Aresource.labels.project_id%%3D"%s"%%0Aresource.labels.pod_id%%3D"%s"`
	WORKING   = "WORKING"
	SUCCESS   = "SUCCESS"
	FAILURE   = "FAILURE"
	QUEUED    = "QUEUED"
	CANCELLED = "CANCELLED"
)
