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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"

	"cloud.google.com/go/compute/metadata"
	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/logs"
	"github.com/ImJasonH/compat/pkg/pubsub"
	"github.com/ImJasonH/compat/pkg/server/errorutil"
	"github.com/julienschmidt/httprouter"
	typedv1alpha1 "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcb "google.golang.org/api/cloudbuild/v1"
	crm "google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/option"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type Server struct {
	client     typedv1alpha1.TaskRunInterface
	crmService *crm.Service
	logCopier  logs.LogCopier
	pubsub     *pubsub.Publisher
}

func New(client typedv1alpha1.TaskRunInterface, podClient typedcorev1.PodExpansion) *Server {
	return &Server{
		client: client,
		logCopier: logs.LogCopier{
			Client:    client,
			PodClient: podClient,
		},
		pubsub: pubsub.New(),
	}
}

func (s *Server) Preflight() error {
	// KSA has permission to list TaskRuns:
	if _, err := s.client.List(metav1.ListOptions{}); err != nil {
		return fmt.Errorf("taskRuns.List: cannot list TaskRuns in namespace %q: %v", constants.Namespace, err)
	}
	log.Println("✔️ Successfully listed TaskRuns in namespace", constants.Namespace)

	// Service is running on GCE:
	if !metadata.OnGCE() {
		return errors.New("metadata.OnGCE: service not running on GCE")
	}
	log.Println("✔️ Service running on GCP")

	// KSA can get its project ID from GCE metadata:
	if projectID, err := metadata.ProjectID(); err != nil {
		return fmt.Errorf("metadata.ProjectID: cannot determine GCP project ID: %v", err)
	} else {
		// Note this for later...
		constants.ProjectID = projectID
	}
	log.Println("✔️ Service can get its GCP project ID")

	// GSA can get a Google OAuth token for expected service account:
	if tok, err := google.ComputeTokenSource("").Token(); err != nil {
		return fmt.Errorf("google.ComputeTokenSource: cannot get Google auth token: %v", err)
	} else if tok.AccessToken == "" {
		return fmt.Errorf("google.ComputeTokenSource: AccessToken is empty")
	}
	// TODO: Check that the GSA has the expected name?
	log.Println("✔️ Service can get Google OAuth token")

	// GSA can write to the logs bucket:
	logsBucket := constants.LogsBucket()
	if _, err := logs.NewWriter(logsBucket, "preflight"); err != nil {
		return fmt.Errorf("object.NewWriter: cannot write preflight object to logs bucket %q: %v", logsBucket, err)
	}
	log.Println("✔️ Service can write to GCS logs bucket")

	// TODO: preflight pods logs.
	return nil
}

func checkProject(got string) error {
	if got != constants.ProjectID {
		return fmt.Errorf("Project mismatch: got %q, want %q", got, constants.ProjectID)
	}
	return nil
}

var (
	buildGetter  = []string{"cloudbuild.builds.get"}
	buildUpdater = []string{"cloudbuild.builds.update"}
	buildLister  = []string{"cloudbuild.builds.list"}
	buildCreator = []string{"cloudbuild.builds.create"}
)

func (s *Server) checkAuth(r *http.Request, perms []string) error {
	tok := r.Header.Get("Authorization")
	if tok == "" {
		return errors.New("No Authorization header")
	}
	if !strings.HasPrefix(tok, "Bearer ") {
		return errors.New("Authorization header malformed")
	}
	tok = strings.TrimPrefix(tok, "Bearer ")
	svc, err := crm.NewService(context.Background(), option.WithTokenSource(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: tok})))
	if err != nil {
		return err
	}
	resp, err := svc.Projects.TestIamPermissions(constants.ProjectID, &crm.TestIamPermissionsRequest{
		Permissions: perms,
	}).Do()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(resp.Permissions, perms) {
		return fmt.Errorf("Caller has insufficient subset of permissions: got %v, want %v", resp.Permissions, perms)
	}
	return nil
}

func (s *Server) ListBuilds(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if err := checkProject(ps.ByName("projectID")); err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := s.checkAuth(r, buildLister); err != nil {
		errorutil.Serve(w, err)
		return
	}

	resp, err := s.list()
	if err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Encode: %v", err)
	}
}

func (s *Server) CreateBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if err := checkProject(ps.ByName("projectID")); err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := s.checkAuth(r, buildCreator); err != nil {
		errorutil.Serve(w, err)
		return
	}

	b := &gcb.Build{}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(b); err != nil {
		errorutil.Serve(w, err)
		return
	}

	op, err := s.create(b)
	if err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(op); err != nil {
		log.Printf("Encode: %v", err)
	}
}

func (s *Server) GetBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if err := checkProject(ps.ByName("projectID")); err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := s.checkAuth(r, buildGetter); err != nil {
		errorutil.Serve(w, err)
		return
	}

	buildID := ps.ByName("buildID")
	log.Printf("GetBuild for build %q", buildID)

	b, err := s.get(buildID)
	if err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(b); err != nil {
		log.Printf("Encode: %v", err)
	}
}

func (s *Server) GetOperation(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	if err := checkProject(ps.ByName("projectID")); err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := s.checkAuth(r, buildGetter); err != nil {
		errorutil.Serve(w, err)
		return
	}

	opName := ps.ByName("opName")
	log.Printf("GetOperation for operation %q", opName)
	bs, err := base64.StdEncoding.DecodeString(opName)
	if err != nil {
		errorutil.Serve(w, err)
		return
	}
	buildID := string(bs)
	log.Printf("GetOperation for build %q", buildID)

	b, err := s.get(buildID)
	if err != nil {
		errorutil.Serve(w, err)
		return
	}
	op, err := buildToOp(b)
	if err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(op); err != nil {
		log.Printf("Encode: %v", err)
	}
}

func (s *Server) CancelBuild(w http.ResponseWriter, r *http.Request, projectID, buildID string) {
	if err := checkProject(projectID); err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := s.checkAuth(r, buildUpdater); err != nil {
		errorutil.Serve(w, err)
		return
	}

	log.Printf("GetBuild for build %q", buildID)

	b, err := s.cancel(buildID)
	if err != nil {
		errorutil.Serve(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(b); err != nil {
		log.Printf("Encode: %v", err)
	}
}
