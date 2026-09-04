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

	"go.opentelemetry.io/otel/trace"
)

// SuppressChildren returns a context that marks its span context as not
// sampled. The span that is currently running keeps recording, but every span
// started below the returned context is created non-recording: our own spans,
// those of instrumented libraries such as otelpgx, and those of called
// services, because the traceparent header propagated from this context
// carries the cleared sampled flag as well.
//
// It exists for the fan-outs of the batch endpoints. Those start one span per
// request element and call cache, database and device repository once per
// element, so a single /queries/v2 call with a few hundred elements produced
// several thousand spans; traces of over 30.000 spans and 20 MB were the
// result, which is both the bulk of the tracing volume and too large for the
// query UI to load. The span above the fan-out carries the element counts
// instead, and errors are recorded on it, so what is lost is the per-element
// breakdown, not the fact that something failed.
//
// This relies on the default ParentBased(AlwaysSample) sampler, which drops a
// span whose parent is not sampled. gin-middleware/otelx configures no other
// sampler.
//
// detailed comes from the detailed_tracing config flag and switches the
// suppression off, which restores the per-element spans for debugging.
func SuppressChildren(ctx context.Context, detailed bool) context.Context {
	if detailed {
		return ctx
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ctx
	}
	return trace.ContextWithSpanContext(ctx, spanContext.WithTraceFlags(spanContext.TraceFlags().WithSampled(false)))
}
