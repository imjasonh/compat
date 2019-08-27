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
	"log"

	"github.com/GoogleCloudPlatform/compat/pkg/constants"
	"github.com/tektoncd/cli/pkg/cli"
	trlog "github.com/tektoncd/cli/pkg/cmd/taskrun"
	"github.com/tektoncd/cli/pkg/helper/pods"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	k8s "k8s.io/client-go/kubernetes"
)

type LogCopier struct {
	tektonClient versioned.Interface
	kubeClient   k8s.Interface
}

func NewLogCopier(tektonClient versioned.Interface, kubeClient k8s.Interface) LogCopier {
	return LogCopier{
		tektonClient: tektonClient,
		kubeClient:   kubeClient,
	}
}

func (l LogCopier) Copy(name string) error {
	objectName := fmt.Sprintf("log-%s.txt", name)
	w, err := NewWriter(constants.LogsBucket(), objectName)
	if err != nil {
		return err
	}

	if err := l.waitUntilStart(name); err != nil {
		return err
	}

	r := &trlog.LogReader{
		Run: name,
		Ns:  constants.Namespace,
		Clients: &cli.Clients{
			Tekton: l.tektonClient,
			Kube:   l.kubeClient,
		},
		Follow:   true,
		AllSteps: true,
		Streamer: pods.NewStream,
	}
	log.Printf("reading logs for %q, writing to %q", name, objectName)
	logCh, errCh, err := r.Read()
	if err != nil {
		return err
	}
	trlog.NewLogWriter().Write(&cli.Stream{
		Out: w,
		Err: w,
	}, logCh, errCh)
	return l.annotateLogsCopied(name)
}

// annotateLogsCopied annotates the TaskRun to note that the logs are done
// being copied.  pkg/convert/convert.go uses this to determine whether it
// should return a finished Build status, so that gcloud stops polling for
// logs.
func (l LogCopier) annotateLogsCopied(name string) error {
	client := l.tektonClient.TektonV1alpha1().TaskRuns(constants.Namespace)
	tr, err := client.Get(name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Error getting TaskRun to annotate for logs copy completion: %v", err)
	}
	if tr.Annotations == nil {
		tr.Annotations = map[string]string{}
	}
	tr.Annotations["cloudbuild.googleapis.com/logs-copied"] = "true"
	if _, err := client.Update(tr); err != nil {
		return fmt.Errorf("Error annotating TaskRun to annotate for logs copy completion: %v", err)
	}
	return nil
}

func (l LogCopier) waitUntilStart(name string) error {
	client := l.tektonClient.TektonV1alpha1().TaskRuns(constants.Namespace)

	tr, err := client.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if tr.Status.PodName != "" {
		return nil
	}

	watcher, err := client.Watch(metav1.SingleObject(metav1.ObjectMeta{
		Name:      name,
		Namespace: constants.Namespace,
	}))
	if err != nil {
		return err
	}
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

		if tr.Status.PodName != "" {
			return nil
		}
	}
	return errors.New("watch ended before taskrun started")
}
