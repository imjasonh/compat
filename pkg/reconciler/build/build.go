/*
Copyright 2019 The Knative Authors

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

package build

import (
	"context"
	"reflect"

	v1 "github.com/ImJasonH/compat/pkg/apis/cloudbuild/v1"
	clientset "github.com/ImJasonH/compat/pkg/client/clientset/versioned"
	listers "github.com/ImJasonH/compat/pkg/client/listers/cloudbuild/v1"
	"github.com/ImJasonH/compat/pkg/convert"
	pipelineclient "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"go.uber.org/zap"
	gcb "google.golang.org/api/cloudbuild/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"
)

const compatNamespace = "gcb-compat"

// Reconciler implements controller.Reconciler for Build resources.
type Reconciler struct {
	// Client is used to write back status updates.
	buildClient  clientset.Interface
	tektonClient pipelineclient.Interface

	// Listers index properties about resources
	buildLister listers.BuildLister

	// The tracker builds an index of what resources are watching other
	// resources so that we can immediately react to changes to changes in
	// tracked resources.
	Tracker tracker.Interface

	// Recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	Recorder record.EventRecorder
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile implements controller.Reconciler
func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	if namespace != compatNamespace {
		logger.Warnf("Got Build in namespace %q; ignoring", namespace)
		return nil
	}

	// Get the resource with this namespace/name.
	orig, err := r.buildLister.Builds(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("resource %q no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy.
	b := orig.DeepCopy()

	// Reconcile this copy of the resource and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := r.reconcile(ctx, b)
	if equality.Semantic.DeepEqual(orig, b) {
		// If we didn't change anything then don't call updateBuild.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if err := r.updateBuild(b); err != nil {
		logger.Warnw("Failed to update resource status", zap.Error(err))
		r.Recorder.Eventf(b, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for %q: %v", b.Build.Id, err)
		return err
	}
	if reconcileErr != nil {
		r.Recorder.Event(b, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
	}
	return reconcileErr
}

func (r *Reconciler) reconcile(ctx context.Context, b *v1.Build) error {
	logger := logging.FromContext(ctx)

	inner := gcb.Build(b.Build)

	tr, err := r.tektonClient.TektonV1alpha1().TaskRuns(compatNamespace).Get(b.Build.Id, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// We haven't created a TaskRun for this Build yet. Do that now.
		tr, err = convert.ToTaskRun(&inner)
		if err != nil {
			logger.Errorf("Error converting Build %q to TaskRun: %v", b.Build.Id, err)
			return err
		}
		tr, err = r.tektonClient.TektonV1alpha1().TaskRuns(compatNamespace).Create(tr)
		if err != nil {
			logger.Errorf("Error creating TaskRun %q: %v", b.Build.Id, err)
			return err
		}
		logger.Infof("Created TaskRun for Build %q", b.Build.Id)
	} else if err != nil {
		logger.Errorf("Error getting TaskRun for Build %q: %v", b.Build.Id, err)
		return err
	}

	// Convert the TaskRun back to a Build and update its state in etcd.
	back, err := convert.ToBuild(*tr)
	if err != nil {
		logger.Errorf("Error converting TaskRun %q to Build: %v", b.Build.Id, err)
		return err
	}
	b.Build = v1.GCBBuild(*back)
	if err := r.updateBuild(b); err != nil {
		logger.Errorf("Error updating Build %q: %v", b.Build.Id, err)
		return err
	}
	return nil
}

// Update the Build resource.  Caller is responsible for checking for semantic
// differences before calling.
func (r *Reconciler) updateBuild(desired *v1.Build) error {
	actual, err := r.buildLister.Builds(compatNamespace).Get(desired.Name)
	if err != nil {
		return err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(actual, desired) {
		return nil
	}
	_, err = r.buildClient.CloudbuildV1().Builds(compatNamespace).Update(desired)
	return err
}
