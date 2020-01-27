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

package convert

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/compat/pkg/constants"
	"github.com/GoogleCloudPlatform/compat/pkg/server/errorutil"
	"github.com/google/go-cmp/cmp"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duck "knative.dev/pkg/apis/duck/v1beta1"
)

const buildID = "build-id"

func init() {
	constants.ProjectID = "project-id"
}

func TestIncompatibleToTaskRun(t *testing.T) {
	for _, b := range []*gcb.Build{{
		Id: "wait-for",
		Steps: []*gcb.BuildStep{{
			WaitFor: []string{"-"},
		}},
	}, {
		Id: "secret env",
		Steps: []*gcb.BuildStep{{
			SecretEnv: []string{"SEKRIT"},
		}},
	}, {
		Id: "step timeout",
		Steps: []*gcb.BuildStep{{
			Timeout: "specified",
		}},
	}, {
		Id: "secrets",
		Secrets: []*gcb.Secret{{
			KmsKeyName: "foo",
			SecretEnv:  map[string]string{"SEKRIT": "omgsekrit"},
		}},
	}, {
		Id:      "bad machine type",
		Options: &gcb.BuildOptions{MachineType: "NONSENSE"},
	}} {
		if _, err := ToTaskRun(b); err == nil {
			t.Errorf("ToTaskRun(%q): got nil, wanted error", b.Id)
		} else {
			if herr, ok := err.(*errorutil.HTTPError); !ok || herr.Code != http.StatusBadRequest {
				t.Errorf("ToTaskRun(%q): got %v, want errorutil.Invalid", b.Id, err)
			}
		}
	}
}

func TestToTaskRun(t *testing.T) {
	got, err := ToTaskRun(&gcb.Build{
		Id:      buildID,
		Timeout: time.Minute.String(),
		Steps: []*gcb.BuildStep{{
			Name:       "image",
			Args:       []string{"foo", "bar", "baz"},
			Env:        []string{"FOO=foo", "BAR=bar"},
			Entrypoint: "ep",
			Dir:        "dir",
			Id:         "id",
			Volumes:    []*gcb.Volume{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}},
		}},
	})
	if err != nil {
		t.Fatalf("ToTaskRun: %v", err)
	}

	want := &v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildID,
			Namespace: constants.Namespace,
		},
		Spec: v1alpha1.TaskRunSpec{
			ServiceAccountName: constants.ServiceAccountName,
			Timeout:            &metav1.Duration{time.Minute},
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []v1alpha1.Step{{
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
					},
					Script: `#!/bin/sh
# Wait for the sidecar's generated TLS certs to be generated and shared.
while [ ! -f /certs/client/ca.pem ]; do sleep 1; done
set -euxo pipefail
docker volume create home
docker volume create "a"
docker volume create "b"
`,
				}, {
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
						Resources:    corev1.ResourceRequirements{Requests: defaultResources},
					},
					Script: `#!/bin/sh
#id id
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace/dir \
--entrypoint ep \
-e HOME=/builder/home \
-e FOO=foo \
-e BAR=bar \
-v a:/a \
-v b:/b \
image \
foo \
bar \
baz`,
				}},
				Sidecars: []corev1.Container{dindSidecar},
				Volumes:  dockerTLSCertsVolumes,
			},
		},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatalf("Diff(-want,+got): %s", d)
	}
}

// TestToTaskRun_Resources tests conversion of build requests that specify a
// machine_type and custom disk size.
func TestToTaskRun_Resources(t *testing.T) {
	buildID := "build-id"
	got, err := ToTaskRun(&gcb.Build{
		Id:    buildID,
		Steps: []*gcb.BuildStep{{Name: "image"}},
		Options: &gcb.BuildOptions{
			MachineType: "N1_HIGHCPU_32",
			DiskSizeGb:  500,
		},
	})
	if err != nil {
		t.Fatalf("ToTaskRun: %v", err)
	}
	want := &v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildID,
			Namespace: constants.Namespace,
		},
		Spec: v1alpha1.TaskRunSpec{
			ServiceAccountName: constants.ServiceAccountName,
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []v1alpha1.Step{{
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
					},
					Script: `#!/bin/sh
# Wait for the sidecar's generated TLS certs to be generated and shared.
while [ ! -f /certs/client/ca.pem ]; do sleep 1; done
set -euxo pipefail
docker volume create home
`,
				}, {
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
							corev1.ResourceCPU:              resource.MustParse("32"),
							corev1.ResourceMemory:           resource.MustParse("28.8Gi"),
							corev1.ResourceEphemeralStorage: resource.MustParse("500Gi"),
						}},
					},
					Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace \
