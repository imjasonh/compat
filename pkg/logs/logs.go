package logs

import (
	"fmt"
	"io"
	"log"

	"github.com/ImJasonH/compat/pkg/constants"
	"github.com/ImJasonH/compat/pkg/convert"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	typedv1alpha1 "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/typed/pipeline/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

type LogCopier struct {
	Client    typedv1alpha1.TaskRunInterface
	PodClient typedcorev1.PodExpansion
}

func (l LogCopier) Copy(name string) error {
	objectName := fmt.Sprintf("log-%s.txt", name)
	w, err := NewWriter(constants.LogsBucket(), objectName)
	if err != nil {
		return err
	}

	// First, wait until the TaskRun is done.
	watcher, err := l.Client.Watch(metav1.SingleObject(metav1.ObjectMeta{
		Name:      name,
		Namespace: constants.Namespace,
	}))
	if err != nil {
		return err
	}
	var podName string
	var containerNames []string
	for evt := range watcher.ResultChan() {
		switch evt.Type {
		case watch.Deleted:
			return fmt.Errorf("TaskRun %q was deleted while watching", name)
		case watch.Error:
			return fmt.Errorf("Error watching TaskRun %q: %v", name, evt.Object)
		}
		tr, ok := evt.Object.(*v1alpha1.TaskRun)
		if !ok {
			return fmt.Errorf("Got non-TaskRun object watching %q: %T", name, evt.Object)
		}
		b, err := convert.ToBuild(*tr)
		if err != nil {
			return fmt.Errorf("Error converting watched TaskRun %q: %v", name, err)
		}
		switch b.Status {
		case convert.WORKING, convert.QUEUED:
			continue
		}
		if tr.Status.PodName != "" {
			podName = tr.Status.PodName
		}
		for _, ss := range tr.Status.Steps { // These are in order.
			containerNames = append(containerNames, ss.ContainerName)
		}
		break
	}

	// Then, copy the logs for each container in the TaskRun's pods.
	for _, containerName := range containerNames {
		log.Printf("getting logs for pod %q container %q", podName, containerName)
		rs, err := l.PodClient.GetLogs(podName, &corev1.PodLogOptions{
			Container: containerName,
		}).Stream()
		if err != nil {
			return fmt.Errorf("Error getting K8s log stream: %v", err)
		}
		if _, err := io.Copy(w, rs); err != nil {
			return fmt.Errorf("Error copying logs to GCS: %v", err)
		}
		if err := rs.Close(); err != nil {
			return fmt.Errorf("Error closing K8s logs stream: %v", err)
		}
	}
	return nil
}
