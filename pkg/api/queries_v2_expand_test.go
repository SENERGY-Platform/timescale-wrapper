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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// columnsWrapper answers a query with the rows its columns select: four rows, the value of a column is its name and
// the row number, and the last row of the column "b" is NULL. It counts the queries.
type columnsWrapper struct {
	generated atomic.Int32
}

func (w *columnsWrapper) GenerateQueries(_ context.Context, elements []model.QueriesRequestElement, _ string, _ []string, _ string, _ []models.Device) ([]string, error) {
	queries := make([]string, len(elements))
	for i, element := range elements {
		names := make([]string, len(element.Columns))
		for j, column := range element.Columns {
			names[j] = column.Name
		}
		queries[i] = strings.Join(names, ",")
	}
	w.generated.Add(int32(len(queries)))
	return queries, nil
}

func (w *columnsWrapper) ExecuteQueries(_ context.Context, queries []string) ([][][]interface{}, error) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	data := make([][][]interface{}, len(queries))
	for i, query := range queries {
		names := strings.Split(query, ",")
		for row := 0; row < 4; row++ {
			values := []interface{}{start.Add(time.Duration(row) * time.Hour)}
			for _, name := range names {
				if name == "b" && row == 3 {
					values = append(values, nil)
				} else {
					values = append(values, name+strings.Repeat("x", row))
				}
			}
			data[i] = append(data[i], values)
		}
	}
	return data, nil
}

func v2TestElement(limit *int, groupTime *string, orderColumn *int) model.QueriesRequestElement {
	export := "export"
	columns := []model.QueriesRequestElementColumn{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	if groupTime != nil {
		for i := range columns {
			groupType := "mean"
			columns[i].GroupType = &groupType
		}
	}
	return model.QueriesRequestElement{ExportId: &export, Columns: columns, Limit: limit, GroupTime: groupTime, OrderColumnIndex: orderColumn}
}

func runV2Element(t *testing.T, w *columnsWrapper, element model.QueriesRequestElement, subs func(*v2Handler) []subQuery) []model.QueriesV2ResponseElement {
	t.Helper()
	h := &v2Handler{pipeline: &pipeline{config: &configuration.ConfigStruct{}, wrapper: w}}
	params := v2Params{userId: "user", timeFormat: ""}
	var expanded []subQuery
	if subs != nil {
		expanded = subs(h)
	} else {
		var err error
		expanded, _, err = h.expandDirect(context.Background(), params, element, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	series, err := h.querySubs(context.Background(), params, element, "owner", expanded, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := []model.QueriesV2ResponseElement{}
	for _, s := range series {
		response = mergeQueriesV2Series(response, s.requestIndex, &s.element, s.columnIndex, s.rows)
	}
	return response
}

func TestExpandDirectQueryCount(t *testing.T) {
	t.Parallel()
	one, zero := 1, 0
	hour := "1h"
	cases := []struct {
		name    string
		element model.QueriesRequestElement
		queries int32
	}{
		{"raw columns without limit", v2TestElement(nil, nil, nil), 1},
		{"raw ordered by time", v2TestElement(nil, nil, &zero), 1},
		{"raw with limit", v2TestElement(&one, nil, nil), 3},
		{"grouped", v2TestElement(nil, &hour, nil), 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &columnsWrapper{}
			response := runV2Element(t, w, c.element, nil)
			if got := w.generated.Load(); got != c.queries {
				t.Errorf("%d queries, want %d", got, c.queries)
			}
			if len(response) != 1 || len(response[0].Data) != 3 {
				t.Errorf("expected one response element with 3 series, got %+v", response)
			}
		})
	}
}

// An order by another column refers to the position in the select list of the query, which differs between the
// combined query and the query of a column, so such an element is queried per column as before.
func TestOrderByColumnIsNotCombined(t *testing.T) {
	t.Parallel()
	two := 2
	if combinable(v2TestElement(nil, nil, &two)) {
		t.Error("an element ordered by a column must not be combined")
	}
	h := &v2Handler{pipeline: &pipeline{config: &configuration.ConfigStruct{}, wrapper: &columnsWrapper{}}}
	subs, _, err := h.expandDirect(context.Background(), v2Params{}, v2TestElement(nil, nil, &two), 0)
	if err != nil || len(subs) != 3 {
		t.Errorf("got %d sub queries, %v", len(subs), err)
	}
}

// One query for all columns must yield what a query per column does.
func TestCombinedQueryEqualsQueryPerColumn(t *testing.T) {
	t.Parallel()
	zero := 0
	for _, order := range []*int{nil, &zero} {
		for _, direction := range []model.Direction{model.Asc, model.Desc} {
			element := v2TestElement(nil, nil, order)
			element.OrderDirection = &direction
			combined := runV2Element(t, &columnsWrapper{}, element, nil)
			w := &columnsWrapper{}
			perColumn := runV2Element(t, w, element, func(*v2Handler) []subQuery {
				return perColumnSubs(element, 0)
			})
			if w.generated.Load() != 3 {
				t.Fatalf("the per column path must issue 3 queries, got %d", w.generated.Load())
			}
			if !reflect.DeepEqual(combined, perColumn) {
				t.Errorf("order %v %v: combined %+v, per column %+v", order, direction, combined, perColumn)
			}
			// the NULL of column b at its end is stripped, as it is for its own query
			if rows := combined[0].Data[1]; len(rows) != 3 {
				t.Errorf("column b has %d rows, want 3", len(rows))
			}
		}
	}
}

// Every element of the criteria path is expanded to at least one sub query with a column index, so the response
// element is always built by mergeQueriesV2Series and never directly.
func TestNoSubQueriesNoResponse(t *testing.T) {
	t.Parallel()
	w := &columnsWrapper{}
	h := &v2Handler{pipeline: &pipeline{config: &configuration.ConfigStruct{}, wrapper: w}}
	series, err := h.querySubs(context.Background(), v2Params{}, model.QueriesRequestElement{}, "owner", nil, nil)
	if err != nil || len(series) != 0 {
		t.Errorf("got %v %v", series, err)
	}
}
