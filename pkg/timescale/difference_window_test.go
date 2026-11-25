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

package timescale

import (
	"reflect"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestWidenTimeWindowForDifferenceUnits(t *testing.T) {
	const startS = "2024-03-10T00:00:00Z"
	const endS = "2024-03-12T00:00:00Z"

	deref := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}
	diffElement := func(groupTime string) *model.QueriesRequestElement {
		return &model.QueriesRequestElement{
			Columns:   []model.QueriesRequestElementColumn{{GroupType: strPtr("difference-last")}},
			Time:      &model.QueriesRequestElementTime{Start: strPtr(startS), End: strPtr(endS)},
			GroupTime: strPtr(groupTime),
		}
	}

	// The window spans 48 hours. wantStart is the start after widening by one unit, formatted as
	// RFC3339, which keeps whole seconds only: a 1ms step therefore lands on 23:59:59.
	tests := []struct {
		groupTime   string
		wantStart   string
		wantBuckets int
	}{
		{groupTime: "1ms", wantStart: "2024-03-09T23:59:59Z", wantBuckets: 172800000},
		{groupTime: "1s", wantStart: "2024-03-09T23:59:59Z", wantBuckets: 172800},
		{groupTime: "1m", wantStart: "2024-03-09T23:59:00Z", wantBuckets: 2880},
		{groupTime: "1h", wantStart: "2024-03-09T23:00:00Z", wantBuckets: 48},
		{groupTime: "1d", wantStart: "2024-03-09T00:00:00Z", wantBuckets: 2},
		{groupTime: "1day", wantStart: "2024-03-09T00:00:00Z", wantBuckets: 2},
		{groupTime: "1w", wantStart: "2024-03-03T00:00:00Z", wantBuckets: 0},
		{groupTime: "1mon", wantStart: "2024-02-10T00:00:00Z", wantBuckets: 0},
		{groupTime: "1months", wantStart: "2024-02-10T00:00:00Z", wantBuckets: 0},
		{groupTime: "1y", wantStart: "2023-03-10T00:00:00Z", wantBuckets: 0},
	}
	for _, tc := range tests {
		t.Run(tc.groupTime, func(t *testing.T) {
			element := diffElement(tc.groupTime)
			origTime := element.Time

			buckets, err := widenTimeWindowForDifference(element)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buckets == nil {
				t.Fatalf("buckets is nil, want %d", tc.wantBuckets)
			}
			if *buckets != tc.wantBuckets {
				t.Errorf("buckets = %d, want %d", *buckets, tc.wantBuckets)
			}
			if got := deref(element.Time.Start); got != tc.wantStart {
				t.Errorf("Time.Start = %s, want %s", got, tc.wantStart)
			}
			if got := deref(element.Time.End); got != endS {
				t.Errorf("Time.End = %s, want %s", got, endS)
			}
			if got := deref(element.Time.EndOriginal); got != endS {
				t.Errorf("Time.EndOriginal = %s, want %s", got, endS)
			}

			if element.Time == origTime {
				t.Errorf("element.Time still points to the caller's time object")
			}
			if got := deref(origTime.Start); got != startS {
				t.Errorf("caller's Time.Start changed to %s, want %s", got, startS)
			}
			if got := deref(origTime.End); got != endS {
				t.Errorf("caller's Time.End changed to %s, want %s", got, endS)
			}
			if origTime.EndOriginal != nil {
				t.Errorf("caller's Time.EndOriginal set to %s, want nil", *origTime.EndOriginal)
			}
		})
	}
}

func TestWidenTimeWindowForDifferenceUnitErrors(t *testing.T) {
	const startS = "2024-03-10T00:00:00Z"
	const endS = "2024-03-12T00:00:00Z"

	tests := []struct {
		name      string
		start     string
		end       string
		groupTime string
	}{
		{name: "group time without unit", start: startS, end: endS, groupTime: "15"},
		{name: "start not RFC3339", start: "2024-03-10", end: endS, groupTime: "1h"},
		{name: "end not RFC3339", start: startS, end: "12.03.2024", groupTime: "1h"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			element := &model.QueriesRequestElement{
				Columns:   []model.QueriesRequestElementColumn{{GroupType: strPtr("difference-last")}},
				Time:      &model.QueriesRequestElementTime{Start: strPtr(tc.start), End: strPtr(tc.end)},
				GroupTime: strPtr(tc.groupTime),
			}

			buckets, err := widenTimeWindowForDifference(element)
			if err == nil {
				t.Errorf("expected error, got nil")
			}
			if buckets != nil {
				t.Errorf("buckets = %d, want nil", *buckets)
			}
		})
	}
}

