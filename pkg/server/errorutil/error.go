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

package errorutil

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type httpResponse struct {
	Error *HTTPError `json:"error"`
}

// HTTPError represents an HTTP API error message which will be JSON-encoded
// and sent to clients.
type HTTPError struct {
	Code    int      `json:"code"`
	Message string   `json:"message"`
	Status  string   `json:"status"`
	Details []detail `json:"detail"`
}

type detail struct {
	Type   string `json:"@type"`
	Detail string `json:"detail"`
}

func (h HTTPError) Error() string { return h.Message }

const errorType = "type.googleapis.com/google.rpc.DebugInfo"

func httpError(code int, message, status string) *HTTPError {
	return &HTTPError{
		Code:    code,
		Message: message,
		Status:  status,
		Details: []detail{{
			Type:   errorType,
			Detail: message,
		}},
	}
}

// FromK8s attempts to translate an error received from a Kubernetes API server
// request into a useful end-user-visible HTTP API error message.
func FromK8s(err error) error {
	switch {
	case strings.Contains(err.Error(), "admission webhook \"webhook.tekton.dev\" denied the request"):
		return Invalid(err.Error())
	case strings.Contains(err.Error(), "is forbidden"):
		return Forbidden(err.Error())
	case strings.Contains(err.Error(), "not found"):
		return NotFound(err.Error())
	}
	return err
}

// Serve serves the error to an HTTP response.
//
// The details of the error will be JSON-encoded in the format that clients
// expect.
func Serve(w http.ResponseWriter, err error) {
	var herr *HTTPError
	var ok bool
	if herr, ok = err.(*HTTPError); !ok {
		herr = httpError(http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(herr.Code)
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	if err := e.Encode(httpResponse{Error: herr}); err != nil {
		log.Println("Error JSON-encoding HTTP error response: %v", err)
	}
}

// Invalid returns an HTTPError denoting the request was invalid.
func Invalid(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusBadRequest, fmt.Sprintf(format, args...), "INVALID_ARGUMENT")
}

// Unauthorized returns an HTTPError denoting the user is not authorized to
// perform the requested action.
func Unauthorized(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusUnauthorized, fmt.Sprintf(format, args...), "UNAUTHORIZED")
}

// Forbidden returns an HTTPError denoting the user is forbidden to perform the
// requested action.
func Forbidden(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusForbidden, fmt.Sprintf(format, args...), "FORBIDDEN")
}

// NotFound returns an HTTPError denoting the requested resource is not found.
func NotFound(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusNotFound, fmt.Sprintf(format, args...), "NOT_FOUND")
}
