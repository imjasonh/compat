package reconciler

import (
	"context"

	"github.com/GoogleCloudPlatform/compat/pkg/apis"
	pipelineclient "github.com/tektoncd/pipeline/pkg/client/injection/client"
	taskruninformer "github.com/tektoncd/pipeline/pkg/client/injection/informers/pipeline/v1alpha1/taskrun"
	"k8s.io/client-go/tools/cache"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"
)

// NewController returns a controller.Impl that watches Build CRD resources.
func NewController() func(context.Context, configmap.Watcher) *controller.Impl {
	return func(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
		logger := logging.FromContext(ctx)

		// For creating TaskRuns and Pods.
		kubeclientset := kubeclient.Get(ctx)
		pipelineclientset := pipelineclient.Get(ctx)

		// For watching TaskRun updates.
		taskRunInformer := taskruninformer.Get(ctx)

		// For updating Build statuses in response to TaskRun status changes.
		//buildclientset := buildclient.Get(ctx)

		c := &reconciler{
			logger:            logger,
			kubeClientSet:     kubeclientset,
			pipelineClientSet: pipelineclientset,
			taskRunLister:     taskRunInformer.Lister(),
		}
		impl := controller.NewImpl(c, c.logger, controllerName)

		// Call Reconcile whenever a Build is created or updated.
		taskRunInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.Enqueue,
			UpdateFunc: controller.PassNew(impl.Enqueue),
		})

		// Call Reconcile whenever a TaskRun owned by a Build is updated.
		taskRunInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
			FilterFunc: controller.Filter(apis.BuildGVK),
			Handler:    controller.HandleAll(impl.EnqueueControllerOf),
		})

		c.tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
		return impl
	}
}
