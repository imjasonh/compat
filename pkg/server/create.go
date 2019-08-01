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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/convert"
	"github.com/google/uuid"
	gcb "google.golang.org/api/cloudbuild/v1"
)

func (s *Server) create(b *gcb.Build) (*gcb.Operation, error) {
	log.Println("Creating Build...")
	tr, err := convert.ToTaskRun(b)
	if err != nil {
		return nil, err
	}
	tr.Name = uuid.New().String() // Generate the build ID.
	tr, err = s.client.Create(tr)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := s.logCopier.Copy(tr.Name); err != nil {
			log.Printf("Error copying logs for build %q: %v", tr.Name, err)
		}
	}()

	b, err = convert.ToBuild(*tr)
	if err != nil {
		return nil, err
	}
	return buildToOp(b)
}

func buildToOp(b *gcb.Build) (*gcb.Operation, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(gcb.BuildOperationMetadata{Build: b}); err != nil {
		return nil, err
	}

	name := fmt.Sprintf("operations/build/%s/%s", constants.ProjectID, base64.StdEncoding.EncodeToString([]byte(b.Id)))
	return &gcb.Operation{
		Name:     name,
		Done:     false,
		Metadata: buf.Bytes(),
	}, nil
}
