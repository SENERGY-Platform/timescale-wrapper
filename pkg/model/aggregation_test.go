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

import "testing"

func TestParseAggregation(t *testing.T) {
	funcs := []string{"first", "last", "min", "max", "count", "mean", "sum", "median"}
	for _, f := range funcs {
		got, ok := ParseAggregation(f)
		if !ok || got.Kind != Plain || got.Function != f || got.String() != f {
			t.Errorf("%q: got %+v ok=%v", f, got, ok)
		}
		d := "difference-" + f
		got, ok = ParseAggregation(d)
		if !ok || got.Kind != Difference || got.Function != f || got.String() != d {
			t.Errorf("%q: got %+v ok=%v", d, got, ok)
		}
	}
	for _, tw := range []string{"time-weighted-mean-linear", "time-weighted-mean-locf"} {
		got, ok := ParseAggregation(tw)
		if !ok || got.Kind != TimeWeighted || got.Function != tw || got.String() != tw {
			t.Errorf("%q: got %+v ok=%v", tw, got, ok)
		}
	}
}

func TestParseAggregationRejects(t *testing.T) {
	for _, in := range []string{"", "difference", "difference-", "Mean", "time-weighted-mean", "time-weighted-", "difference-difference-last", "difference-time-weighted-mean-linear", "avg", " mean", "mean "} {
		if got, ok := ParseAggregation(in); ok {
			t.Errorf("%q accepted: %+v", in, got)
		}
	}
}

func TestColumnAggregation(t *testing.T) {
	if _, ok := (QueriesRequestElementColumn{}).Aggregation(); ok {
		t.Error("nil GroupType must not parse")
	}
	g := "difference-max"
	got, ok := QueriesRequestElementColumn{GroupType: &g}.Aggregation()
	if !ok || got.Kind != Difference || got.Function != "max" {
		t.Errorf("got %+v ok=%v", got, ok)
	}
	bad := "nope"
	if _, ok := (QueriesRequestElementColumn{GroupType: &bad}).Aggregation(); ok {
		t.Error("invalid GroupType must not parse")
	}
}
