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
	"encoding/json"
	"testing"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duck "knative.dev/pkg/apis/duck/v1beta1"
)

const (
	buildID = "build-id"
)

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
		Id: "repo-source",
		Source: &gcb.Source{RepoSource: &gcb.RepoSource{
			BranchName: "master",
		}},
	}} {
		if _, err := ToTaskRun(b); err != ErrIncompatible {
			t.Errorf("ToTaskRun(%q): got %v, want incompatible", b.Id, err)
		}
	}
}

func TestToTaskRun(t *testing.T) {
	build, err := ToTaskRun(&gcb.Build{
		Id:      "compatible",
		Timeout: time.Minute.String(),
		Steps: []*gcb.BuildStep{{
			Name:       "ubuntu",
			Args:       []string{"sleep", "10"},
			Env:        []string{"FOO=bar", "BAR=baz"},
			Entrypoint: "bash",
			Dir:        "foo/bar",
			Id:         "my-id",
			Volumes: []*gcb.Volume{{
				Name: "foo",
				Path: "/foo",
			}, {
				Name: "bar",
				Path: "/bar",
			}},
		}, {
			Name: "busybox",
			Args: []string{"true"},
			// No entrypoint, command should not be [""]
			Volumes: []*gcb.Volume{{
				Name: "foo",
				Path: "/something/else",
			}},
		}},
		Source: &gcb.Source{
			StorageSource: &gcb.StorageSource{
				Bucket:     "my-bucket",
				Object:     "my-object.tar.gz",
				Generation: 12345,
			},
		},
	})
	if err != nil {
		t.Fatalf("ToTaskRun: %v", err)
	}

	wantTaskRun := &v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "compatible",
			Annotations: map[string]string{
				"cloud.google.com/service-account": robotEmail,
				"entrypoint-0":                     "bash",
			},
		},
		Spec: v1alpha1.TaskRunSpec{
			Timeout: &metav1.Duration{time.Minute},
			TaskSpec: &v1alpha1.TaskSpec{
				Inputs: &v1alpha1.Inputs{
					Resources: []v1alpha1.TaskResource{{
						Name: "source",
						Type: "storage",
					}},
				},
				Steps: []corev1.Container{{
					Image:      "ubuntu",
					Name:       "my-id",
					WorkingDir: "foo/bar",
					Command:    []string{"bash", "sleep", "10"},
					Env: []corev1.EnvVar{{
						Name:  "FOO",
						Value: "bar",
					}, {
						Name:  "BAR",
						Value: "baz",
					}},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "foo",
						MountPath: "/foo",
					}, {
						Name:      "bar",
						MountPath: "/bar",
					}},
				}, {
					Image:   "busybox",
					Command: []string{"true"},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "foo",
						MountPath: "/something/else",
					}},
				}},
				Volumes: []corev1.Volume{{
					Name:         "bar",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}, {
					Name:         "foo",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}},
			},
			Inputs: v1alpha1.TaskRunInputs{
				Resources: []v1alpha1.TaskResourceBinding{{
					Name: "source",
					ResourceSpec: &v1alpha1.PipelineResourceSpec{
						Type: v1alpha1.PipelineResourceTypeStorage,
						Params: []v1alpha1.Param{{
							Name:  "location",
							Value: "gs://my-bucket/my-object.tar.gz#12345",
						}, {
							Name:  "artifactType",
							Value: string(v1alpha1.GCSArchive),
						}, {
							Name:  "type",
							Value: "build-gcs",
						}},
					},
				}},
			},
		},
	}
	if diff := jsondiff(build, wantTaskRun); diff != "" {
		t.Errorf("ToTaskRun build diff: %s", diff)
	}
}