-e HOME=/builder/home \
image`,
				}},
				Sidecars: []corev1.Container{dindSidecar},
				Volumes:  dockerTLSCertsVolumes,
			},
		},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatalf("Diff(-want,+got): %s", d)
	}
}

func TestToBuild(t *testing.T) {
	create := time.Now()
	start := create.Add(3 * time.Second)
	end := start.Add(time.Minute)
	createTime, startTime, endTime := metav1.NewTime(create), metav1.NewTime(start), metav1.NewTime(end)
	output := "This is my output"
	outputBytes := make([]byte, base64.StdEncoding.EncodedLen(len(output)))
	base64.StdEncoding.Encode(outputBytes, []byte(output))

	got, err := ToBuild(v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:              buildID,
			CreationTimestamp: createTime,
			Annotations: map[string]string{
				"cloudbuild.googleapis.com/logs-copied": "true",
			},
		},
		Spec: v1alpha1.TaskRunSpec{
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []v1alpha1.Step{{
					// Init step.
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
					},
					Script: `#!/bin/sh
exit 0`,
				}, {
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
					},
					Script: `#!/bin/sh
docker run \
--workdir /workspace/dir \
--entrypoint ep \
-e FOO=foo \
-e BAR=bar \
-v a:/a \
-v b:/b \
success \
foo \
bar \
baz`,
				}, {
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
					},
					Script: `#!/bin/sh
docker run \
--workdir /workspace \
failure`,
				}, {
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
					},
					Script: `#!/bin/sh
docker run \
--workdir /workspace \
running`,
				}, {
					Container: corev1.Container{
						Image:        "docker",
						Env:          dockerClientEnvs,
						VolumeMounts: dockerTLSCertsVolumeMounts,
					},
					Script: `#!/bin/sh
docker run \
--workdir /workspace \
waiting`,
				}},
			},
		},
		Status: v1alpha1.TaskRunStatus{
			PodName: "my-cool-pod-name",
			Status: duck.Status{
				Conditions: []apis.Condition{{
					Type:   apis.ConditionSucceeded,
					Status: corev1.ConditionTrue,
				}},
			},
			StartTime:      &startTime,
			CompletionTime: &endTime,
			Steps: []v1alpha1.StepState{{
				// Init step.
				ContainerState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						StartedAt:  startTime,
						FinishedAt: endTime,
						ExitCode:   0,
					},
				},
			}, {
				ContainerState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						StartedAt:  startTime,
						FinishedAt: endTime,
						ExitCode:   0,
					},
				},
			}, {
				ContainerState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						StartedAt:  startTime,
						FinishedAt: endTime,
						ExitCode:   1,
						Reason:     output,
					},
				},
			}, {
				ContainerState: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{
						StartedAt: startTime,
					},
				},
			}, {
				ContainerState: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("ToBuild: %v", err)
	}

	want := &gcb.Build{
		Id:         buildID,
		ProjectId:  constants.ProjectID,
		Status:     SUCCESS,
		LogsBucket: "gs://project-id_cloudbuild",
		LogUrl:     fmt.Sprintf(logURLFmt, "project-id", buildID, buildID),
		CreateTime: create.Format(time.RFC3339),
		StartTime:  start.Format(time.RFC3339),
		FinishTime: end.Format(time.RFC3339),
		Steps: []*gcb.BuildStep{{
			Name:       "success",
			Args:       []string{"foo", "bar", "baz"},
			Entrypoint: "ep",
			Dir:        "dir",
			Env:        []string{"FOO=foo", "BAR=bar"},
			Volumes:    []*gcb.Volume{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}},
			Status:     SUCCESS,
			Timing: &gcb.TimeSpan{
				StartTime: start.Format(time.RFC3339),
				EndTime:   end.Format(time.RFC3339),
			},
		}, {
			Name:   "failure",
			Status: FAILURE,
			Timing: &gcb.TimeSpan{
				StartTime: start.Format(time.RFC3339),
				EndTime:   end.Format(time.RFC3339)},
		}, {
			Name:   "running",
			Status: WORKING,
			Timing: &gcb.TimeSpan{
				StartTime: start.Format(time.RFC3339),
			},
		}, {
			Name: "waiting",
		}},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatalf("Diff(-want,+got): %s", d)
	}
}

func TestToBuild_Status(t *testing.T) {
	for _, c := range []struct {
		cond apis.Condition
		ann  map[string]string
		want string
	}{{
		cond: apis.Condition{},
		want: QUEUED,
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionUnknown,
		},
		want: WORKING,
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionFalse,
		},
		want: WORKING, // Logs not yet copied.
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionFalse,
		},
		ann: map[string]string{
			"cloudbuild.googleapis.com/logs-copied": "true",
		},
		want: FAILURE,
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionTrue,
		},
		want: WORKING, // Logs not yet copied.
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionTrue,
		},
		ann: map[string]string{
			"cloudbuild.googleapis.com/logs-copied": "true",
		},
		want: SUCCESS,
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionUnknown,
			Reason: "ExceededNodeResources",
		},
		want: QUEUED,
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionFalse,
		},
		ann: map[string]string{
			"cloudbuild.googleapis.com/cancelled": "true",
		},
		want: CANCELLED,
	}} {
		t.Run(c.want, func(t *testing.T) {
			got, err := ToBuild(v1alpha1.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: c.ann,
				},
				Spec: v1alpha1.TaskRunSpec{
					TaskSpec: &v1alpha1.TaskSpec{},
				},
				Status: v1alpha1.TaskRunStatus{
					Status: duck.Status{
						Conditions: []apis.Condition{c.cond},
					},
				},
			})
			if err != nil {
				t.Fatalf("ToBuild: %v", err)
			}
			if got.Status != c.want {
				t.Fatalf("ToBuild got status %s, want %s", got.Status, c.want)
			}
		})
	}
}

func TestToBuild_NoStatus(t *testing.T) {
	got, err := ToBuild(v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildID,
		},
		Spec: v1alpha1.TaskRunSpec{
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []v1alpha1.Step{{
					// Init step.
				}, {
					Script: `#!/bin/sh
