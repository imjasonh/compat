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

	"github.com/ImJasonH/compat/pkg/server"
	"github.com/julienschmidt/httprouter"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

var (
	namespace = flag.String("namespace", "compat", "Namespace in which to run Tekton TaskRuns")
)

func main() {
	flag.Parse()

	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("InClusterConfig: %v", err)
	}
	client := versioned.NewForConfigOrDie(cfg).TektonV1alpha1().TaskRuns(*namespace)
	if _, err := client.List(metav1.ListOptions{}); err != nil {
		log.Fatalf("Cannot list TaskRuns in namespace %q: %v", *namespace, err)
	}
	log.Println("Successfully listed TaskRuns in namespace", *namespace)
	srv := server.New(client)

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