func TestWidenTimeWindowForDifferenceRelative(t *testing.T) {
	deref := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}

	tests := []struct {
		name        string
		time        model.QueriesRequestElementTime
		wantLast    string
		wantAhead   string
		wantBuckets int
	}{
		{name: "last", time: model.QueriesRequestElementTime{Last: strPtr("3h")}, wantLast: "4h", wantAhead: "<nil>", wantBuckets: 3},
		{name: "ahead", time: model.QueriesRequestElementTime{Ahead: strPtr("2d")}, wantLast: "<nil>", wantAhead: "3d", wantBuckets: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			timeObj := tc.time
			element := &model.QueriesRequestElement{
				Columns:   []model.QueriesRequestElementColumn{{GroupType: strPtr("difference-first")}},
				Time:      &timeObj,
				GroupTime: strPtr("1h"),
			}

			buckets, err := widenTimeWindowForDifference(element)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buckets == nil {
				t.Fatalf("buckets is nil, want %d", tc.wantBuckets)
			}
			if *buckets != tc.wantBuckets {
				t.Errorf("buckets = %d, want %d", *buckets, tc.wantBuckets)
			}
			if got := deref(element.Time.Last); got != tc.wantLast {
				t.Errorf("Time.Last = %s, want %s", got, tc.wantLast)
			}
			if got := deref(element.Time.Ahead); got != tc.wantAhead {
				t.Errorf("Time.Ahead = %s, want %s", got, tc.wantAhead)
			}
			if element.Time == &timeObj {
				t.Errorf("element.Time still points to the caller's time object")
			}
			if got := deref(timeObj.Last); got != deref(tc.time.Last) {
				t.Errorf("caller's Time.Last changed to %s", got)
			}
			if got := deref(timeObj.Ahead); got != deref(tc.time.Ahead) {
				t.Errorf("caller's Time.Ahead changed to %s", got)
			}
		})
	}
}

func TestWidenTimeWindowForDifferenceNoop(t *testing.T) {
	const startS = "2024-03-10T00:00:00Z"
	const endS = "2024-03-12T00:00:00Z"

	validTime := func() *model.QueriesRequestElementTime {
		return &model.QueriesRequestElementTime{Start: strPtr(startS), End: strPtr(endS)}
	}

	tests := []struct {
		name    string
		element func() *model.QueriesRequestElement
	}{
		{
			name: "no difference column",
			element: func() *model.QueriesRequestElement {
				return &model.QueriesRequestElement{
					Columns: []model.QueriesRequestElementColumn{
						{GroupType: strPtr("last")},
						{GroupType: nil},
					},
					Time:      validTime(),
					GroupTime: strPtr("1h"),
				}
			},
		},
		{
			name: "time nil",
			element: func() *model.QueriesRequestElement {
				return &model.QueriesRequestElement{
					Columns:   []model.QueriesRequestElementColumn{{GroupType: strPtr("difference-last")}},
					Time:      nil,
					GroupTime: strPtr("1h"),
				}
			},
		},
		{
			name: "start and end without group time",
			element: func() *model.QueriesRequestElement {
				return &model.QueriesRequestElement{
					Columns:   []model.QueriesRequestElementColumn{{GroupType: strPtr("difference-last")}},
					Time:      validTime(),
					GroupTime: nil,
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			element := tc.element()
			want := tc.element()
			timeBefore := element.Time

			buckets, err := widenTimeWindowForDifference(element)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buckets != nil {
				t.Errorf("buckets = %d, want nil", *buckets)
			}
			if element.Time != timeBefore {
				t.Errorf("element.Time pointer changed")
			}
			if !reflect.DeepEqual(element, want) {
				t.Errorf("element changed: got %+v, want %+v", element, want)
			}
		})
	}
}
