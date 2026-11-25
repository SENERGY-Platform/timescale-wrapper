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

package model

import "testing"

func TestQueriesRequestElementTimeValid(t *testing.T) {
	tests := []struct {
		name string
		time QueriesRequestElementTime
		want bool
	}{
		{name: "last only", time: QueriesRequestElementTime{Last: ptr("1h")}, want: true},
		{name: "last with start rejected", time: QueriesRequestElementTime{Last: ptr("1h"), Start: ptr("2021-01-01T00:00:00Z")}, want: false},
		{name: "last with ahead rejected", time: QueriesRequestElementTime{Last: ptr("1h"), Ahead: ptr("2d")}, want: false},
		{name: "last invalid interval rejected", time: QueriesRequestElementTime{Last: ptr("abc")}, want: false},
		{name: "ahead only", time: QueriesRequestElementTime{Ahead: ptr("2d")}, want: true},
		{name: "ahead with end rejected", time: QueriesRequestElementTime{Ahead: ptr("2d"), End: ptr("2021-01-01T00:00:00Z")}, want: false},
		{name: "ahead invalid interval rejected", time: QueriesRequestElementTime{Ahead: ptr("abc")}, want: false},
		{name: "start and end RFC3339", time: QueriesRequestElementTime{Start: ptr("2021-01-01T00:00:00Z"), End: ptr("2021-01-02T00:00:00Z")}, want: true},
		{name: "start only rejected", time: QueriesRequestElementTime{Start: ptr("2021-01-01T00:00:00Z")}, want: false},
		{name: "end only rejected", time: QueriesRequestElementTime{End: ptr("2021-01-02T00:00:00Z")}, want: false},
		{name: "empty element rejected", time: QueriesRequestElementTime{}, want: false},
		{name: "start not RFC3339 rejected", time: QueriesRequestElementTime{Start: ptr("2021-01-01"), End: ptr("2021-01-02T00:00:00Z")}, want: false},
		{name: "end not RFC3339 rejected", time: QueriesRequestElementTime{Start: ptr("2021-01-01T00:00:00Z"), End: ptr("2021-01-02")}, want: false},
		{name: "last 10ms", time: QueriesRequestElementTime{Last: ptr("10ms")}, want: true},
		{name: "last 1 mon", time: QueriesRequestElementTime{Last: ptr("1 mon")}, want: true},
		{name: "last H:MM:SS", time: QueriesRequestElementTime{Last: ptr("01:00:00")}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.time.Valid()
			if got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueriesRequestElementTimeCopy(t *testing.T) {
	t.Run("all fields copied with distinct pointers", func(t *testing.T) {
		orig := QueriesRequestElementTime{
			Last:  ptr("1h"),
			Ahead: ptr("2d"),
			Start: ptr("2021-01-01T00:00:00Z"),
			End:   ptr("2021-01-02T00:00:00Z"),
		}
		cpy := orig.Copy()

		if cpy.Last == nil || *cpy.Last != "1h" {
			t.Errorf("Last = %v, want 1h", cpy.Last)
		}
		if cpy.Ahead == nil || *cpy.Ahead != "2d" {
			t.Errorf("Ahead = %v, want 2d", cpy.Ahead)
		}
		if cpy.Start == nil || *cpy.Start != "2021-01-01T00:00:00Z" {
			t.Errorf("Start = %v, want 2021-01-01T00:00:00Z", cpy.Start)
		}
		if cpy.End == nil || *cpy.End != "2021-01-02T00:00:00Z" {
			t.Errorf("End = %v, want 2021-01-02T00:00:00Z", cpy.End)
		}

		if cpy.Last == orig.Last || cpy.Ahead == orig.Ahead || cpy.Start == orig.Start || cpy.End == orig.End {
			t.Error("copy shares pointers with original")
		}

		*orig.Last = "changed"
		*orig.Ahead = "changed"
		*orig.Start = "changed"
		*orig.End = "changed"
		if *cpy.Last != "1h" || *cpy.Ahead != "2d" || *cpy.Start != "2021-01-01T00:00:00Z" || *cpy.End != "2021-01-02T00:00:00Z" {
			t.Errorf("changing the original changed the copy: %q %q %q %q", *cpy.Last, *cpy.Ahead, *cpy.Start, *cpy.End)
		}
	})

	t.Run("nil fields stay nil", func(t *testing.T) {
		orig := QueriesRequestElementTime{Last: ptr("1h")}
		cpy := orig.Copy()
		if cpy.Ahead != nil || cpy.Start != nil || cpy.End != nil {
			t.Errorf("nil fields became non-nil: ahead=%v start=%v end=%v", cpy.Ahead, cpy.Start, cpy.End)
		}
	})

	t.Run("EndOriginal is not copied", func(t *testing.T) {
		orig := QueriesRequestElementTime{
			Start:       ptr("2021-01-01T00:00:00Z"),
			End:         ptr("2021-01-02T00:00:00Z"),
			EndOriginal: ptr("2021-01-02T02:00:00+02:00"),
		}
		cpy := orig.Copy()
		if cpy.EndOriginal != nil {
			t.Errorf("EndOriginal = %q, want nil", *cpy.EndOriginal)
		}
	})
}
