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
	"path/filepath"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/compat/pkg/constants"
	"github.com/GoogleCloudPlatform/compat/pkg/server/errorutil"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"knative.dev/pkg/apis"
)

var (
	boolTrue         = true
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
)

// ToTaskRun returns the on-cluster representation of the given Build proto message,
// or errorsutil.Invalid if the build is not compatible with on-cluster execution.
func ToTaskRun(b *gcb.Build) (*v1alpha1.TaskRun, error) {
	if len(b.Secrets) != 0 {
		return nil, errorutil.Invalid("Incompatible build: .secrets is not supported")
	}
	out := &v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.Id,
			Namespace: constants.Namespace,
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

	// Prepend init step to set up Docker environment.
	initStep := v1alpha1.Step{
		Container: corev1.Container{
			Image:        "docker",
			Env:          dockerClientEnvs,
			VolumeMounts: dockerTLSCertsVolumeMounts,
		},
		// Create a Docker volume to store home directory.
		Script: `#!/bin/sh
# Wait for the sidecar's generated TLS certs to be generated and shared.
while [ ! -f /certs/client/ca.pem ]; do sleep 1; done
set -euxo pipefail
docker volume create home
`,
	}
	// Initialize volumes for all steps.
	seenVols := map[string]struct{}{}
	for _, s := range b.Steps {
		for _, v := range s.Volumes {
			if _, found := seenVols[v.Name]; !found {
				seenVols[v.Name] = struct{}{}
				initStep.Script += fmt.Sprintf("docker volume create %q\n", v.Name)
			}
		}
	}
	// TODO: initialize the cloudbuild network.
	// TODO: initialize source fetch and collect hashes and timing.
	out.Spec.TaskSpec.Steps = []v1alpha1.Step{initStep}

	for idx, s := range b.Steps {
		// These features are not supported.
		if len(s.WaitFor) != 0 || len(s.SecretEnv) != 0 || s.Timeout != "" {
			return nil, errorutil.Invalid("Incompatible build: step %d cannot specify waitFor, secretEnv or timeout", idx)
		}

		gs := toStep(s)
		gs.Resources = resources
		out.Spec.TaskSpec.Steps = append(out.Spec.TaskSpec.Steps, *gs)
	}

	out.Spec.TaskSpec.Volumes = dockerTLSCertsVolumes

	out.Spec.TaskSpec.Sidecars = []corev1.Container{dindSidecar}

	return out, nil
}

