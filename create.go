package main

import (
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
)

func create(b *gcb.Build, client v1alpha1.TaskRunInterface) (*gcb.Build, error) {
	tr, err := toTaskRun(b)
	if err != nil {
		return nil, err
	}
	tr, err = client.Create(tr)
	if err != nil {
		return nil, err
	}
	return toBuild(*tr)
}
