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

	"github.com/GoogleCloudPlatform/compat/pkg/convert"
	"github.com/GoogleCloudPlatform/compat/pkg/server/errorutil"
	gcb "google.golang.org/api/cloudbuild/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Server) get(buildID string) (*gcb.Build, error) {
	log.Println("Getting Build...")
	tr, err := s.client.Get(buildID, metav1.GetOptions{})
	if err != nil {
		return nil, errorutil.FromK8s(err)
	}
	return convert.ToBuild(*tr)
}
