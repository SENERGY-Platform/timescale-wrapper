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

package tracing

import (
	"context"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestSuppressChildren(t *testing.T) {
	t.Run("children of the suppressed context are not exported", func(t *testing.T) {
		exporter, tracer := setup()

		ctx, parent := tracer.Start(context.Background(), "batch")
		for i := 0; i < 10; i++ {
			_, element := tracer.Start(SuppressChildren(ctx, false), "element")
			element.End()
		}
		parent.End()

		assertSpans(t, exporter, "batch")
	})

	t.Run("detailed keeps the per element spans", func(t *testing.T) {
		exporter, tracer := setup()

		ctx, parent := tracer.Start(context.Background(), "batch")
		_, element := tracer.Start(SuppressChildren(ctx, true), "element")
		element.End()
		parent.End()

		assertSpans(t, exporter, "element", "batch")
	})

	t.Run("grandchildren stay suppressed", func(t *testing.T) {
		exporter, tracer := setup()

		ctx, parent := tracer.Start(context.Background(), "batch")
		elementCtx, element := tracer.Start(SuppressChildren(ctx, false), "element")
		// stands for the spans of otelpgx below our own call
		_, query := tracer.Start(elementCtx, "query")
		query.End()
		element.End()
		parent.End()

		assertSpans(t, exporter, "batch")
	})

	t.Run("without a running span the context is unchanged", func(t *testing.T) {
		exporter, tracer := setup()

		_, root := tracer.Start(SuppressChildren(context.Background(), false), "root")
		root.End()

		assertSpans(t, exporter, "root")
	})

	t.Run("called services are told not to sample", func(t *testing.T) {
		_, tracer := setup()
		propagator := propagation.TraceContext{}

		ctx, parent := tracer.Start(context.Background(), "batch")
		defer parent.End()

		request := httptest.NewRequest("GET", "http://device-repository/devices/1", nil)
		propagator.Inject(ctx, propagation.HeaderCarrier(request.Header))
		sampled := request.Header.Get("traceparent")

		request = httptest.NewRequest("GET", "http://device-repository/devices/1", nil)
		propagator.Inject(SuppressChildren(ctx, false), propagation.HeaderCarrier(request.Header))
		suppressed := request.Header.Get("traceparent")

		if len(sampled) == 0 || sampled[len(sampled)-2:] != "01" {
			t.Fatalf("expected the unsuppressed traceparent to be sampled, got %v", sampled)
		}
		if len(suppressed) == 0 || suppressed[len(suppressed)-2:] != "00" {
			t.Errorf("expected the suppressed traceparent to be unsampled, got %v", suppressed)
		}
	})
}

func setup() (*tracetest.InMemoryExporter, trace.Tracer) {
	exporter := tracetest.NewInMemoryExporter()
	// default sampler, as gin-middleware/otelx configures it
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	return exporter, provider.Tracer("pkg/tracing")
}

func assertSpans(t *testing.T, exporter *tracetest.InMemoryExporter, expected ...string) {
	t.Helper()
	actual := []string{}
	for _, span := range exporter.GetSpans() {
		actual = append(actual, span.Name)
	}
	if len(actual) != len(expected) {
		t.Fatalf("expected spans %v, got %v", expected, actual)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Errorf("expected span %v at index %v, got %v", expected[i], i, actual[i])
		}
	}
}