// ToBuild returns the proto representation of a build in the on-cluster
// representation, or an error if conversion failed.
func ToBuild(tr v1alpha1.TaskRun) (out *gcb.Build, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("error converting TaskRun: %v", r)
		}
	}()
	out = &gcb.Build{
		Id:         tr.ObjectMeta.Name,
		ProjectId:  constants.ProjectID,
		LogsBucket: fmt.Sprintf("gs://%s", constants.LogsBucket()),
	}

	if tr.Spec.TaskSpec == nil {
		return nil, errorutil.Invalid("Incompatible taskRun: .spec.taskSpec is required")
	}

	hasStatus := len(tr.Status.Steps) > 0
	if steps, statuses := len(tr.Spec.TaskSpec.Steps), len(tr.Status.Steps); hasStatus && steps != statuses {
		return nil, fmt.Errorf("mismatched lengths: steps=%d, statuses=%d", steps, statuses)
	}

	for i, s := range tr.Spec.TaskSpec.Steps {
		if i == 0 {
			continue
		}
		ts, err := fromStep(&s)
		if err != nil {
			return nil, fmt.Errorf("error converting step %d: %v", i, err)
		}
		out.Steps = append(out.Steps, ts)

		// Add step status details.
		// If the TaskRun doesn't have a status yet (e.g., it was just
		// created, or the Pod hasn't started yet), just continue.
		if !hasStatus {
			continue
		}
		state := tr.Status.Steps[i]
		if term := state.Terminated; term != nil {
			if term.ExitCode == 0 {
				ts.Status = SUCCESS
			} else {
				ts.Status = FAILURE
			}

			ts.Timing = &gcb.TimeSpan{
				StartTime: term.StartedAt.Time.Format(time.RFC3339),
				EndTime:   term.FinishedAt.Time.Format(time.RFC3339),
			}
		} else if run := state.Running; run != nil {
			ts.Status = WORKING
			ts.Timing = &gcb.TimeSpan{
				StartTime: run.StartedAt.Time.Format(time.RFC3339),
			}
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
		out.Status = FAILURE
	case cond.Status == corev1.ConditionTrue:
		out.Status = SUCCESS
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

	if podName := tr.Status.PodName; podName != "" {
		out.LogUrl = fmt.Sprintf(logURLFmt, constants.ProjectID, constants.ProjectID, podName)
	}

	return out, nil
}

const (
	logURLFmt = `https://console.cloud.google.com/logs/viewer?project=%s&advancedFilter=resource.type%%3D"container"%%0Aresource.labels.namespace_id%%3D"gcb-compat"%%0Aresource.labels.project_id%%3D"%s"%%0Aresource.labels.pod_id%%3D"%s"`
	WORKING   = "WORKING"
	SUCCESS   = "SUCCESS"
	FAILURE   = "FAILURE"
	QUEUED    = "QUEUED"
	CANCELLED = "CANCELLED"
)

var (
	dockerClientEnvs = []corev1.EnvVar{{
		// Connect to the Docker daemon over TCP, using TLS.
		Name:  "DOCKER_HOST",
		Value: "tcp://localhost:2376",
	}, {
		// Verify TLS.
		Name:  "DOCKER_TLS_VERIFY",
		Value: "1",
	}, {
		// Use the certs generated by the sidecar daemon.
		Name:  "DOCKER_CERT_PATH",
		Value: "/certs/client",
	}}
	dindSidecar = corev1.Container{
		Image:           "docker:dind",
		Name:            "dind-sidecar",
		SecurityContext: &corev1.SecurityContext{Privileged: pointer.BoolPtr(true)},
		Env: []corev1.EnvVar{{
			// Write generated certs to the path shared with the client.
			Name:  "DOCKER_TLS_CERTDIR",
			Value: "/certs",
		}},
		VolumeMounts: dockerTLSCertsVolumeMounts,
	}
	dockerTLSCertsVolumeMounts = []corev1.VolumeMount{{
		Name:      "dind-certs",
		MountPath: "/certs/client",
	}}
	dockerTLSCertsVolumes = []corev1.Volume{{
		Name:         "dind-certs",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
)

func toStep(s *gcb.BuildStep) *v1alpha1.Step {
	out := &v1alpha1.Step{
		Container: corev1.Container{
			Image:        "docker",
			Env:          dockerClientEnvs,
			VolumeMounts: dockerTLSCertsVolumeMounts,
		},
	}
	var b strings.Builder
	fmt.Fprintln(&b, "#!/bin/sh")
	if s.Id != "" {
		fmt.Fprintln(&b, "#id", s.Id)
	}

	// TODO: Pull image first, and collect digest and pull timing.

	// By default, mount /workspace to the "host's" (TaskRun Pod's)
	// /workspace directory, which is mounted across all steps.
	// /builder/home is backed by a Docker volume.
	fmt.Fprintln(&b, `docker run \
-v /workspace:/workspace \
-v home:/builder/home \`)

	if s.Dir != "" && filepath.IsAbs(s.Dir) {
		fmt.Fprintln(&b, "--workdir", s.Dir, `\`)
	} else {
		fmt.Fprintln(&b, "--workdir", filepath.Join("/workspace", s.Dir), `\`)
	}

	if s.Entrypoint != "" {
		fmt.Fprintln(&b, "--entrypoint", s.Entrypoint, `\`)
	}

	fmt.Fprintln(&b, "-e", "HOME=/builder/home", `\`) // By default, $HOME is /builder/home
	for _, e := range s.Env {
		fmt.Fprintln(&b, "-e", e, `\`)
	}

	for _, v := range s.Volumes {
		fmt.Fprintf(&b, "-v %s:%s \\\n", v.Name, v.Path)
	}

	// Image name and args.
	fmt.Fprintf(&b, s.Name)
	if len(s.Args) > 0 {
		fmt.Fprintln(&b, ` \`)
	}
	for idx, a := range s.Args {
		fmt.Fprint(&b, a)
		if idx != len(s.Args)-1 {
			fmt.Fprintln(&b, ` \`)
		}
	}
	out.Script = b.String()
	return out
}

func fromStep(s *v1alpha1.Step) (out *gcb.BuildStep, err error) {
	var l string
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("error parsing line %q: %v", l, r)
		}
	}()
	out = &gcb.BuildStep{}
	lines := strings.Split(s.Script, "\n")
	var idx int
L:
	for idx, l = range lines {
		switch {
		case strings.HasPrefix(l, "#id "):
			out.Id = strings.Split(l, " ")[1]
		case l == "#!/bin/sh" || strings.HasPrefix(l, "docker run "):
			continue
		case strings.HasPrefix(l, "-e "):
			e := strings.Split(l, " ")[1]
			if e == "HOME=/builder/home" {
				// Implicit env value.
				continue
			}
			out.Env = append(out.Env, e)
		case strings.HasPrefix(l, "-v "):
			v := strings.Split(strings.Split(l, " ")[1], ":")
			if v[0] == "/workspace" || v[0] == "home" {
				// Default volumes.
				continue
			}
			out.Volumes = append(out.Volumes, &gcb.Volume{
				Name: v[0],
				Path: v[1],
			})
		case strings.HasPrefix(l, "--workdir "):
			d := strings.Split(l, " ")[1]
			if d == "/workspace" {
				d = ""
			}
			if strings.HasPrefix(d, "/workspace/") {
				d = strings.TrimPrefix(d, "/workspace/")
			}
			out.Dir = d
		case strings.HasPrefix(l, "--entrypoint "):
			out.Entrypoint = strings.Split(l, " ")[1]
		default:
			out.Name = strings.Split(l, " ")[0]
			// The rest of the lines are going to be args.
			for _, l = range lines[idx+1:] {
				out.Args = append(out.Args, strings.Split(l, " ")[0])
			}
			break L
		}
	}
	if out.Name == "" {
		//return nil, errors.New("didn't parse an image name")
	}
	return out, nil
}
