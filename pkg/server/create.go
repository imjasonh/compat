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
	"github.com/ImJasonH/compat/pkg/server/errorutil"
	"github.com/google/uuid"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func (s *Server) create(b *gcb.Build) (*gcb.Operation, error) {
	log.Println("Creating Build...")
	b.Id = uuid.New().String() // Generate a new build ID.

	// Apply substitutions.
	if err := convert.SubstituteBuildFields(b); err != nil {
		return nil, err
	}

	// Convert to TaskRun and create it.
	tr, err := convert.ToTaskRun(b)
	if err != nil {
		return nil, err
	}
	tr, err = s.client.Create(tr)
	if err != nil {
		return nil, errorutil.FromK8s(err)
	}

	// Watch TaskRun for logs.
	go func() {
		if err := s.logCopier.Copy(tr.Name); err != nil {
			log.Printf("Error copying logs for build %q: %v", tr.Name, err)
		}
	}()

	// Watch TaskRun for PubSub updates.
	go func() {
		if err := s.watch(tr.Name); err != nil {
			log.Printf("Error watching TaskRun for build %q: %v", tr.Name, err)
		}
	}()

	// Convert TaskRun to Build and wrap it in an Operation response.
	b, err = convert.ToBuild(*tr)
	if err != nil {
		return nil, err
	}
	return buildToOp(b)
}

func (s *Server) watch(name string) error {
	watcher, err := s.client.Watch(metav1.SingleObject(metav1.ObjectMeta{
		Name:      name,
		Namespace: constants.Namespace,
	}))
	if err != nil {
		return err
	}
	for evt := range watcher.ResultChan() {
		switch evt.Type {
		case watch.Deleted:
			log.Println("TaskRun was deleted; possible cancellation?")
			return nil
		case watch.Error:
			return fmt.Errorf("Error watching TaskRun %q: %v", name, evt.Object)
		}

		tr, ok := evt.Object.(*v1alpha1.TaskRun)
		if !ok {
			return fmt.Errorf("Got non-TaskRun object watching %q: %T", name, evt.Object)
		}
		b, err := convert.ToBuild(*tr)
		if err != nil {
			return fmt.Errorf("Error converting watched TaskRun %q: %v", name, err)
		}
		if err := s.pubsub.Publish(b); err != nil {
			return fmt.Errorf("Error publishing: %v", err)
		}
	}
	return nil
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
