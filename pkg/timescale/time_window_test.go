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
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestSQLBoundsAndTimeCondition(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []struct {
		name      string
		time      model.QueriesRequestElementTime
		start     string
		end       string
		condition string
	}{
		{
			name: "last", time: model.QueriesRequestElementTime{Last: s("1h")},
			start: "now() - interval '1h'", end: "now()",
			condition: `"time" > now() - interval '1h'`,
		},
		{
			name: "ahead", time: model.QueriesRequestElementTime{Ahead: s("2d")},
			start: "now()", end: "now() + interval '2d'",
			condition: `"time" > now() AND "time" < now() + interval '2d'`,
		},
		{
			name: "start end", time: model.QueriesRequestElementTime{Start: s("2024-01-01T00:00:00Z"), End: s("2024-01-02T00:00:00Z")},
			start: "'2024-01-01T00:00:00Z'::timestamptz", end: "'2024-01-02T00:00:00Z'::timestamptz",
			condition: `"time" > '2024-01-01T00:00:00Z' AND "time" < '2024-01-02T00:00:00Z'`,
		},
		{
			name: "quotes are doubled", time: model.QueriesRequestElementTime{Last: s("1h' OR '1'='1")},
			start: "now() - interval '1h'' OR ''1''=''1'", end: "now()",
			condition: `"time" > now() - interval '1h'' OR ''1''=''1'`,
		},
		{name: "incomplete", time: model.QueriesRequestElementTime{Start: s("2024-01-01T00:00:00Z")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end := sqlBounds(&c.time)
			if start != c.start || end != c.end {
				t.Errorf("got %q, %q; want %q, %q", start, end, c.start, c.end)
			}
			if c.condition != "" && timeCondition(&c.time) != c.condition {
				t.Errorf("got %q, want %q", timeCondition(&c.time), c.condition)
			}
		})
	}
}
