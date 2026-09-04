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
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// The propagator and the tracer provider are process-wide and are installed
// by otelx's initialisation in a running service. Installed here because this
// test does not start one: without a propagator the injection is a no-op, and
// the test would pass while asserting nothing.
func init() {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// recordingServer answers an empty JSON array and keeps the request's headers.
func recordingServer(t *testing.T) (*httptest.Server, *http.Header) {
	t.Helper()
	received := &http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		*received = request.Header.Clone()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("[]"))
	}))
	t.Cleanup(server.Close)
	return server, received
}

// TestTheContextVariantCarriesTheTrace is the whole point of the variant: a
// read of the timescale wrapper appears in the trace of the request that
// caused it, rather than as a trace of its own with no idea who asked.
func TestTheContextVariantCarriesTheTrace(t *testing.T) {
	server, received := recordingServer(t)

	ctx, span := otel.Tracer("client_test").Start(context.Background(), "caller")
	defer span.End()
	member, err := baggage.NewMember("user_id", "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx = baggage.ContextWithBaggage(ctx, bag)

	if _, _, err = NewClient(server.URL).GetQueriesV2Context(ctx, "Bearer test", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	traceparent := received.Get("traceparent")
	if traceparent == "" {
		t.Fatal("no traceparent header reached the wrapper - the call would start a trace of its own")
	}
	if !strings.Contains(traceparent, span.SpanContext().TraceID().String()) {
		t.Errorf("traceparent %q does not name the caller's trace %v", traceparent, span.SpanContext().TraceID())
	}
	// The baggage is the other half: it is what puts the caller's user id on
	// the wrapper's own log lines and spans.
	if got := received.Get("baggage"); !strings.Contains(got, "user_id") {
		t.Errorf("baggage %q does not carry the caller's user_id", got)
	}
	if got := received.Get("Authorization"); got != "Bearer test" {
		t.Errorf("authorization: got %q", got)
	}
}

// TestTheUsageVariantsCarryItToo, so the trace does not depend on which call
// a service happens to make.
func TestTheUsageVariantsCarryItToo(t *testing.T) {
	server, received := recordingServer(t)

	ctx, span := otel.Tracer("client_test").Start(context.Background(), "caller")
	defer span.End()

	client := NewClient(server.URL)
	for _, call := range []struct {
		name string
		run  func() error
	}{
		{name: "devices", run: func() error {
			_, _, err := client.GetDeviceUsageContext(ctx, "Bearer test", []string{"d1"})
			return err
		}},
		{name: "exports", run: func() error {
			_, _, err := client.GetExportUsageContext(ctx, "Bearer test", []string{"e1"})
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			if err := call.run(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(received.Get("traceparent"), span.SpanContext().TraceID().String()) {
				t.Errorf("traceparent %q does not name the caller's trace", received.Get("traceparent"))
			}
		})
	}
}

// TestTheVariantWithoutAContextCarriesNothing states the difference rather
// than leaving it to be inferred.
//
// It is context.TODO() behind those methods, so there is no trace to inject
// and nothing to cancel. They exist so that callers written before this
// change keep building.
func TestTheVariantWithoutAContextCarriesNothing(t *testing.T) {
	server, received := recordingServer(t)

	if _, _, err := NewClient(server.URL).GetQueriesV2("Bearer test", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := received.Get("traceparent"); got != "" {
		t.Errorf("traceparent: got %q, want none - a context.TODO() has no trace to pass on", got)
	}
}

// TestACancelledCallerAbortsTheCallInFlight is the second half of what the
// context buys, and the half permissions-v2 does not have: its client injects
// the trace but builds the request without the context, so a caller's timeout
// bounds nothing.
//
// Worth a test rather than a comment, because the failure is invisible - a
// call that ignores a cancellation just takes as long as it takes, and the
// caller is gone by then.
func TestACancelledCallerAbortsTheCallInFlight(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(arrived)
		<-release
		_, _ = writer.Write([]byte("[]"))
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()

	started := time.Now()
	_, _, err := NewClient(server.URL).GetQueriesV2Context(ctx, "Bearer test", nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: got %v, want a cancellation", err)
	}
	// The handler is still blocked, so anything that returned this quickly
	// returned because the context said so.
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("the call took %v - it waited for the server rather than for its context", took)
	}
}
