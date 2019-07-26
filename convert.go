package main

import (
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
)

func toBuild(tr v1alpha1.TaskRun) (*gcb.Build, error) {
	// TODO: convert
	return &gcb.Build{}, nil
}

func toTaskRun(b *gcb.Build) (*v1alpha1.TaskRun, error) {
	// TODO: convert
	return &v1alpha1.TaskRun{}, nil
}
