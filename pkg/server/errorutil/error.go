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
)

type httpResponse struct {
	Error *HTTPError `json:"error"`
}

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

func Invalid(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusBadRequest, fmt.Sprintf(format, args...), "INVALID_ARGUMENT")
}

func Unauthorized(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusUnauthorized, fmt.Sprintf(format, args...), "UNAUTHORIZED")
}

func Forbidden(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusForbidden, fmt.Sprintf(format, args...), "FORBIDDEN")
}

func NotFound(format string, args ...interface{}) *HTTPError {
	return httpError(http.StatusNotFound, fmt.Sprintf(format, args...), "NOT_FOUND")
}
