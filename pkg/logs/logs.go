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

package logs

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/GoogleCloudPlatform/compat/pkg/constants"
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

	podName, containerNames, err := l.waitUntilStart(name)
	if err != nil {
		return err
	}

	for _, containerName := range containerNames {
		log.Printf("getting logs for pod %q container %q", podName, containerName)
		var rc io.ReadCloser
		for {
			rc, err = l.PodClient.GetLogs(podName, &corev1.PodLogOptions{
				Container: containerName,
				Follow:    true,
			}).Stream()
			if err != nil {
				if strings.Contains(err.Error(), "is waiting to start: PodInitializing") {
					continue
				}
				return fmt.Errorf("Error getting K8s log stream: %v", err)
			}
			break
		}
		if _, err := io.Copy(io.MultiWriter(os.Stdout, w), rc); err != nil {
			return fmt.Errorf("Error copying logs to GCS: %v", err)
		}
		if err := rc.Close(); err != nil {
			return fmt.Errorf("Error closing K8s logs stream: %v", err)
		}
	}

	// Annotate the TaskRun to note that the logs are done being copied.
	// pkg/convert/convert.go uses this to determine whether it should
	// return a finished Build status, so that gcloud stops polling for
	// logs.
	tr, err := l.Client.Get(name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Error getting TaskRun to annotate for logs copy completion: %v", err)
	}
	if tr.Annotations == nil {
		tr.Annotations = map[string]string{}
	}
	tr.Annotations["cloudbuild.googleapis.com/logs-copied"] = "true"
	if _, err := l.Client.Update(tr); err != nil {
		return fmt.Errorf("Error annotating TaskRun to annotate for logs copy completion: %v", err)
	}
	return nil
}

func (l LogCopier) waitUntilStart(name string) (podName string, containerNames []string, err error) {
	tr, err := l.Client.Get(name, metav1.GetOptions{})
	if err != nil {
		return "", nil, err
	}

	if tr.Status.PodName != "" {
		return tr.Status.PodName, getContainerNames(tr), nil
	}

	watcher, err := l.Client.Watch(metav1.SingleObject(metav1.ObjectMeta{
		Name:      name,
		Namespace: constants.Namespace,
	}))
	if err != nil {
		return "", nil, err
	}
	for evt := range watcher.ResultChan() {
		switch evt.Type {
		case watch.Deleted:
			return "", nil, fmt.Errorf("TaskRun %q was deleted while watching", name)
		case watch.Error:
			return "", nil, fmt.Errorf("Error watching TaskRun %q: %v", name, evt.Object)
		}
		tr, ok := evt.Object.(*v1alpha1.TaskRun)
		if !ok {
			return "", nil, fmt.Errorf("Got non-TaskRun object watching %q: %T", name, evt.Object)
		}

		if tr.Status.PodName != "" {
			return tr.Status.PodName, getContainerNames(tr), nil
		}
	}
	return "", nil, errors.New("watch ended before taskrun started")
}

func getContainerNames(tr *v1alpha1.TaskRun) []string {
	var cn []string
	for _, s := range tr.Status.Steps {
		cn = append(cn, s.ContainerName)
	}
	return cn
}
