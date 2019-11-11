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

	buildclient "github.com/ImJasonH/compat/pkg/client/injection/client"
	buildinformer "github.com/ImJasonH/compat/pkg/client/injection/informers/cloudbuild/v1/build"
	pipelineclient "github.com/tektoncd/pipeline/pkg/client/injection/client"
	taskruninformer "github.com/tektoncd/pipeline/pkg/client/injection/informers/pipeline/v1alpha1/taskrun"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"
)

const (
	controllerAgentName = "addressableservice-controller"
)

// NewController returns a new HPA reconcile controller.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)

	buildInformer := buildinformer.Get(ctx)
	taskrunInformer := taskruninformer.Get(ctx)

	c := &Reconciler{
		buildClient:  buildclient.Get(ctx),
		tektonClient: pipelineclient.Get(ctx),
		buildLister:  buildInformer.Lister(),
		Recorder: record.NewBroadcaster().NewRecorder(
			scheme.Scheme, corev1.EventSource{Component: controllerAgentName}),
	}
	impl := controller.NewImpl(c, logger, "AddressableServices")

	logger.Info("Setting up event handlers")

	// Call Reconcile whenever a Build is created or updated.
	buildInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	// Call Reconcile whenever a TaskRun owned by a Build is updated.
	c.Tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
	taskrunInformer.Informer().AddEventHandler(controller.HandleAll(
		// Call the tracker's OnChanged method, but we've seen the objects
		// coming through this path missing TypeMeta, so ensure it is properly
		// populated.
		controller.EnsureTypeMeta(
			c.Tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("TaskRun"),
		),
	))
	return impl
}
