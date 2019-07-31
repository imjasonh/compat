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
	"encoding/json"
	"log"
	"net/http"

	"cloud.google.com/go/storage"
	"github.com/ImJasonH/compat/pkg/logs"
	"github.com/julienschmidt/httprouter"
	typedv1alpha1 "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type Server struct {
	client    typedv1alpha1.TaskRunInterface
	logCopier logs.LogCopier
}

func New(client typedv1alpha1.TaskRunInterface, podClient typedcorev1.PodExpansion, gcs *storage.Client) *Server {
	return &Server{
		client: client,
		logCopier: logs.LogCopier{
			Client:    client,
			PodClient: podClient,
			GCS:       gcs,
		},
	}
}

func httpError(w http.ResponseWriter, err error) {
	// TODO: JSON-encode response
	// TODO: actual real error codes
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *Server) ListBuilds(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := ps.ByName("projectID")
	log.Printf("ListBuilds for project %q", projectID)
	// TODO: Check if projectID == constants.ProjectID

	resp, err := s.list()
	if err != nil {
		httpError(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Encode: %v", err)
	}
}

func (s *Server) CreateBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := ps.ByName("projectID")
	log.Printf("CreateBuild for project %q", projectID)
	// TODO: Check if projectID == constants.ProjectID

	b := &gcb.Build{}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(b); err != nil {
		httpError(w, err)
		return
	}

	b, err := s.create(b)
	if err != nil {
		httpError(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(b); err != nil {
		log.Printf("Encode: %v", err)
	}
}

func (s *Server) GetBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	buildID := ps.ByName("buildID")
	log.Printf("GetBuild for build %q", buildID)
	// TODO: Check if projectID == constants.ProjectID

	b, err := s.get(buildID)
	if err != nil {
		httpError(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(b); err != nil {
		log.Printf("Encode: %v", err)
	}
}
