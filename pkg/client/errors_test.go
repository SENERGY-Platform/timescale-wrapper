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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestGetErrCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil is OK", err: nil, want: http.StatusOK},
		{name: "bad request", err: ErrBadRequest, want: http.StatusBadRequest},
		{name: "access denied", err: ErrAccessDenied, want: http.StatusForbidden},
		{name: "not found", err: ErrNotFound, want: http.StatusNotFound},
		{name: "invalid auth", err: ErrInvalidAuth, want: http.StatusUnauthorized},
		{name: "wrapped bad request", err: fmt.Errorf("query: %w", ErrBadRequest), want: http.StatusBadRequest},
		{name: "wrapped not found", err: fmt.Errorf("device: %w", ErrNotFound), want: http.StatusNotFound},
		{name: "unknown error", err: errors.New("boom"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetErrCode(tt.err); got != tt.want {
				t.Errorf("GetErrCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestGetErrFromCode(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		msg      string
		wantIs   error
		wantText string
		wantNil  bool
	}{
		{name: "below 300 is no error", code: http.StatusOK, msg: "boom", wantNil: true},
		{name: "204 is no error", code: http.StatusNoContent, msg: "", wantNil: true},
		{name: "bad request", code: http.StatusBadRequest, msg: "boom", wantIs: ErrBadRequest, wantText: "boom"},
		{name: "access denied", code: http.StatusForbidden, msg: "boom", wantIs: ErrAccessDenied, wantText: "boom"},
		{name: "not found", code: http.StatusNotFound, msg: "boom", wantIs: ErrNotFound, wantText: "boom"},
		{name: "invalid auth", code: http.StatusUnauthorized, msg: "boom", wantIs: ErrInvalidAuth, wantText: "boom"},
		{name: "server error keeps message", code: http.StatusInternalServerError, msg: "boom", wantText: "boom"},
		{name: "empty message uses status code", code: http.StatusServiceUnavailable, msg: "", wantText: "received status code 503"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := GetErrFromCode(tt.code, tt.msg)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("GetErrFromCode(%d, %q) = %v, want nil", tt.code, tt.msg, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("GetErrFromCode(%d, %q) = nil, want error", tt.code, tt.msg)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("errors.Is(%v, %v) = false, want true", err, tt.wantIs)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("error text %q does not contain %q", err.Error(), tt.wantText)
			}
		})
	}
}

func TestGetErrFromCodeRoundTrip(t *testing.T) {
	for _, code := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
	} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			if got := GetErrCode(GetErrFromCode(code, "x")); got != code {
				t.Errorf("GetErrCode(GetErrFromCode(%d, \"x\")) = %d, want %d", code, got, code)
			}
		})
	}
}

// The server's message can already start with the sentinel text. The prefix
// must appear once in the error, not twice.
func TestGetErrFromCodeStripsPrefix(t *testing.T) {
	tests := []struct {
		name string
		code int
		msg  string
		want string
	}{
		{name: "bad request", code: http.StatusBadRequest, msg: "bad request: invalid query", want: "bad request: invalid query"},
		{name: "access denied", code: http.StatusForbidden, msg: "access denied: x", want: "access denied: x"},
		{name: "invalid auth", code: http.StatusUnauthorized, msg: "invalid auth token: x", want: "invalid auth token: x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetErrFromCode(tt.code, tt.msg).Error(); got != tt.want {
				t.Errorf("GetErrFromCode(%d, %q).Error() = %q, want %q", tt.code, tt.msg, got, tt.want)
			}
		})
	}
}
