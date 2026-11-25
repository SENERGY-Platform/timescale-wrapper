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

package api

import (
	"bytes"
	"encoding/csv"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestQueriesParseQueryparams(t *testing.T) {
	type expected struct {
		format      model.Format
		index       int
		orderDir    model.Direction
		expectError bool
	}
	tests := []struct {
		name  string
		query string
		want  expected
	}{
		{
			name:  "no params",
			query: "",
			want:  expected{format: "", index: 0, orderDir: model.Desc},
		},
		{
			name:  "format table",
			query: "format=table",
			want:  expected{format: model.Table, index: 0, orderDir: model.Desc},
		},
		{
			name:  "order column index",
			query: "order_column_index=2",
			want:  expected{format: "", index: 2, orderDir: model.Desc},
		},
		{
			name:  "invalid order column index",
			query: "order_column_index=x",
			want:  expected{expectError: true},
		},
		{
			name:  "order direction asc",
			query: "order_direction=asc",
			want:  expected{format: "", index: 0, orderDir: model.Asc},
		},
		{
			name:  "invalid order direction",
			query: "order_direction=up",
			want:  expected{expectError: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/queries?"+tt.query, nil)
			format, index, dir, err := queriesParseQueryparams(request)
			if tt.want.expectError {
				if err == nil {
					t.Fatalf("expected error, got format=%q index=%d direction=%q", format, index, dir)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if format != tt.want.format {
				t.Errorf("format: expected %q, got %q", tt.want.format, format)
			}
			if index != tt.want.index {
				t.Errorf("index: expected %d, got %d", tt.want.index, index)
			}
			if dir != tt.want.orderDir {
				t.Errorf("direction: expected %q, got %q", tt.want.orderDir, dir)
			}
		})
	}
}

func TestV2Order(t *testing.T) {
	direction := func(d model.Direction) *model.Direction { return &d }
	tests := []struct {
		name      string
		element   model.QueriesRequestElement
		wantIndex int
		wantDir   model.Direction
	}{
		{
			name:      "empty element defaults to index 0 ascending",
			element:   model.QueriesRequestElement{},
			wantIndex: 0,
			wantDir:   model.Asc,
		},
		{
			name:      "set index is taken over",
			element:   model.QueriesRequestElement{OrderColumnIndex: ptr(3)},
			wantIndex: 3,
			wantDir:   model.Asc,
		},
		{
			name:      "set direction is taken over",
			element:   model.QueriesRequestElement{OrderDirection: direction(model.Desc)},
			wantIndex: 0,
			wantDir:   model.Desc,
		},
		{
			name: "set index and direction are taken over",
			element: model.QueriesRequestElement{
				OrderColumnIndex: ptr(1),
				OrderDirection:   direction(model.Desc),
			},
			wantIndex: 1,
			wantDir:   model.Desc,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index, dir := v2Order(tt.element)
			if index != tt.wantIndex {
				t.Errorf("index: expected %d, got %d", tt.wantIndex, index)
			}
			if dir != tt.wantDir {
				t.Errorf("direction: expected %q, got %q", tt.wantDir, dir)
			}
		})
	}
}

func TestWriteCsv(t *testing.T) {
	ts := time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC)
	write := func(data interface{}) (string, error) {
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		err := writeCsv(data, writer)
		return buf.String(), err
	}
	tests := []struct {
		name    string
		data    interface{}
		want    string
		wantErr bool
	}{
		{
			name: "3d writes all rows of all series in order",
			data: [][][]interface{}{
				{{"a", 1}, {"b", 2}},
				{{"c", 3}},
			},
			want: "a,1\nb,2\nc,3\n",
		},
		{
			name: "2d",
			data: [][]interface{}{
				{"x", 1.5},
				{true, nil},
			},
			want: "x,1.5\ntrue,null\n",
		},
		{
			name: "strings are written without quotes",
			data: [][]interface{}{{"a"}},
			want: "a\n",
		},
		{
			name: "numbers",
			data: [][]interface{}{{42, 0.25}},
			want: "42,0.25\n",
		},
		{
			name: "nil is written as null",
			data: [][]interface{}{{nil}},
			want: "null\n",
		},
		{
			name: "bool",
			data: [][]interface{}{{false}},
			want: "false\n",
		},
		{
			name: "time is written as rfc3339nano without quotes",
			data: [][]interface{}{{ts}},
			want: ts.Format(time.RFC3339Nano) + "\n",
		},
		{
			name:    "wrong type",
			data:    "x",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := write(tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got output %q", got)
				}
				if err.Error() != "data is not 2d or 3d" {
					t.Errorf("error: expected %q, got %q", "data is not 2d or 3d", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
