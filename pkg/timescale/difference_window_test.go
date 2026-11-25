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
	"strings"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestDifferenceWindowFor(t *testing.T) {
	const startS = "2024-03-10T00:00:00Z"
	const endS = "2024-03-12T00:00:00Z"
	diff := model.QueriesRequestElementColumn{Name: "a", GroupType: strPtr("difference-last")}
	plain := model.QueriesRequestElementColumn{Name: "b", GroupType: strPtr("last")}

	tests := []struct {
		name      string
		time      *model.QueriesRequestElementTime
		columns   []model.QueriesRequestElementColumn
		groupTime string
		// wantFirst is the first requested bucket, empty if the window is not widened
		wantFirst string
	}{
		{
			name: "last", time: &model.QueriesRequestElementTime{Last: strPtr("1d")}, columns: []model.QueriesRequestElementColumn{diff}, groupTime: "1h",
			wantFirst: "time_bucket('1h', now() - interval '1d', 'Europe/Berlin')",
		},
		{
			name: "ahead", time: &model.QueriesRequestElementTime{Ahead: strPtr("2d")}, columns: []model.QueriesRequestElementColumn{diff, plain}, groupTime: "6h",
			wantFirst: "time_bucket('6h', now(), 'Europe/Berlin')",
		},
		{
			name: "start and end", time: &model.QueriesRequestElementTime{Start: strPtr(startS), End: strPtr(endS)}, columns: []model.QueriesRequestElementColumn{diff}, groupTime: "1mon",
			wantFirst: "time_bucket('1mon', '2024-03-10T00:00:00Z'::timestamptz, 'Europe/Berlin')",
		},
		{name: "no difference column", time: &model.QueriesRequestElementTime{Last: strPtr("1d")}, columns: []model.QueriesRequestElementColumn{plain}, groupTime: "1h"},
		{name: "no time window", columns: []model.QueriesRequestElementColumn{diff}, groupTime: "1h"},
		{name: "incomplete time window", time: &model.QueriesRequestElementTime{Start: strPtr(startS)}, columns: []model.QueriesRequestElementColumn{diff}, groupTime: "1h"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			element := model.QueriesRequestElement{Columns: tc.columns, Time: tc.time, GroupTime: strPtr(tc.groupTime)}
			query, err := newElementQuery(element, "t", "Europe/Berlin")
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantFirst == "" {
				if query.difference != nil {
					t.Fatalf("unexpected difference window %+v", *query.difference)
				}
				return
			}
			if query.difference == nil {
				t.Fatal("no difference window")
			}
			if query.difference.firstBucket != tc.wantFirst {
				t.Errorf("firstBucket = %s, want %s", query.difference.firstBucket, tc.wantFirst)
			}
			wantStart := "time_bucket('" + tc.groupTime + "', " + tc.wantFirst + " - interval '1 microsecond', 'Europe/Berlin')"
			if query.difference.start != wantStart {
				t.Errorf("start = %s, want %s", query.difference.start, wantStart)
			}
			// the end of the window is not moved
			where, err := getWhereString(query.element, query.difference.start)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(where, " WHERE \"time\" >= "+wantStart) {
				t.Errorf("where = %s", where)
			}
			if tc.time.Start != nil && !strings.HasSuffix(where, " AND \"time\" < '"+endS+"'") {
				t.Errorf("where = %s", where)
			}
		})
	}
}
