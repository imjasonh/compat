package apis

import (
	"context"

	gcb "google.golang.org/api/cloudbuild/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
)

// BuildGVK is the GroupVersionKind of a Build CRD.
var BuildGVK = schema.GroupVersion{Group: "cloudbuild.googleapis.com", Version: "v1"}.WithKind("Build")

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Build struct {
	*gcb.Build `json:",inline"`
}

// TODO: Implement defaulting and validation
func (b *Build) SetDefaults(context.Context)               {}
func (b *Build) Validate(context.Context) *apis.FieldError { return nil }

// TODO: generate zz_generated_deepcopy.go
func (b *Build) DeepCopyObject() runtime.Object   { return nil }
func (b *Build) GetObjectKind() schema.ObjectKind { return nil }
