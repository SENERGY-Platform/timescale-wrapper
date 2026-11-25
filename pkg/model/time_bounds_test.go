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
	"testing"
	"time"
)

func TestQueriesRequestElementTimeBounds(t *testing.T) {
	now := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
	s := func(v string) *string { return &v }
	cases := []struct {
		name   string
		time   *QueriesRequestElementTime
		start  time.Time
		end    time.Time
		err    bool
		badReq bool
	}{
		{name: "nil", err: true, badReq: true},
		{name: "last", time: &QueriesRequestElementTime{Last: s("2h")}, start: now.Add(-2 * time.Hour), end: now},
		{name: "ahead", time: &QueriesRequestElementTime{Ahead: s("30m")}, start: now, end: now.Add(30 * time.Minute)},
		{name: "start end", time: &QueriesRequestElementTime{Start: s("2024-01-01T00:00:00Z"), End: s("2024-01-15T06:00:00+01:00")},
			start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), end: time.Date(2024, 1, 15, 5, 0, 0, 0, time.UTC)},
		{name: "only start", time: &QueriesRequestElementTime{Start: s("2024-01-01T00:00:00Z")}, err: true, badReq: true},
		{name: "only end", time: &QueriesRequestElementTime{End: s("2024-01-01T00:00:00Z")}, err: true, badReq: true},
		{name: "empty", time: &QueriesRequestElementTime{}, err: true, badReq: true},
		{name: "bad start", time: &QueriesRequestElementTime{Start: s("2024-01-01"), End: s("2024-01-15T06:00:00Z")}, err: true, badReq: true},
		{name: "bad end", time: &QueriesRequestElementTime{Start: s("2024-01-01T00:00:00Z"), End: s("x")}, err: true, badReq: true},
		{name: "invalid last", time: &QueriesRequestElementTime{Last: s("2 hours ago")}, err: true},
		{name: "invalid ahead", time: &QueriesRequestElementTime{Ahead: s("")}, err: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end, err := c.time.Bounds(now)
			if c.err {
				if err == nil {
					t.Fatal("expected an error")
				}
				if errors.Is(err, ErrBadRequest) != c.badReq {
					t.Errorf("ErrBadRequest = %v, want %v: %v", errors.Is(err, ErrBadRequest), c.badReq, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !start.Equal(c.start) || !end.Equal(c.end) {
				t.Errorf("got [%v, %v), want [%v, %v)", start, end, c.start, c.end)
			}
		})
	}
}
