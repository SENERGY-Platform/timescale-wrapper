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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gin_mw "github.com/SENERGY-Platform/gin-middleware"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type fakeVerifier struct {
	ok       bool
	owners   []string
	err      error
	calls    int
	gotUser  string
	gotToken string
}

func (f *fakeVerifier) VerifyAccess(_ context.Context, _ []model.QueriesRequestElement, token string, userId string) (bool, []string, error) {
	f.calls++
	f.gotUser, f.gotToken = userId, token
	return f.ok, f.owners, f.err
}

func helperToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("k"))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestVerifyRequest(t *testing.T) {
	t.Parallel()
	withSub := "Bearer " + helperToken(t, jwt.MapClaims{"sub": "user-1"})
	withoutSub := "Bearer " + helperToken(t, jwt.MapClaims{"name": "x"})
	elements := []model.QueriesRequestElement{{}}

	cases := []struct {
		name       string
		header     string
		verifier   *fakeVerifier
		wantStatus int
		wantCalls  int
	}{
		{"valid", withSub, &fakeVerifier{ok: true, owners: []string{"owner-1"}}, http.StatusOK, 1},
		{"missing token", "", &fakeVerifier{ok: true}, http.StatusBadRequest, 0},
		{"unparsable token", "Bearer kaputt", &fakeVerifier{ok: true}, http.StatusBadRequest, 0},
		{"token without sub", withoutSub, &fakeVerifier{ok: true}, http.StatusBadRequest, 0},
		{"denied", withSub, &fakeVerifier{ok: false}, http.StatusNotFound, 1},
		{"upstream error", withSub, &fakeVerifier{err: errors.New("permissions down")}, http.StatusInternalServerError, 1},
		{"upstream error with class", withSub, &fakeVerifier{err: errors.Join(errors.New("x"), model.ErrForbidden)}, http.StatusForbidden, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			if c.header != "" {
				request.Header.Set("Authorization", c.header)
			}
			userId, owners, err := verifyRequest(context.Background(), request, c.verifier, elements)
			if got := model.GetStatusCode(err); got != c.wantStatus {
				t.Fatalf("status %d, want %d (err: %v)", got, c.wantStatus, err)
			}
			if c.verifier.calls != c.wantCalls {
				t.Errorf("verifier called %d times, want %d", c.verifier.calls, c.wantCalls)
			}
			if err != nil {
				if got := countClasses(err); got != 1 {
					t.Errorf("error carries %d classes, want 1: %v", got, err)
				}
				if userId != "" || owners != nil {
					t.Errorf("results on error: %q %v", userId, owners)
				}
				return
			}
			if userId != "user-1" || len(owners) != 1 || owners[0] != "owner-1" {
				t.Errorf("got %q %v", userId, owners)
			}
			if c.verifier.gotUser != "user-1" || c.verifier.gotToken != c.header {
				t.Errorf("verifier got user %q token %q", c.verifier.gotUser, c.verifier.gotToken)
			}
		})
	}
}

func countClasses(err error) int {
	n := 0
	for _, class := range statusClasses {
		if errors.Is(err, class) {
			n++
		}
	}
	return n
}

func TestClassify(t *testing.T) {
	t.Parallel()
	plain := errors.New("plain")
	cases := []struct {
		name     string
		err      error
		fallback error
		wantNil  bool
		class    error
	}{
		{name: "nil", wantNil: true},
		{name: "plain gets fallback", err: plain, fallback: model.ErrBadRequest, class: model.ErrBadRequest},
		{name: "plain with nil fallback is internal", err: plain, class: model.ErrInternalServerError},
		{name: "bad request kept over internal fallback", err: errors.Join(plain, model.ErrBadRequest), fallback: model.ErrInternalServerError, class: model.ErrBadRequest},
		{name: "not found kept over internal fallback", err: fmt.Errorf("wrapped: %w", model.ErrNotFound), fallback: model.ErrInternalServerError, class: model.ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classify(c.err, c.fallback)
			if c.wantNil {
				if got != nil {
					t.Fatalf("got %v", got)
				}
				return
			}
			if !errors.Is(got, plain) && !errors.Is(got, c.err) {
				t.Errorf("lost the original error: %v", got)
			}
			if n := countClasses(got); n != 1 || !errors.Is(got, c.class) {
				t.Errorf("classes = %d, want exactly %v: %v", n, c.class, got)
			}
		})
	}

	t.Run("classified error is returned unchanged", func(t *testing.T) {
		err := errors.Join(plain, model.ErrBadRequest)
		if got := classify(err, model.ErrInternalServerError); got != err {
			t.Errorf("error was rewrapped: %v", got)
		}
	})
}

