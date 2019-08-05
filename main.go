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
	"flag"
	"log"
	"net/http"

	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/server"
	"github.com/julienschmidt/httprouter"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

func main() {
	flag.Parse()

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("InClusterConfig: %v", err)
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
	// TODO: Correct path is ":cancel" not "/cancel"
	// https://github.com/julienschmidt/httprouter/issues/196
	router.GET("/v1/projects/:projectID/builds/:buildID", srv.GetBuild)
	router.POST("/v1/projects/:projectID/builds/:buildID/cancel", srv.CancelBuild)
	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Not found:", r.Method, r.URL.Path)
		http.Error(w, "Not found", http.StatusNotFound)
	})
	log.Println("Serving on :80...")
	log.Fatal(http.ListenAndServe(":80", router))
}
