package main

import (
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func list(projectID string, client v1alpha1.TaskRunInterface) (*gcb.ListBuildsResponse, error) {
	resp, err := client.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var lr gcb.ListBuildsResponse
	for _, tr := range resp.Items {
		b, err := toBuild(tr)
		if err != nil {
			return nil, err
		}
		lr.Builds = append(lr.Builds, b)
	}
	return &lr, nil
}
