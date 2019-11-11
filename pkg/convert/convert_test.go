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
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/server/errorutil"
	"github.com/sergi/go-diff/diffmatchpatch"
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
		Id: "repo-source",
		Source: &gcb.Source{RepoSource: &gcb.RepoSource{
			BranchName: "master",
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
				Object:     "path/to/my-object.tar.gz",
				Generation: 12345,
			},
		},
	})
	if err != nil {
		t.Fatalf("ToTaskRun: %v", err)
	}

	want := &v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildID,
			Namespace: constants.Namespace,
			Annotations: map[string]string{
				"cloudbuild.googleapis.com/entrypoint-0": "bash",
			},
		},
		Spec: v1alpha1.TaskRunSpec{
			ServiceAccountName: constants.ServiceAccountName,
			Timeout:            &metav1.Duration{time.Minute},
			TaskSpec: &v1alpha1.TaskSpec{
				Inputs: &v1alpha1.Inputs{
					Resources: []v1alpha1.TaskResource{{ResourceDeclaration: v1alpha1.ResourceDeclaration{
						Name: "source",
						Type: "storage",
					}}},
				},
				Steps: []v1alpha1.Step{{Container: corev1.Container{
					Image:      "ubuntu",
					Name:       "my-id",
					WorkingDir: "/workspace/source/foo/bar",
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
					Resources: corev1.ResourceRequirements{Requests: defaultResources},
				}}, {Container: corev1.Container{
					Image:      "busybox",
					WorkingDir: "/workspace/source",
					Command:    []string{"true"},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "foo",
						MountPath: "/something/else",
					}},
					Resources: corev1.ResourceRequirements{Requests: defaultResources},
				}}},
				Volumes: []corev1.Volume{{
					Name:         "bar",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}, {
					Name:         "foo",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}},
			},
			Inputs: v1alpha1.TaskRunInputs{
				Resources: []v1alpha1.TaskResourceBinding{{PipelineResourceBinding: v1alpha1.PipelineResourceBinding{
					Name: "source",
					ResourceSpec: &v1alpha1.PipelineResourceSpec{
						Type: v1alpha1.PipelineResourceTypeStorage,
						Params: []v1alpha1.ResourceParam{{
							Name:  "location",
							Value: "gs://my-bucket/path/to/my-object.tar.gz", // TODO: generation
						}, {
							Name:  "artifactType",
							Value: string(v1alpha1.GCSTarGzArchive),
						}, {
							Name:  "type",
							Value: "build-gcs",
						}},
					},
				}}},
			},
		},
	}
	if diff := jsondiff(got, want); diff != "" {
		t.Errorf("ToTaskRun build diff: %s", diff)
	}
}

