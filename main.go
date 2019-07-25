package main

import (
	"log"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

func main() {
	srv := &server{}

	router := httprouter.New()
	router.POST("/v1/projects/:projectID/builds", srv.createBuild)
	router.GET("/v1/projects/:projectID/builds", srv.listBuilds)
	router.GET("/v1/projects/:projectID/builds/:buildID", srv.getBuild)
	log.Fatal(http.ListenAndServe(":8080", router))
}

type server struct {
}

func (s *server) createBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	log.Printf("CreateBuild for project %q", ps.ByName("projectID"))
}

func (s *server) listBuilds(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	log.Printf("ListBuilds for project %q", ps.ByName("projectID"))
}

func (s *server) getBuild(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	log.Printf("GetBuild for project %q build %q", ps.ByName("projectID"), ps.ByName("buildID"))
}
