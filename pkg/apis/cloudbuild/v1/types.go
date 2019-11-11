/*
Copyright 2019 The Knative Authors.

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

package v1

import (
	"bytes"
	"context"
	"encoding/json"

	gcb "google.golang.org/api/cloudbuild/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Build struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Build GCBBuild `json:",inline"`
}

type GCBBuild gcb.Build

func (in *GCBBuild) DeepCopyInto(out *GCBBuild) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(in)
	_ = json.NewDecoder(&buf).Decode(out)
}

// Check that Build can be validated and defaulted.
var _ apis.Validatable = (*Build)(nil)
var _ apis.Defaultable = (*Build)(nil)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BuildList is a list of Build resources
type BuildList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Build `json:"items"`
}

// GetGroupVersionKind implements kmeta.OwnerRefable
func (b *Build) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Build")
}

// SetDefaults implements apis.Defaultable
func (*Build) SetDefaults(ctx context.Context) {
	// TODO: Set ID, createTime, etc.
}

// Validate implements apis.Validatable
func (b *Build) Validate(ctx context.Context) *apis.FieldError {
	return nil // TODO: validate
}