// TestToTaskRun_Resources tests conversion of build requests that specify a
// machine_type and custom disk size.
func TestToTaskRun_Resources(t *testing.T) {
	buildID := "build-id"
	build, err := ToTaskRun(&gcb.Build{
		Id:    buildID,
		Steps: []*gcb.BuildStep{{Name: "ubuntu"}},
		Options: &gcb.BuildOptions{
			MachineType: "N1_HIGHCPU_32",
			DiskSizeGb:  500,
		},
	})
	if err != nil {
		t.Fatalf("ToTaskRun: %v", err)
	}
	wantTaskRun := &v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildID,
			Namespace: constants.Namespace,
		},
		Spec: v1alpha1.TaskRunSpec{
			ServiceAccountName: constants.ServiceAccountName,
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []v1alpha1.Step{{Container: corev1.Container{
					Image: "ubuntu",
					Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
						corev1.ResourceCPU:              resource.MustParse("32"),
						corev1.ResourceMemory:           resource.MustParse("28.8Gi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("500Gi"),
					}},
				}}},
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
	create := time.Now()
	start := create.Add(3 * time.Second)
	end := start.Add(time.Minute)
	createTime, startTime, endTime := metav1.NewTime(create), metav1.NewTime(start), metav1.NewTime(end)
	output := "This is my output"
	outputBytes := make([]byte, base64.StdEncoding.EncodedLen(len(output)))
	base64.StdEncoding.Encode(outputBytes, []byte(output))

	got, err := ToBuild(v1alpha1.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: buildID,
			Annotations: map[string]string{
				"cloudbuild.googleapis.com/entrypoint-0": "foo",
				"cloudbuild.googleapis.com/logs-copied":  "true",
			},
			CreationTimestamp: createTime,
		},
		Spec: v1alpha1.TaskRunSpec{
			Inputs: v1alpha1.TaskRunInputs{
				Resources: []v1alpha1.TaskResourceBinding{{PipelineResourceBinding: v1alpha1.PipelineResourceBinding{
					Name: "source",
					ResourceSpec: &v1alpha1.PipelineResourceSpec{
						Type: v1alpha1.PipelineResourceTypeStorage,
						Params: []v1alpha1.ResourceParam{{
							Name:  "location",
							Value: "gs://my-bucket/path/to/my-object.tar.gz#12345",
						}, {
							Name:  "artifactType",
							Value: string(v1alpha1.GCSArchive),
						}, {
							Name:  "type",
							Value: "build-gcs",
						}},
					},
				}}},
			},
			TaskSpec: &v1alpha1.TaskSpec{
				Steps: []v1alpha1.Step{{Container: corev1.Container{
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
				}}, {Container: corev1.Container{
					Image: "failure",
				}}, {Container: corev1.Container{
					Image: "running",
				}}, {Container: corev1.Container{
					Image: "waiting",
				}}},
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
				ImageID: "docker-pullable://foo@sha256:abcdef",
				ContainerState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						StartedAt:  startTime,
						FinishedAt: endTime,
						ExitCode:   0,
					},
				},
			}, {
				ImageID: "docker-pullable://bar@sha256:def123",
				ContainerState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						StartedAt:  startTime,
						FinishedAt: endTime,
						ExitCode:   1,
						Reason:     output,
					},
				},
			}, {
				// No image ID.
				ContainerState: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{
						StartedAt: startTime,
					},
				},
			}, {
				ImageID: "docker-pullable://baz@sha256:123456",
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
		LogUrl:     fmt.Sprintf(logURLFmt, "project-id", "project-id", "my-cool-pod-name"),
		CreateTime: create.Format(time.RFC3339),
		StartTime:  start.Format(time.RFC3339),
		FinishTime: end.Format(time.RFC3339),
		Source: &gcb.Source{StorageSource: &gcb.StorageSource{
			Bucket:     "my-bucket",
			Object:     "path/to/my-object.tar.gz",
			Generation: 12345,
		}},
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
		Results: &gcb.Results{
			BuildStepImages: []string{
				"sha256:abcdef",
				"sha256:def123",
				"",
				"sha256:123456",
			},
		},
	}
	if diff := jsondiff(got, want); diff != "" {
		t.Fatalf("Got diff: %s", diff)
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

func TestToBuild_MoreSteps(t *testing.T) {
	sourceFetchStart, sourceFetchFinish := time.Now(), time.Now().Add(3*time.Minute)
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
				Steps: []v1alpha1.Step{{Container: corev1.Container{
					Image: "one",
				}}, {Container: corev1.Container{
					Image: "two",
				}}, {Container: corev1.Container{
					Image: "three",
				}}},
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
				ContainerName: "create-dir-source-blahblahblah",
			}, {
				ContainerName: "storage-fetch-source-blahblahblah",
				ContainerState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						StartedAt:  metav1.NewTime(sourceFetchStart),
						FinishedAt: metav1.NewTime(sourceFetchFinish),
					},
				},
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
		Results: &gcb.Results{
			BuildStepImages: []string{"", "", ""},
		},
		Timing: map[string]gcb.TimeSpan{
			"FETCHSOURCE": gcb.TimeSpan{
				StartTime: sourceFetchStart.Format(time.RFC3339),
				EndTime:   sourceFetchFinish.Format(time.RFC3339),
			},
		},
	}
	if diff := jsondiff(got, want); diff != "" {
		t.Fatalf("Got diff: %s", diff)
	}
}

