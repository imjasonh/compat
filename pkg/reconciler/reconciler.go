package reconciler

import (
	"context"
	"time"

	clientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	listers "github.com/tektoncd/pipeline/pkg/client/listers/pipeline/v1alpha1"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/tracker"
)

const (
	controllerName = "gcb-compat-controller"
	resyncPeriod   = 10 * time.Hour
)

// reconciler implements controller.Reconciler for Configuration resources.
type reconciler struct {
	// Sugared logger is easier to use but is not as performant as the
	// raw logger. In performance critical paths, call logger.Desugar()
	// and use the returned raw logger instead. In addition to the
	// performance benefits, raw logger also preserves type-safety at
	// the expense of slightly greater verbosity.
	logger *zap.SugaredLogger

	// kubeClientSet allows us to talk to the k8s for core APIs
	kubeClientSet kubernetes.Interface

	// pipelineClientSet allows us to configure pipeline objects
	pipelineClientSet clientset.Interface

	// listers index properties about resources
	taskRunLister listers.TaskRunLister
	tracker       tracker.Interface
}

func (c *reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	c.logger.Infof("Reconciling %q / %q...", namespace, name)
	return nil
}
