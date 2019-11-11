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

package main // import "github.com/ImJasonH/compat/cmd/api"

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/server"
	"github.com/julienschmidt/httprouter"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	container "google.golang.org/api/container/v1beta1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

func main() {
	flag.Parse()

	var cfg *rest.Config
	var err error
	if clusterName := os.Getenv("CLUSTER_NAME"); clusterName != "" {
		cfg, err = offClusterConfig(clusterName)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatalf("Could not get cluster REST config: %v", err)
	}

	client := versioned.NewForConfigOrDie(cfg).TektonV1alpha1().TaskRuns(constants.Namespace)

	podClient := typedcorev1.NewForConfigOrDie(cfg).Pods(constants.Namespace)

	srv := server.New(client, podClient)
	if err := srv.Preflight(); err != nil {
		log.Fatalf("❌ Preflight check failed: %v", err)
	}

	router := httprouter.New()
	router.POST("/v1/projects/:projectID/builds", srv.CreateBuild)
	router.GET("/v1/projects/:projectID/builds", srv.ListBuilds)
	router.GET("/v1/operations/build/:projectID/:opName", srv.GetOperation)
	router.GET("/v1/projects/:projectID/builds/:buildID", srv.GetBuild)
	router.MethodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: httprouter does not support paths containing the ":"
		// literal. To support the path to cancel builds, we hook in to
		// the MethodNotAllowed handler and parse the request
		// ourselves.
		// https://github.com/julienschmidt/httprouter/issues/196
		//
		// Avoid invoking regexp matching if the request isn't a POST
		// to projects/*

		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/projects/") {
			if found := cancelPathRE.FindStringSubmatch(r.URL.Path); len(found) == 3 {
				projectID := found[1]
				buildID := found[2]
				srv.CancelBuild(w, r, projectID, buildID)
				return
			}
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	})
	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Not found: %s %s", r.Method, r.URL.Path)
		http.Error(w, "Not found", http.StatusNotFound)
	})
	router.PanicHandler = func(w http.ResponseWriter, r *http.Request, i interface{}) {
		log.Printf("PANIC: %v", i)
		http.Error(w, fmt.Sprintf("panic: %v", i), http.StatusInternalServerError)
	}
	log.Println("Serving on :8080...")
	log.Fatal(http.ListenAndServe(":8080", router))
}

var cancelPathRE = regexp.MustCompile(`/v1/projects/([a-z-]+)/builds/([a-z0-9-]+):cancel`)

func offClusterConfig(clusterName string) (*rest.Config, error) {
	ctx := context.Background()
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, err
	}

	svc, err := container.NewService(ctx)
	if err != nil {
		return nil, err
	}
	gc, err := svc.Projects.Locations.Clusters.Get(clusterName).Do()
	if err != nil {
		return nil, err
	}

	caBytes, err := base64.StdEncoding.DecodeString(gc.MasterAuth.ClusterCaCertificate)
	if err != nil {
		return nil, err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caBytes)
	trans := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: caCertPool,
		},
	}
	return &rest.Config{
		Host: "https://" + gc.Endpoint,
		Transport: &oauth2.Transport{
			Base:   trans,
			Source: ts,
		},
	}, nil
}