func TestGetImageDigest(t *testing.T) {
	for _, c := range []struct {
		imageID, want string
	}{{
		// normal case
		imageID: "docker-pullable://image-name@sha256:abcdefg",
		want:    "sha256:abcdefg",
	}, {
		// new digest type!
		imageID: "docker-pullable://image-name@sha512:abcdefg",
		want:    "sha512:abcdefg",
	}, {
		// invalid prefix
		imageID: "not-pullable://blahblah",
		want:    "",
	}, {
		// invalid, ends in @
		imageID: "docker-pullable://image-name@",
		want:    "",
	}, {
		// invalid, does not contain @
		imageID: "docker-pullable://undigested",
		want:    "",
	}, {
		// not valid, but parseable anyway; trims from the last @
		imageID: "docker-pullable://contains-@-image@sha256:abcdefg",
		want:    "sha256:abcdefg",
	}, {
		// empty in, empty out
		imageID: "",
		want:    "",
	}} {
		got := getImageDigest(c.imageID)
		if got != c.want {
			t.Errorf("getImageDigest(%q): got %q, want %q", c.imageID, got, c.want)
		}
	}
}

func TestToTaskRun_Source(t *testing.T) {
	got, err := ToTaskRun(&gcb.Build{
		Id:        buildID,
		ProjectId: constants.ProjectID,
		Source: &gcb.Source{
			StorageSource: &gcb.StorageSource{
				Bucket: "my-bucket",
				Object: "path/to/my-object.tar.gz",
			},
		},
		Steps: []*gcb.BuildStep{{
			Name: "ubuntu",
			// No dir.
		}, {
			Name: "ubuntu",
			Dir:  "relative/dir",
		}, {
			Name: "ubuntu",
			Dir:  "/absolute/dir",
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
			TaskSpec: &v1alpha1.TaskSpec{
				Inputs: &v1alpha1.Inputs{
					Resources: []v1alpha1.TaskResource{{ResourceDeclaration: v1alpha1.ResourceDeclaration{
						Name: "source",
						Type: "storage",
					}}},
				},
				Steps: []v1alpha1.Step{{Container: corev1.Container{
					Image:      "ubuntu",
					WorkingDir: "/workspace/source",
					Resources:  corev1.ResourceRequirements{Requests: defaultResources},
				}}, {Container: corev1.Container{
					Image:      "ubuntu",
					WorkingDir: "/workspace/source/relative/dir",
					Resources:  corev1.ResourceRequirements{Requests: defaultResources},
				}}, {Container: corev1.Container{
					Image:      "ubuntu",
					WorkingDir: "/absolute/dir",
					Resources:  corev1.ResourceRequirements{Requests: defaultResources},
				}}},
			},
			Inputs: v1alpha1.TaskRunInputs{
				Resources: []v1alpha1.TaskResourceBinding{{PipelineResourceBinding: v1alpha1.PipelineResourceBinding{
					Name: "source",
					ResourceSpec: &v1alpha1.PipelineResourceSpec{
						Type: v1alpha1.PipelineResourceTypeStorage,
						Params: []v1alpha1.ResourceParam{{
							Name:  "location",
							Value: "gs://my-bucket/path/to/my-object.tar.gz", // TODO: generation
						}, {
							Name:  "artifactType",
							Value: string(v1alpha1.GCSTarGzArchive),
						}, {
							Name:  "type",
							Value: "build-gcs",
						}},
					},
				}}},
			},
		},
	}
	if diff := jsondiff(got, want); diff != "" {
		t.Fatalf("Got diff: %s", diff)
	}

}
