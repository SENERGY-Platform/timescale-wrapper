/*
 *    Copyright 2026 InfAI (CC SES)
 *
 *    Licensed under the Apache License, Version 2.0 (the "License");
 *    you may not use this file except in compliance with the License.
 *    You may obtain a copy of the License at
 *
 *        http://www.apache.org/licenses/LICENSE-2.0
 *
 *    Unless required by applicable law or agreed to in writing, software
 *    distributed under the License is distributed on an "AS IS" BASIS,
 *    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *    See the License for the specific language governing permissions and
 *    limitations under the License.
 */

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDocEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	DocEndpoint(router, newDeps(nil, nil, nil, nil, nil, nil))
	for range 2 { // the second request is served from the rendered document
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/doc", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d", recorder.Code)
		}
		body := recorder.Body.String()
		if strings.Contains(body, `"host": "",`) || !json.Valid([]byte(body)) {
			t.Fatalf("unexpected document: %.200s", body)
		}
	}
}
