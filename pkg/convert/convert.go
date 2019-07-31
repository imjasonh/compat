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
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

// ErrIncompatible is returned by ToTaskRun when the requested build is not
// compatible with on-cluster execution.
//
// TODO(jasonhall): If this error becomes user-facing, give details about why
// the build is incompatible with on-cluster execution.
var ErrIncompatible = errors.New("Build is incompatible with on-cluster execution")

// ToTaskRun returns the on-cluster representation of the given Build proto message,
// or ErrIncompatible if the build is not compatible with on-cluster execution.
func ToTaskRun(b *gcb.Build) (*v1alpha1.TaskRun, error) {
	if len(b.Secrets) != 0 {
		return nil, ErrIncompatible
	}
	out := &v1alpha1.TaskRun{
		Spec: v1alpha1.TaskRunSpec{
			TaskSpec: &v1alpha1.TaskSpec{},
		},
	}

	if b.Timeout != "" {
		d, err := time.ParseDuration(b.Timeout)
		if err != nil {
			return nil, err
		}
		out.Spec.Timeout = &metav1.Duration{d}
	}

	allVols := map[string]corev1.Volume{}

	for idx, s := range b.Steps {
		// These features are not supported.
		if len(s.WaitFor) != 0 || len(s.SecretEnv) != 0 || s.Timeout != "" {
			return nil, ErrIncompatible
		}

		// Env vars are specified as []EnvVar, instead of []string
		// where each value contains a "=" separator.
		var env []corev1.EnvVar
		for _, e := range s.Env {
			parts := strings.SplitN(e, "=", 2)
			env = append(env, corev1.EnvVar{
				Name:  parts[0],
				Value: parts[1],
			})
		}

		// Stuff entrypoint+args into command, and add an annotation
		// denoting any original entrypoint, so we can split them back
		// apart correctly.
		cmd := s.Args
		if s.Entrypoint != "" {
			cmd = append([]string{s.Entrypoint}, cmd...)
			out.Annotations[fmt.Sprintf("entrypoint-%d", idx)] = s.Entrypoint
		}

		var volMounts []corev1.VolumeMount
		for _, v := range s.Volumes {
			volMounts = append(volMounts, corev1.VolumeMount{
				Name:      v.Name,
				MountPath: v.Path,
			})

			if _, found := allVols[v.Name]; !found {
				allVols[v.Name] = corev1.Volume{
					Name: v.Name,
					// EmptyDir is a volume which is created as empty at the beginning of
					// the build and discarded at the end, just like GCB volumes today.
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}
			}
		}

		out.Spec.TaskSpec.Steps = append(out.Spec.TaskSpec.Steps, corev1.Container{
			Image:        s.Name,
			Name:         s.Id,
			WorkingDir:   s.Dir,
			Command:      cmd,
			Env:          env,
			VolumeMounts: volMounts,
		})
	}

	// Specify all the volumes used by all steps.
	for _, vol := range allVols {
		out.Spec.TaskSpec.Volumes = append(out.Spec.TaskSpec.Volumes, vol)
	}
	// Sort volumes for reproducibility.
	sort.Slice(out.Spec.TaskSpec.Volumes, func(i, j int) bool { return out.Spec.TaskSpec.Volumes[i].Name < out.Spec.TaskSpec.Volumes[j].Name })

	if b.Source != nil {
		if b.Source.StorageSource == nil {
			return nil, ErrIncompatible
		}
		out.Spec.TaskSpec.Inputs = &v1alpha1.Inputs{
			Resources: []v1alpha1.TaskResource{{
				Name: "source",
				Type: "storage",
			}},
		}
		out.Spec.Inputs = v1alpha1.TaskRunInputs{
			Resources: []v1alpha1.TaskResourceBinding{{
				Name: "source",
				ResourceSpec: &v1alpha1.PipelineResourceSpec{
					Type: v1alpha1.PipelineResourceTypeStorage,
					Params: []v1alpha1.Param{{
						Name: "location",
						Value: fmt.Sprintf("gs://%s/%s#%d",
							b.Source.StorageSource.Bucket,
							b.Source.StorageSource.Object,
							b.Source.StorageSource.Generation),
					}, {
						Name:  "artifactType",
						Value: string(v1alpha1.GCSArchive),
					}, {
						Name:  "type",
						Value: "build-gcs",
					}},
				},
			}},
		}
	}

	return out, nil
}

// ToBuild returns the proto representation of a build in the on-cluster
// representation, or an error if conversion failed.
func ToBuild(tr v1alpha1.TaskRun) (*gcb.Build, error) {
	out := &gcb.Build{
		Id:      tr.ObjectMeta.Name,
		Results: &gcb.Results{},
	}
	if tr.Spec.TaskSpec == nil {
		return nil, ErrIncompatible
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
		if epa, found := tr.Annotations[fmt.Sprintf("entrypoint-%d", idx)]; found {
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

	cond := tr.Status.GetCondition(apis.ConditionSucceeded)
	switch {
	case cond == nil:
		out.Status = QUEUED
	case cond.Status == corev1.ConditionUnknown:
		out.Status = WORKING
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

	// TODO(jasonhall): build.Timing for FETCHSOURCE and BUILD (no PUSH)

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
	}

	return out, nil
}

const (
	WORKING = "WORKING"
	SUCCESS = "SUCCESS"
	FAILURE = "FAILURE"
	QUEUED  = "QUEUED"
)