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
	"github.com/ImJasonH/compat/pkg/convert"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
)

func create(b *gcb.Build, client v1alpha1.TaskRunInterface) (*gcb.Build, error) {
	tr, err := convert.ToTaskRun(b)
	if err != nil {
		return nil, err
	}
	tr, err = client.Create(tr)
	if err != nil {
		return nil, err
	}
	return convert.ToBuild(*tr)
}
