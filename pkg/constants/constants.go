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

// Package constants provides constants used by many packages.
package constants

import "fmt"

const (
	// Namespace is the K8s namespace the service expects to run in.
	Namespace = "gcb-compat"

	// ServiceAccountName is the K8s Service Account the service expects
	// to run as, and which it runs TaskRuns as.
	ServiceAccountName = "gcb-compat-account"
)

var (
	// ProjectID is the ID of the project running this service, as
	// determined at server startup.
	ProjectID = ""
)

// LogsBucket returns the only supported logs bucket, based on the project ID.
func LogsBucket() string { return fmt.Sprintf("%s_cloudbuild", ProjectID) }
