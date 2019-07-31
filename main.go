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

package main // import "github.com/ImJasonH/compat"

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"

	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/storage"
	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/server"
	"github.com/julienschmidt/httprouter"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	"golang.org/x/oauth2/google"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

func preflight(client v1alpha1.TaskRunInterface, gcs *storage.Client) error {
	// KSA has permission to list TaskRuns:
	if _, err := client.List(metav1.ListOptions{}); err != nil {
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

	// GSA can get a Google OAuth token for necessary scopes:
	if _, err := google.ComputeTokenSource("", "https://www.googleapis.com/auth/cloud-platform").Token(); err != nil {
		return fmt.Errorf("google.ComputeTokenSource: cannot get Google auth token: %v", err)
	}
	log.Println("✔️ Service can get Google OAuth token")

	// GSA can write to the logs bucket:
	/*
		logsBucket := constants.LogsBucket()
		w := gcs.Bucket(logsBucket).Object("preflight").NewWriter(context.Background())
		io.WriteString(w, "hello")
		if err := w.Close(); err != nil {
			return fmt.Errorf("object.NewWriter: cannot write preflight object to logs bucket %q: %v", logsBucket, err)
		}
		log.Println("✔️ Service can write to GCS logs bucket")
	*/

	return nil
}

func main() {
	flag.Parse()

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("InClusterConfig: %v", err)
	}
	client := versioned.NewForConfigOrDie(cfg).TektonV1alpha1().TaskRuns(constants.Namespace)
	gcs, err := storage.NewClient(context.Background())
	if err != nil {
		log.Fatalf("storage.NewClient: %v", err)
	}

	if err := preflight(client, gcs); err != nil {
		log.Fatalf("❌ Preflight check failed: %v", err)
	}

	podClient := typedcorev1.NewForConfigOrDie(cfg).Pods(constants.Namespace)
	// TODO: preflight pods logs.

	srv := server.New(client, podClient, gcs)
	router := httprouter.New()
	router.POST("/v1/projects/:projectID/builds", srv.CreateBuild)
	router.GET("/v1/projects/:projectID/builds", srv.ListBuilds)
	router.GET("/v1/projects/:projectID/builds/:buildID", srv.GetBuild)
	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Not found:", r.Method, r.URL.Path)
		http.Error(w, "Not found", http.StatusNotFound)
	})
	log.Println("Serving on :80...")
	log.Fatal(http.ListenAndServe(":80", router))
}
