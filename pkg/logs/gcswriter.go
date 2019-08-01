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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"google.golang.org/api/googleapi"
	storage "google.golang.org/api/storage/v1"
)

// TODO: Port over tests and mock GCS.

const (
	// Maximum number of times we can compose an object before needing to
	// reset the object.
	maxComposes = 500

	// String constant for MIME type.
	textPlain = "text/plain"
)

var (
	// Time to sleep after a failed API call to GCS.
	initialWaitDur = time.Second

	// Maximum time to sleep after a failed API call to GCS.
	maxWaitDur = 32 * time.Second

	// Maximum number of retries before giving up.
	maxRetries = 7
)

// NewWriter returns a Writer used for streamily writing a log object to GCS.
// NewWriter also attempts to create the empty log object and will fail if that
// doesn't succeed (eg, permissions issues).
func NewWriter(bucket, object string) (io.Writer, error) {
	svc, err := storage.NewService(context.Background())
	if err != nil {
		return nil, err
	}
	w := &writer{bucket: bucket, object: object, service: svc}
	if err := w.create(); err != nil {
		return nil, err
	}
	return w, nil
}

type writer struct {
	bucket, object string // GCS bucket and object name.
	service        *storage.Service
	created        bool
	numComposes    int
	generation     int64
}

// retry implements an exponential backoff that retries four times: 1s, 2s, 4s, 8s.
func retry(fn func() error) error {
	waitDur := initialWaitDur
	n := 0
	for {
		if err := fn(); err != nil {
			if n >= maxRetries {
				return err
			}
			n++
			log.Printf("Exponential retry #%d (%s) for error: %v\n", n, waitDur.String(), err)
			time.Sleep(waitDur)
			waitDur *= 2
			if waitDur > maxWaitDur {
				waitDur = maxWaitDur
			}
			continue
		}
		return nil
	}
}

// create writes an empty file to GCS, which we will later append to.
func (w *writer) create() error {
	var empty bytes.Buffer
	req := w.service.Objects.Insert(w.bucket, &storage.Object{
		Name: w.object,
	}).Media(&empty, googleapi.ContentType(textPlain))
	// If we see transient failures here, add retry logic.
	obj, err := req.Do()
	if err != nil {
		return fmt.Errorf("error creating logfile: %v", err)
	}
	w.generation = obj.Generation
	return nil
}

func (w *writer) resetComponentCount() error {
	// Because we're composing the running object from intermediate
	// objects, and there is a limit to the number of objects that
	// can compose an object, we have to reset the composite object
	// by copying the object and copying it back. See:
	// https://cloud.google.com/storage/docs/composite-objects#_Count

	// First, copy the contents of the object to a new object. Copying by
	// downloading then uploading to the new object resets the object's component
	// count. Copying "in the cloud" with the Copy API does not reset the
	// component count.
	// Idempotency: safe to insert the temp object repeatedly.
	tmpObj := w.object + "-copied"
	if err := retry(func() error {
		start := time.Now()
		resp, err := w.service.Objects.Get(w.bucket, w.object).Download()
		if err != nil {
			return err
		}
		log.Printf("objects.get took %s", time.Since(start))
		defer resp.Body.Close()
		start = time.Now()
		obj, err := w.service.Objects.Insert(w.bucket, &storage.Object{
			Name: tmpObj,
		}).Media(resp.Body).Do()
		log.Printf("objects.insert took %s", time.Since(start))
		if err != nil {
			return err
		} else if obj.ComponentCount > 2 {
			return errors.New("failed to reset object component count")
		}
		return nil
	}); err != nil {
		return err
	}

	// Then, copy the object back from tmpObj back to the original location, "in
	// the cloud", since the component count has been reset.
	// Idempotency: safe to copy object repeatedly.
	var obj *storage.Object
	if err := retry(func() error {
		var err error
		start := time.Now()
		obj, err = w.service.Objects.Copy(w.bucket, tmpObj, w.bucket, w.object, nil).Do()
		log.Printf("objects.get took %s", time.Since(start))
		return err
	}); err != nil {
		return err
	}
	// We modified the object, so we need to record the new generation.
	w.generation = obj.Generation

	// Finally, delete the temp object in the background. Deleting is only
	// best-effort, and we want to avoid the added latency.
	go func() {
		start := time.Now()
		if err := w.service.Objects.Delete(w.bucket, tmpObj).Do(); err != nil {
			log.Printf("Failed to delete temp object %s: %v", tmpObj, err)
		}
		log.Printf("objects.delete took %s (background)", time.Since(start))
	}()

	w.numComposes = 0
	return nil
}

