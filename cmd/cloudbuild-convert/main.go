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
	"fmt"
	"log"
	"os"

	"github.com/GoogleCloudPlatform/compat/pkg/convert"
	gcb "google.golang.org/api/cloudbuild/v1"
	yaml "gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

var (
	config         = flag.String("config", "cloudbuild.yaml", "Config file to convert")
	namespace      = flag.String("namespace", "default", "Namespace of resulting output TaskRun")
	name           = flag.String("name", "converted-", "Prefix of TaskRun name")
	serviceAccount = flag.String("serviceaccount", "", "Service Account to use to execute the TaskRun")
	help           = flag.Bool("help", false, "Print usage and exit")
)

func usage() {
	fmt.Println(`Convert a Google Cloud Build config (cloudbuild.yaml) into
a Tekton Task that specifies the same steps.

The resulting Task definition is printed to stdout, where it can be redirected
to a file or piped to "kubectl apply -f -" to apply it directly to a cluster.

	--config	Config file to convert (default "cloudbuild.yaml")
	--name		Prefix of TaskRun name (default "converted-")
	--namespace	Namespace of resulting output Task (default "default")
	--help		Print usage and exit`)
	os.Exit(0)
}

func main() {
	flag.Parse()
	if *help {
		usage()
	}

	f, err := os.OpenFile(*config, os.O_RDONLY, 0755)
	if err != nil {
		log.Fatalf("Could not open %q: %v", *config, err)
	}
	defer f.Close()

	var b gcb.Build
	d := yaml.NewDecoder(f)
	d.SetStrict(true) // Fail on unknown fields.
	if err := d.Decode(&b); err != nil {
		log.Fatalf("Parsing YAML: %v", err)
	}

	tr, err := convert.ToTaskRun(&b)
	if err != nil {
		log.Fatalf("Converting to TaskRun: %v", err)
	}
	tr.TypeMeta = metav1.TypeMeta{
		APIVersion: "tekton.dev/v1alpha1",
		Kind:       "TaskRun",
	}
	tr.ObjectMeta.GenerateName = *name
	tr.ObjectMeta.Namespace = *namespace
	tr.Spec.ServiceAccountName = *serviceAccount

	if err := json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil).Encode(tr, os.Stdout); err != nil {
		log.Fatalf("YAML-encoding Task: %v", err)
	}
}