docker run \
image`,
				}},
			},
		},
		Status: v1alpha1.TaskRunStatus{
			Status: duck.Status{
				Conditions: []apis.Condition{{
					Type:   apis.ConditionSucceeded,
					Status: corev1.ConditionUnknown,
				}},
			},
			// No step status yet; TaskRun or Pod hasn't started.
		},
	})
	if err != nil {
		t.Fatalf("ToBuild: %v", err)
	}

	want := &gcb.Build{
		Id:         buildID,
		ProjectId:  constants.ProjectID,
		Status:     WORKING,
		LogsBucket: "gs://project-id_cloudbuild",
		LogUrl:     fmt.Sprintf(logURLFmt, "project-id", buildID, buildID),
		Steps: []*gcb.BuildStep{{
			Name: "image",
		}},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatalf("Diff(-want,+got): %s", d)
	}
}

func TestToBuild_MoreSteps(t *testing.T) {
	stepOneStart, stepTwoStart, stepThreeStart := time.Now().Add(2*time.Hour), time.Now().Add(3*time.Hour), time.Now().Add(4*time.Hour)

	got, err := ToBuild(v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildID,
			Annotations: map[string]string{
				"cloudbuild.googleapis.com/logs-copied": "true",
			},
		},
		Spec: v1alpha1.TaskRunSpec{
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []v1alpha1.Step{{
					// Init step.
				}, {
					Script: `#!/bin/sh
docker run \
one`,
				}, {
					Script: `#!/bin/sh
docker run \
two`,
				}, {
					Script: `#!/bin/sh
