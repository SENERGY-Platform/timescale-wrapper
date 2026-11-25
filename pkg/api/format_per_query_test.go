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

package api

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestFormatResponsePerQueryLimitAndEnd(t *testing.T) {
	t.Parallel()

	ts := func(hour int) time.Time { return time.Date(2022, 12, 6, hour, 0, 0, 0, time.UTC) }
	row := func(hour int, value interface{}) []interface{} { return []interface{}{ts(hour), value} }
	str := func(s string) *string { return &s }
	intp := func(i int) *int { return &i }
	columns := []model.QueriesRequestElementColumn{{Name: "value"}}
	asc := model.Asc

	type testCase struct {
		name       string
		element    model.QueriesRequestElement
		rows       [][]interface{}
		timeFormat string
		expected   [][]interface{}
	}
	testCases := []testCase{
		{
			name: "end is exclusive",
			element: model.QueriesRequestElement{
				Columns: columns,
				Time:    &model.QueriesRequestElementTime{End: str("2022-12-06T12:00:00Z")},
			},
			rows:     [][]interface{}{row(10, 1), row(11, 2), row(12, 3), row(13, 4)},
			expected: [][]interface{}{row(10, 1), row(11, 2)},
		},
		{
			name: "end filters the rows at or after it",
			element: model.QueriesRequestElement{
				Columns: columns,
				Time:    &model.QueriesRequestElementTime{End: str("2022-12-06T11:00:00Z")},
			},
			rows:     [][]interface{}{row(10, 1), row(11, 2), row(12, 3), row(13, 4)},
			expected: [][]interface{}{row(10, 1)},
		},
		{
			name: "limit without end",
			element: model.QueriesRequestElement{
				Columns: columns,
				Limit:   intp(2),
			},
			rows:     [][]interface{}{row(10, 1), row(11, 2), row(12, 3), row(13, 4)},
			expected: [][]interface{}{row(10, 1), row(11, 2)},
		},
		{
			name: "trailing rows without values are removed",
			element: model.QueriesRequestElement{
				Columns: columns,
			},
			rows:     [][]interface{}{row(10, 1), row(11, nil), row(12, nil)},
			expected: [][]interface{}{row(10, 1)},
		},
		{
			name: "time column is formatted",
			element: model.QueriesRequestElement{
				Columns: columns,
			},
			rows:       [][]interface{}{row(10, 1), row(11, 2)},
			timeFormat: time.RFC3339,
			expected:   [][]interface{}{{"2022-12-06T10:00:00Z", 1}, {"2022-12-06T11:00:00Z", 2}},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			element := tc.element
			element.OrderDirection = &asc
			request := []model.QueriesRequestElement{element}
			response, err := formatPerQuery(context.Background(), newRequestLookups(nil), request, [][][]interface{}{tc.rows}, 0, model.Asc, tc.timeFormat, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(response, [][][]interface{}{tc.expected}) {
				t.Errorf("unexpected result: %v", response)
			}
		})
	}
}

// With descending order the rows at or after End are at the front of the series. They have to be
// removed there too, and before the limit, which would otherwise count them.
func TestFormatResponsePerQueryEndDesc(t *testing.T) {
	t.Parallel()

	ts := func(hour int) time.Time { return time.Date(2022, 12, 6, hour, 0, 0, 0, time.UTC) }
	row := func(hour int, value interface{}) []interface{} { return []interface{}{ts(hour), value} }
	str := func(s string) *string { return &s }
	desc := model.Desc

	request := []model.QueriesRequestElement{{
		Columns:        []model.QueriesRequestElementColumn{{Name: "value"}},
		OrderDirection: &desc,
		Time:           &model.QueriesRequestElementTime{End: str("2022-12-06T12:00:00Z")},
	}}
	rows := [][]interface{}{row(10, 1), row(11, 2), row(12, 3), row(13, 4)}
	response, err := formatPerQuery(context.Background(), newRequestLookups(nil), request, [][][]interface{}{rows}, 0, model.Desc, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := [][][]interface{}{{row(11, 2), row(10, 1)}}
	if !reflect.DeepEqual(response, expected) {
		t.Errorf("unexpected result, want rows before 12:00 in descending order: got %v, want %v", response, expected)
	}

	limit := 2
	request[0].Limit = &limit
	rows = [][]interface{}{row(10, 1), row(11, 2), row(12, 3), row(13, 4)}
	response, err = formatPerQuery(context.Background(), newRequestLookups(nil), request, [][][]interface{}{rows}, 0, model.Desc, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response, expected) {
		t.Errorf("unexpected result with limit %d: got %v, want %v", limit, response, expected)
	}
}

func TestFormatResponseMissingIds(t *testing.T) {
	t.Parallel()

	str := func(s string) *string { return &s }

	type testCase struct {
		name     string
		element  model.QueriesRequestElement
		expected string
	}
	testCases := []testCase{
		{
			name: "target characteristic without source characteristic and service id",
			element: model.QueriesRequestElement{
				Columns: []model.QueriesRequestElementColumn{{
					Name:                   "root.value",
					TargetCharacteristicId: str("target"),
				}},
			},
			expected: "service id cant be nil",
		},
		{
			name: "target and source characteristic without concept id",
			element: model.QueriesRequestElement{
				Columns: []model.QueriesRequestElementColumn{{
					Name:                   "value",
					SourceCharacteristicId: str("source"),
					TargetCharacteristicId: str("target"),
				}},
			},
			expected: "concept id cant be nil",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// both errors are returned before the cache or the converter is used
			_, err := formatPerQuery(context.Background(), newRequestLookups(nil), []model.QueriesRequestElement{tc.element}, nil, 0, model.Asc, "", nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if err.Error() != tc.expected {
				t.Errorf("expected error %q, got %q", tc.expected, err.Error())
			}
		})
	}
}
