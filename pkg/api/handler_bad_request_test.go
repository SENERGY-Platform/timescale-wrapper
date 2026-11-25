/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	gin_mw "github.com/SENERGY-Platform/gin-middleware"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/gin-gonic/gin"
)

// The handlers reject these requests before the verifier, the cache or the database is used, so
// every dependency is nil.

const badRequestServiceId = "urn:infai:ses:service:00000000-0000-0000-0000-00000000000a"

// A JWT with the subject 1234567890. The signature is not checked, only the claims are read.
const badRequestToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

func TestQueriesV2RejectsBadRequests(t *testing.T) {
	t.Parallel()
	router := gin.New()
	router.Use(gin_mw.ErrorHandler(model.GetStatusCode, ", "))
	QueriesV2Endpoint(router, newDeps(nil, nil, nil, nil, nil, nil))

	cases := []struct {
		name   string
		target string
		body   string
	}{
		{"order_direction up", "/queries/v2?order_direction=up", "[]"},
		{"invalid precision", "/queries/v2?precision=16", "[]"},
		{"body is not JSON", "/queries/v2", "{"},
		{"element without columns", "/queries/v2", `[{"deviceId":"d","serviceId":"` + badRequestServiceId + `"}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, c.target, strings.NewReader(c.body)))
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestQueriesRejectsBadRequests(t *testing.T) {
	t.Parallel()
	router := gin.New()
	router.Use(gin_mw.ErrorHandler(model.GetStatusCode, ", "))
	QueriesEndpoint(router, newDeps(nil, nil, nil, nil, nil, nil))

	cases := []struct {
		name   string
		target string
		body   string
	}{
		{"order_direction up", "/queries?order_direction=up", "[]"},
		{"invalid precision", "/queries?precision=16", "[]"},
		{"body is not JSON", "/queries", "{"},
		{"element without columns", "/queries", `[{"deviceId":"d","serviceId":"` + badRequestServiceId + `"}]`},
		{"deviceGroupId is only valid for /queries/v2", "/queries", `[{"deviceGroupId":"g","columns":[{"name":"root.a"}]}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, c.target, strings.NewReader(c.body)))
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestLastValuesRejectsBadRequests(t *testing.T) {
	t.Parallel()
	handler := lastValueHandler(newDeps(nil, nil, nil, nil, nil, nil))
	validBody := `[{"deviceId":"d","serviceId":"` + badRequestServiceId + `","columnName":"root.a"}]`

	cases := []struct {
		name          string
		target        string
		body          string
		authorization string
	}{
		{"invalid precision", "/last-values?precision=16", validBody, badRequestToken},
		{"body is not JSON", "/last-values", "{", badRequestToken},
		{"element without device or export", "/last-values", `[{"columnName":"root.a"}]`, badRequestToken},
		{"valid body without Authorization", "/last-values", validBody, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, c.target, strings.NewReader(c.body))
			if c.authorization != "" {
				request.Header.Set("Authorization", "Bearer "+c.authorization)
			}
			code, err := func() (int, error) {
				_, code, err := handler(context.Background(), request)
				return code, err
			}()
			if code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (err: %v)", code, err)
			}
			if err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestPrepareDownloadRejectsBadQueries(t *testing.T) {
	t.Parallel()
	router := gin.New()
	router.Use(gin_mw.ErrorHandler(model.GetStatusCode, ", "))
	PrepareDownloadEndpoints(router, newDeps(nil, nil, nil, nil, nil, nil))

	cases := []struct {
		name  string
		query string
	}{
		// the handler must not add an error of its own after the 400, the last status wins
		{"invalid element returns 400", "{}"},
		{"element without time returns 400", `{"deviceId":"d","serviceId":"` + badRequestServiceId + `","columns":[{"name":"root.a"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/prepare-download?query="+url.QueryEscape(c.query), nil)
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
