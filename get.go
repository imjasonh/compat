package main

import (
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func get(buildID string, client v1alpha1.TaskRunInterface) (*gcb.Build, error) {
	tr, err := client.Get(buildID, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return toBuild(*tr)
}