docker run \
three`,
				}},
			},
		},
		Status: v1alpha1.TaskRunStatus{
			Status: duck.Status{
				Conditions: []apis.Condition{{
					Type:   apis.ConditionSucceeded,
					Status: corev1.ConditionTrue,
				}},
			},
			Steps: []v1alpha1.StepState{{
				// Init step.
			}, {
				ContainerState: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(stepOneStart)},
				},
			}, {
				ContainerState: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(stepTwoStart)},
				},
			}, {
				ContainerState: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(stepThreeStart)},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("ToBuild: %v", err)
	}

	// NB: This build doesn't actually make sense (you wouldn't have three running
	// steps at the same time).
	want := &gcb.Build{
		Id:         buildID,
		ProjectId:  constants.ProjectID,
		Status:     SUCCESS,
		LogsBucket: "gs://project-id_cloudbuild",
		LogUrl:     fmt.Sprintf(logURLFmt, "project-id", buildID, buildID),
		Steps: []*gcb.BuildStep{{
			Name:   "one",
			Timing: &gcb.TimeSpan{StartTime: stepOneStart.Format(time.RFC3339)},
			Status: WORKING,
		}, {
			Name:   "two",
			Timing: &gcb.TimeSpan{StartTime: stepTwoStart.Format(time.RFC3339)},
			Status: WORKING,
		}, {
			Name:   "three",
			Timing: &gcb.TimeSpan{StartTime: stepThreeStart.Format(time.RFC3339)},
			Status: WORKING,
		}},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatalf("Diff(-want,+got): %s", d)
	}
}

func TestToAndFromStep(t *testing.T) {
	for _, c := range []struct {
		desc string
		in   *gcb.BuildStep
		want *v1alpha1.Step
	}{{
		desc: "just name",
		in: &gcb.BuildStep{
			Name: "image",
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace \
-e HOME=/builder/home \
image`,
		},
	}, {
		desc: "args",
		in: &gcb.BuildStep{
			Name: "image",
			Args: []string{"foo", "bar", "baz"},
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace \
-e HOME=/builder/home \
image \
foo \
bar \
baz`,
		},
	}, {
		desc: "envs",
		in: &gcb.BuildStep{
			Name: "image",
			Env:  []string{"FOO=foo", "BAR=bar", "BAZ=baz"},
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace \
-e HOME=/builder/home \
-e FOO=foo \
-e BAR=bar \
-e BAZ=baz \
image`,
		},
	}, {
		desc: "volumes",
		in: &gcb.BuildStep{
			Name:    "image",
			Volumes: []*gcb.Volume{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}},
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace \
-e HOME=/builder/home \
-v a:/a \
-v b:/b \
image`,
		},
	}, {
		desc: "relative dir",
		in: &gcb.BuildStep{
			Name: "image",
			Dir:  "foo",
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace/foo \
-e HOME=/builder/home \
image`,
		},
	}, {
		desc: "absolute dir",
		in: &gcb.BuildStep{
			Name: "image",
			Dir:  "/foo",
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /foo \
-e HOME=/builder/home \
image`,
		},
	}, {
		desc: "entrypoint",
		in: &gcb.BuildStep{
			Name:       "image",
			Entrypoint: "ep",
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace \
--entrypoint ep \
-e HOME=/builder/home \
image`,
		},
	}, {
		desc: "step id",
		in: &gcb.BuildStep{
			Name: "image",
			Id:   "id",
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
#id id
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace \
-e HOME=/builder/home \
image`,
		},
	}, {
		desc: "everything",
		in: &gcb.BuildStep{
			Name:       "image",
			Id:         "id",
			Dir:        "foo",
			Entrypoint: "ep",
			Env:        []string{"FOO=foo", "BAR=bar"},
			Volumes:    []*gcb.Volume{{Name: "a", Path: "/a"}, {Name: "b", Path: "/b"}},
			Args:       []string{"foo", "bar", "baz"},
		},
		want: &v1alpha1.Step{
			Container: corev1.Container{
				Image:        "docker",
				Env:          dockerClientEnvs,
				VolumeMounts: dockerTLSCertsVolumeMounts,
			},
			Script: `#!/bin/sh
#id id
docker run \
-v /workspace:/workspace \
-v home:/builder/home \
--workdir /workspace/foo \
--entrypoint ep \
-e HOME=/builder/home \
-e FOO=foo \
-e BAR=bar \
-v a:/a \
-v b:/b \
image \
foo \
bar \
baz`,
		},
	}} {
		t.Run(c.desc, func(t *testing.T) {
			got := toStep(c.in)
			if d := cmp.Diff(c.want, got); d != "" {
				t.Errorf("Diff converting to Step(-want,+got): %s", d)
			}

			back, err := fromStep(got)
			if err != nil {
				t.Fatalf("Converting back to BuildStep: %v", err)
			}
			if d := cmp.Diff(c.in, back); d != "" {
				t.Fatalf("Diff converting back to BuildStep (-want,+got): %s", d)
			}
		})
	}
}
