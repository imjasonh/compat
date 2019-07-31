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

	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/convert"
	"github.com/google/uuid"
	gcb "google.golang.org/api/cloudbuild/v1"
)

func (s *Server) create(b *gcb.Build) (*gcb.Build, error) {
	log.Println("Creating Build...")
	tr, err := convert.ToTaskRun(b)
	if err != nil {
		return nil, err
	}
	tr.Name = uuid.New().String()                         // Generate the build ID.
	tr.Spec.ServiceAccount = constants.ServiceAccountName // Run as the Workload Identity KSA/GSA
	tr, err = s.client.Create(tr)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := s.logCopier.Copy(tr.Name); err != nil {
			log.Printf("Error copying logs for build %q: %v", tr.Name, err)
		}
	}()

	return convert.ToBuild(*tr)
}