// Write (implements io.Writer) writes data to the GCS object.
func (w *writer) Write(buf []byte) (int, error) {
	if w.numComposes >= maxComposes {
		if err := w.resetComponentCount(); err != nil {
			return 0, err
		}
	}

	// Insert object to append.
	// Idempotency: duplicate append will overwrite with same data and succeed.
	start := time.Now()
	tmpObj := fmt.Sprintf("%s-appending-%d", w.object, time.Now().UnixNano())
	if err := retry(func() error {
		_, err := w.service.Objects.Insert(w.bucket, &storage.Object{Name: tmpObj}).Media(bytes.NewReader(buf)).Do()
		return err
	}); err != nil {
		return 0, err
	}
	log.Printf("objects.insert took %s", time.Since(start))

	// Compose the original and new objects together.
	// Idempotency:
	// If a compose operation silently succeeds (service accepts write, but we time out and fail request),
	// the next compose operation will fail with status 412: Failed Precondition. This is a Good Thing, as
	// it ensures we will write the compose at-most-once.  Our retries ensure at-least-once, so as long as
	// no other writer is messing with our object behind our back, we have an exactly-once guarantee!
	var obj *storage.Object
	if err := retry(func() error {
		var err error
		start := time.Now()
		obj, err = w.service.Objects.Compose(w.bucket, w.object, &storage.ComposeRequest{
			Destination: &storage.Object{
				ContentType: textPlain,
			},
			SourceObjects: []*storage.ComposeRequestSourceObjects{
				{
					Name: w.object,
					ObjectPreconditions: &storage.ComposeRequestSourceObjectsObjectPreconditions{
						// If this doesn't match, the call returns status 412.
						// Since we assume that we are the only writer, we take generation mismatch to mean
						// that we have already written the object and can move on.
						IfGenerationMatch: w.generation,
					},
				},
				{Name: tmpObj},
			},
		}).Do()
		log.Printf("objects.compose took %s", time.Since(start))
		if isPreconditionFailed(err) {
			// If preconditions fail, the call has permanently failed and obj=nil.
			// We interpret this to mean that we must have already written the object.
			obj = nil
			return nil
		}
		return err
	}); err != nil {
		return 0, err
	}
	if obj == nil {
		// We must have missed the successful response to a compose request.
		// However, we still need to get the updated object generation.  That's what this final GET call is for.
		log.Println("GCS logging: PreconditionFail means success. Fetching object to find latest generation.")
		if err := retry(func() error {
			var err error
			obj, err = w.service.Objects.Get(w.bucket, w.object).Do()
			return err
		}); err != nil {
			return len(buf), err
		}
	}
	w.generation = obj.Generation
	w.numComposes++

	// Delete the temp object in the background.
	go func() {
		start := time.Now()
		if err := w.service.Objects.Delete(w.bucket, tmpObj).Do(); err != nil {
			log.Printf("Failed to delete temp object %s: %v", tmpObj, err)
		}
		log.Printf("objects.delete took %s (background)", time.Since(start))
	}()

	return len(buf), nil
}

func isPreconditionFailed(err error) bool {
	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == http.StatusPreconditionFailed {
			return true
		}
	}
	return false
}