func TestClassifyDB(t *testing.T) {
	t.Parallel()
	if classifyDB(nil) != nil {
		t.Error("nil expected")
	}
	// not a PgError: GetHTTPErrorCode answers 500
	if got := model.GetStatusCode(classifyDB(errors.New("boom"))); got != http.StatusInternalServerError {
		t.Errorf("got %d", got)
	}
	if got := model.GetStatusCode(classifyDB(errors.Join(errors.New("x"), model.ErrBadRequest))); got != http.StatusBadRequest {
		t.Errorf("got %d", got)
	}
}

type failingWriter struct {
	*httptest.ResponseRecorder
}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestWriteJSON(t *testing.T) {
	// replaces log.Logger, so it does not run in parallel
	previous := log.Logger
	t.Cleanup(func() { log.Logger = previous })
	var logged bytes.Buffer
	log.Logger = slog.New(slog.NewTextHandler(&logged, nil))

	t.Run("writes the value with the content type", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		writeJSON(c, map[string]int{"a": 1})
		if got := recorder.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type %q", got)
		}
		if got := strings.TrimSpace(recorder.Body.String()); got != `{"a":1}` {
			t.Errorf("body %q", got)
		}
		if logged.Len() != 0 {
			t.Errorf("unexpected log: %s", logged.String())
		}
	})

	t.Run("logs a failed write", func(t *testing.T) {
		c, _ := gin.CreateTestContext(failingWriter{httptest.NewRecorder()})
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		writeJSON(c, map[string]int{"a": 1})
		if !strings.Contains(logged.String(), "broken pipe") || !strings.Contains(logged.String(), "level=ERROR") {
			t.Errorf("log: %q", logged.String())
		}
	})
}

// A request with a token that is no JWT or has no subject is a bad request, not an internal error.
func TestQueriesEndpointsRejectInvalidToken(t *testing.T) {
	t.Parallel()
	withoutSub := "Bearer " + helperToken(t, jwt.MapClaims{"name": "x"})
	body := `[{"deviceId":"d","serviceId":"` + badRequestServiceId + `","columns":[{"name":"root.a"}]}]`
	for _, endpoint := range []struct {
		path  string
		setup func(gin.IRouter)
	}{
		{"/queries", func(r gin.IRouter) { QueriesEndpoint(r, newDeps(nil, nil, nil, nil, nil, nil)) }},
		{"/queries/v2", func(r gin.IRouter) { QueriesV2Endpoint(r, newDeps(nil, nil, nil, nil, nil, nil)) }},
		{"/usage/devices", func(r gin.IRouter) { UsageEndpoint(r, newDeps(nil, nil, nil, nil, nil, nil)) }},
	} {
		router := gin.New()
		router.Use(gin_mw.ErrorHandler(model.GetStatusCode, ", "))
		endpoint.setup(router)
		reqBody := body
		if endpoint.path == "/usage/devices" {
			reqBody = `["d"]`
		}
		for name, header := range map[string]string{"garbage": "Bearer kaputt", "no sub": withoutSub, "none": ""} {
			t.Run(endpoint.path+" "+name, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(reqBody))
				if header != "" {
					request.Header.Set("Authorization", header)
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusBadRequest {
					t.Errorf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
				}
			})
		}
	}
}
