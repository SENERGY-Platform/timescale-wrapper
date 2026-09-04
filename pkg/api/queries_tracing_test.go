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
	"testing"

	"github.com/SENERGY-Platform/device-repository/lib/client"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// The cache fan-out of the batch endpoints must not scale its spans with the
// number of request elements, see pkg/tracing.
func TestQueriesGetFromCacheSpanCount(t *testing.T) {
	log.InitForTest()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider) // queriesGetFromCache uses the global provider

	deviceRepo, _, err := client.NewTestClient()
	if err != nil {
		t.Fatal(err)
	}

	requestElements := make([]model.QueriesRequestElement, 100)
	for i := range requestElements {
		// without a device id the element is not cachable, which is the path
		// that returns for every element without further calls
		requestElements[i] = model.QueriesRequestElement{
			Columns: []model.QueriesRequestElementColumn{{Name: "value"}},
		}
	}

	for _, testCase := range []struct {
		name            string
		detailedTracing bool
		expectedSpans   int
	}{
		{name: "batched", detailedTracing: false, expectedSpans: 1},
		{name: "detailed", detailedTracing: true, expectedSpans: 1 + len(requestElements)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			conf := configuration.ConfigStruct{DetailedTracing: testCase.detailedTracing}
			remoteCache := cache.NewRemote(&conf, deviceRepo, deviceSelection.NewTestClient())

			ctx, root := otel.Tracer("test").Start(context.Background(), "root")
			queriesGetFromCache(ctx, requestElements, remoteCache, &conf, nil)
			root.End()

			// only the spans of this trace, other tests share the provider
			count := 0
			for _, span := range exporter.GetSpans() {
				if span.SpanContext.TraceID() == root.SpanContext().TraceID() && span.Name != "root" {
					count++
				}
			}
			if count != testCase.expectedSpans {
				t.Errorf("expected %v spans below the request, got %v", testCase.expectedSpans, count)
			}
		})
	}
}
