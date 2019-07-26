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

package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/ImJasonH/compat/pkg/server"
	"github.com/julienschmidt/httprouter"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	masterURL  = flag.String("master_url", "", "API server URL")
	kubeconfig = flag.String("kubeconfig", "", "Path to kube config")
	namespace  = flag.String("namespace", "compat", "Namespace in which to run Tekton TaskRuns")
)

func main() {
	flag.Parse()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatalf("BuildConfigFromFlags: %v", err)
	}
	srv := server.New(versioned.NewForConfigOrDie(cfg).TektonV1alpha1().TaskRuns(*namespace))

	router := httprouter.New()
	router.POST("/v1/projects/:projectID/builds", srv.CreateBuild)
	router.GET("/v1/projects/:projectID/builds", srv.ListBuilds)
	router.GET("/v1/projects/:projectID/builds/:buildID", srv.GetBuild)
	log.Fatal(http.ListenAndServe(":8080", router))
}
