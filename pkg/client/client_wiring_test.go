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

package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTheClientHasItsOwnTimeout(t *testing.T) {
	if httpClient == http.DefaultClient {
		t.Fatal("the client must not share http.DefaultClient")
	}
	if httpClient.Timeout != 30*time.Second {
		t.Fatalf("timeout = %v, want 30s", httpClient.Timeout)
	}
}

// A server that never answers must end the call at the client's timeout.
// The request context of the stalled handler ends when the client closes the
// connection, so the handler returns and the server can close.
func TestACallEndsAtTheClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The server notices a closed connection only once the body is read.
		_, _ = io.Copy(io.Discard, request.Body)
		select {
		case <-request.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(server.Close)

	saved := httpClient
	httpClient = &http.Client{Timeout: 50 * time.Millisecond}
	t.Cleanup(func() { httpClient = saved })

	start := time.Now()
	_, code, err := NewClient(server.URL).GetDeviceUsage("token", []string{"device"})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", code)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("call took %v, the timeout did not apply", elapsed)
	}
}

// The sentinels shared with pkg/model match on either side.
func TestSentinelMessages(t *testing.T) {
	if got := ErrNotFound.Error(); got != "not found" {
		t.Errorf("ErrNotFound = %q", got)
	}
	if got := ErrBadRequest.Error(); got != "bad request" {
		t.Errorf("ErrBadRequest = %q", got)
	}
}
