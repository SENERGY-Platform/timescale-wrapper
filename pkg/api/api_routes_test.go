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
	"context"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/gin-gonic/gin"
)

// The routes of the two routers as they were registered before the endpoints took the deps struct. Each entry is
// "METHOD path". The order is not part of the contract, so both sides are compared sorted.
var authenticatedRoutesBefore = []string{
	"GET /doc",
	"GET /download",
	"GET /data-availability",
	"GET /prepare-download",
	"GET /last-message",
	"GET /raw-value",
	"POST /queries",
	"POST /queries/v2",
	"POST /usage/devices",
	"POST /usage/exports",
	"POST /prepare-download",
	"POST /last-values",
}

var unauthenticatedRoutesBefore = []string{
	"GET /download/:secret",
}

func TestRouterRoutes(t *testing.T) {
	// replaces log.Logger, so it does not run in parallel
	previous := log.Logger
	t.Cleanup(func() { log.Logger = previous })
	log.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	cases := []struct {
		name  string
		build func(ctx context.Context, config configuration.Config) (http.Handler, error)
		want  []string
	}{
		{"authenticated", func(ctx context.Context, config configuration.Config) (http.Handler, error) {
			return Router(ctx, config, nil, nil, nil, nil, nil)
		}, authenticatedRoutesBefore},
		{"unauthenticated", func(ctx context.Context, config configuration.Config) (http.Handler, error) {
			return UnauthenticatedRouter(ctx, config, nil, nil, nil, nil, nil)
		}, unauthenticatedRoutesBefore},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler, err := c.build(context.Background(), &configuration.ConfigStruct{})
			if err != nil {
				t.Fatal(err)
			}
			engine, ok := handler.(*gin.Engine)
			if !ok {
				t.Fatalf("router is %T, want *gin.Engine", handler)
			}
			var got []string
			for _, route := range engine.Routes() {
				got = append(got, route.Method+" "+route.Path)
			}
			want := slices.Clone(c.want)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("routes\ngot:  %q\nwant: %q", got, want)
			}
		})
	}
}
