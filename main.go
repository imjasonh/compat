package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	gcb "google.golang.org/api/cloudbuild/v1"
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
	srv := &server{
		client: versioned.NewForConfigOrDie(cfg).TektonV1alpha1().TaskRuns(*namespace),
	}

	router := httprouter.New()
	router.POST("/v1/projects/:projectID/builds", srv.createBuild)
	router.GET("/v1/projects/:projectID/builds", srv.listBuilds)
	router.GET("/v1/projects/:projectID/builds/:buildID", srv.getBuild)
	log.Fatal(http.ListenAndServe(":8080", router))
}

type server struct {
	client v1alpha1.TaskRunInterface
}

func httpError(w http.ResponseWriter, err error) {
	// TODO: JSON-encode response
	// TODO: actual real error codes
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func (s *server) listBuilds(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := ps.ByName("projectID")
	log.Printf("ListBuilds for project %q", projectID)

	resp, err := list(projectID, s.client)
	if err != nil {
		httpError(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Println("Encode: %v", err)
	}
}

func (s *server) createBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	projectID := ps.ByName("projectID")
	log.Printf("CreateBuild for project %q", projectID)

	b := &gcb.Build{}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(b); err != nil {
		httpError(w, err)
		return
	}

	b, err := create(b, s.client)
	if err != nil {
		httpError(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(b); err != nil {
		log.Println("Encode: %v", err)
	}
}

func (s *server) getBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	buildID := ps.ByName("buildID")
	log.Printf("GetBuild for build %q", buildID)

	b, err := get(buildID, s.client)
	if err != nil {
		httpError(w, err)
		return
	}
	if err := json.NewEncoder(w).Encode(b); err != nil {
		log.Println("Encode: %v", err)
	}
}
