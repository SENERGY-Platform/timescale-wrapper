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
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestGetUserId(t *testing.T) {
	mkToken := func(t *testing.T, claims jwt.MapClaims) string {
		t.Helper()
		tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("k"))
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	withSub := mkToken(t, jwt.MapClaims{"sub": "user-1"})
	withoutSub := mkToken(t, jwt.MapClaims{"name": "ohne-sub"})

	tests := []struct {
		name    string
		header  string
		want    string
		wantErr bool
	}{
		{name: "Bearer-Praefix", header: "Bearer " + withSub, want: "user-1"},
		{name: "kleingeschriebener Praefix", header: "bearer " + withSub, want: "user-1"},
		{name: "grossgeschriebener Praefix", header: "BEARER " + withSub, want: "user-1"},
		{name: "Token ohne Praefix", header: withSub, want: "user-1"},
		{name: "kein Header", header: "", wantErr: true},
		{name: "kaputtes Token", header: "Bearer kaputt", wantErr: true},
		{name: "Token ohne sub", header: "Bearer " + withoutSub, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			got, err := getUserId(req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("getUserId() error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("getUserId() = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("getToken gibt den Header unveraendert zurueck", func(t *testing.T) {
		for _, h := range []string{"Bearer " + withSub, "bearer x", "kaputt", ""} {
			req := httptest.NewRequest("GET", "/", nil)
			if h != "" {
				req.Header.Set("Authorization", h)
			}
			if got := getToken(req); got != h {
				t.Errorf("getToken() = %q, want %q", got, h)
			}
		}
	})
}

func TestTokenValid(t *testing.T) {
	tests := []struct {
		name    string
		token   Token
		wantErr bool
	}{
		{name: "leerer sub", token: Token{}, wantErr: true},
		{name: "gesetzter sub", token: Token{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}}, wantErr: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tok := tc.token
			err := tok.Valid()
			if (err != nil) != tc.wantErr {
				t.Errorf("Valid() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
