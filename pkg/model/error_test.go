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

package model

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestGetStatusCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: http.StatusOK},
		{name: "bad request", err: ErrBadRequest, want: http.StatusBadRequest},
		{name: "internal server error", err: ErrInternalServerError, want: http.StatusInternalServerError},
		{name: "not found", err: ErrNotFound, want: http.StatusNotFound},
		{name: "forbidden", err: ErrForbidden, want: http.StatusForbidden},
		{name: "no content", err: ErrNoContent, want: http.StatusNoContent},
		{name: "joined bad request", err: errors.Join(errors.New("x"), ErrBadRequest), want: http.StatusBadRequest},
		{name: "wrapped not found", err: fmt.Errorf("%w", ErrNotFound), want: http.StatusNotFound},
		{name: "unknown error", err: errors.New("unknown"), want: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetStatusCode(tt.err); got != tt.want {
				t.Errorf("GetStatusCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestGetError(t *testing.T) {
	tests := []struct {
		name string
		code int
		want error
	}{
		{name: "ok", code: http.StatusOK, want: nil},
		{name: "bad request", code: http.StatusBadRequest, want: ErrBadRequest},
		{name: "internal server error", code: http.StatusInternalServerError, want: ErrInternalServerError},
		{name: "not found", code: http.StatusNotFound, want: ErrNotFound},
		{name: "forbidden", code: http.StatusForbidden, want: ErrForbidden},
		{name: "no content", code: http.StatusNoContent, want: ErrNoContent},
		{name: "unmapped code falls back to internal server error", code: http.StatusTeapot, want: ErrInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetError(tt.code)
			if tt.want == nil {
				if got != nil {
					t.Errorf("GetError(%d) = %v, want nil", tt.code, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Errorf("GetError(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}

	t.Run("roundtrip", func(t *testing.T) {
		codes := []int{
			http.StatusBadRequest,
			http.StatusInternalServerError,
			http.StatusNotFound,
			http.StatusForbidden,
			http.StatusNoContent,
		}
		for _, code := range codes {
			if got := GetStatusCode(GetError(code)); got != code {
				t.Errorf("GetStatusCode(GetError(%d)) = %d, want %d", code, got, code)
			}
		}
	})
}