func jsondiff(l, r interface{}) string {
	lb, err := json.MarshalIndent(l, "", " ")
	if err != nil {
		panic(err.Error())
	}
	rb, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		panic(err.Error())
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(lb), string(rb), true)
	for _, d := range diffs {
		if d.Type != diffmatchpatch.DiffEqual {
			return dmp.DiffPrettyText(diffs)
		}
	}
	return ""
}
func TestToBuild(t *testing.T) {
	start := time.Now()
	end := start.Add(time.Minute)
	startTime, endTime := metav1.NewTime(start), metav1.NewTime(end)
	output := "This is my output"
	outputBytes := make([]byte, base64.StdEncoding.EncodedLen(len(output)))
	base64.StdEncoding.Encode(outputBytes, []byte(output))

	got, err := ToBuild(v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildID,
			Annotations: map[string]string{
				"entrypoint-0": "foo",
			},
		},
		Spec: v1alpha1.TaskRunSpec{
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []corev1.Container{{
					Image:      "success",
					Name:       "id",
					Command:    []string{"foo", "bar", "baz"},
					WorkingDir: "dir",
					Env: []corev1.EnvVar{{
						Name:  "a",
						Value: "b",
					}, {
						Name:  "b",
						Value: "c",
					}},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "foo",
						MountPath: "/foo",
					}},
				}, {
					Image: "failure",
				}, {
					Image: "running",
				}, {
					Image: "waiting",
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
			StartTime:      &startTime,
			CompletionTime: &endTime,
			Steps: []v1alpha1.StepState{{
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
		Status:     SUCCESS,
		StartTime:  start.Format(time.RFC3339),
		FinishTime: end.Format(time.RFC3339),
		Steps: []*gcb.BuildStep{{
			Name:       "success",
			Id:         "id",
			Args:       []string{"bar", "baz"},
			Entrypoint: "foo",
			Dir:        "dir",
			Env:        []string{"a=b", "b=c"},
			Volumes: []*gcb.Volume{{
				Name: "foo",
				Path: "/foo",
			}},
			Status: SUCCESS,
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
		Results: &gcb.Results{},
	}
	if diff := jsondiff(got, want); diff != "" {
		t.Fatalf("Got diff: %s", diff)
	}
}

func TestToBuild_Status(t *testing.T) {
	for _, c := range []struct {
		cond apis.Condition
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
		want: FAILURE,
	}, {
		cond: apis.Condition{
			Type:   apis.ConditionSucceeded,
			Status: corev1.ConditionTrue,
		},
		want: SUCCESS,
	}} {
		t.Run(c.want, func(t *testing.T) {
			got, err := ToBuild(v1alpha1.TaskRun{
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

func TestToBuild_MoreSteps(t *testing.T) {
	implicitOneStart, implicitTwoStart := time.Now(), time.Now().Add(time.Hour)
	stepOneStart, stepTwoStart, stepThreeStart := time.Now().Add(2*time.Hour), time.Now().Add(3*time.Hour), time.Now().Add(4*time.Hour)

	got, err := ToBuild(v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildID,
		},
		Spec: v1alpha1.TaskRunSpec{
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []corev1.Container{{
					Image: "one",
				}, {
					Image: "two",
				}, {
					Image: "three",
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
				ContainerState: corev1.ContainerState{
					// implicit step!
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(implicitOneStart)},
				},
			}, {
				ContainerState: corev1.ContainerState{
					// another implicit step!
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(implicitTwoStart)},
				},
			}, {
				ContainerState: corev1.ContainerState{
					// actual step one
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(stepOneStart)},
				},
			}, {
				ContainerState: corev1.ContainerState{
					// actual step two
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(stepTwoStart)},
				},
			}, {
				ContainerState: corev1.ContainerState{
					// actual step three
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(stepThreeStart)},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("ToBuild: %v", err)
	}

	// NB: This build doesn't actually make sense (you wouldn't have three running
	// steps at the same time), but it demonstrates that the input Build CRD has
	// the last three step states applied to the build's actual three steps, and
	// ignores the implicit step states.
	want := &gcb.Build{
		Id:     buildID,
		Status: SUCCESS,
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
		Results: &gcb.Results{},
	}
	if diff := jsondiff(got, want); diff != "" {
		t.Fatalf("Got diff: %s", diff)
	}
}
