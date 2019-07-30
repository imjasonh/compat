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

package server

import (
	"log"

	"github.com/ImJasonH/compat/pkg/convert"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func list(projectID string, client v1alpha1.TaskRunInterface) (*gcb.ListBuildsResponse, error) {
	log.Println("Listing Builds...")
	resp, err := client.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var lr gcb.ListBuildsResponse
	for _, tr := range resp.Items {
		b, err := convert.ToBuild(tr)
		if err != nil {
			return nil, err
		}
		lr.Builds = append(lr.Builds, b)
	}
	return &lr, nil
}
