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
	"reflect"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestMergeQueriesV2Series(t *testing.T) {
	t.Parallel()

	d1, d2 := "device-1", "device-2"
	s1, s2 := "service-1", "service-2"
	export := "export-1"
	elem := func(deviceId, serviceId, exportId *string, columns ...string) *model.QueriesRequestElement {
		e := &model.QueriesRequestElement{DeviceId: deviceId, ServiceId: serviceId, ExportId: exportId}
		for _, c := range columns {
			e.Columns = append(e.Columns, model.QueriesRequestElementColumn{Name: c})
		}
		return e
	}
	series := func(v int) [][]interface{} {
		return [][]interface{}{{"2026-10-26T00:00:00Z", v}}
	}
	type subQuery struct {
		requestIndex int
		elem         *model.QueriesRequestElement
		colIdx       int
		series       [][]interface{}
	}
	merge := func(subQueries []subQuery) []model.QueriesV2ResponseElement {
		response := []model.QueriesV2ResponseElement{}
		for _, q := range subQueries {
			response = mergeQueriesV2Series(response, q.requestIndex, q.elem, q.colIdx, q.series)
		}
		return response
	}

	// SNRGY-4769: the second service of the first device of a group overwrote
	// the series of the first service.
	t.Run("group whose first device has two matching services", func(t *testing.T) {
		t.Parallel()
		response := merge([]subQuery{
			{0, elem(&d1, &s1, nil, "a"), 0, series(1)},
			{0, elem(&d1, &s2, nil, "b"), 0, series(2)},
			{0, elem(&d2, &s1, nil, "a"), 0, series(3)},
		})
		expected := []model.QueriesV2ResponseElement{
			{RequestIndex: 0, DeviceId: &d1, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(1)}},
			{RequestIndex: 0, DeviceId: &d1, ServiceId: &s2, ColumnNames: []string{"b"}, Data: [][][]interface{}{series(2)}},
			{RequestIndex: 0, DeviceId: &d2, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(3)}},
		}
		if !reflect.DeepEqual(response, expected) {
			t.Errorf("unexpected response %#v", response)
		}
	})

	// Each column selects with its own criteria, so the devices of a group
	// can arrive in a different order per column.
	t.Run("group with two columns in different device order", func(t *testing.T) {
		t.Parallel()
		response := merge([]subQuery{
			{0, elem(&d1, &s1, nil, "a"), 0, series(1)},
			{0, elem(&d2, &s1, nil, "a"), 0, series(2)},
			{0, elem(&d2, &s1, nil, "b"), 1, series(3)},
			{0, elem(&d1, &s1, nil, "b"), 1, series(4)},
		})
		expected := []model.QueriesV2ResponseElement{
			{RequestIndex: 0, DeviceId: &d1, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(1), series(4)}},
			{RequestIndex: 0, DeviceId: &d2, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(2), series(3)}},
		}
		if !reflect.DeepEqual(response, expected) {
			t.Errorf("unexpected response %#v", response)
		}
	})

	t.Run("group whose second column matches another service", func(t *testing.T) {
		t.Parallel()
		response := merge([]subQuery{
			{0, elem(&d1, &s1, nil, "a"), 0, series(1)},
			{0, elem(&d1, &s2, nil, "b"), 1, series(2)},
		})
		expected := []model.QueriesV2ResponseElement{
			{RequestIndex: 0, DeviceId: &d1, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(1)}},
			{RequestIndex: 0, DeviceId: &d1, ServiceId: &s2, ColumnNames: []string{"b"}, Data: [][][]interface{}{{}, series(2)}},
		}
		if !reflect.DeepEqual(response, expected) {
			t.Errorf("unexpected response %#v", response)
		}
	})

	t.Run("single device and export with two columns each", func(t *testing.T) {
		t.Parallel()
		response := merge([]subQuery{
			{0, elem(&d1, &s1, nil, "a"), 0, series(1)},
			{0, elem(&d1, &s1, nil, "b"), 1, series(2)},
			{1, elem(nil, nil, &export, "a"), 0, series(3)},
			{1, elem(nil, nil, &export, "b"), 1, series(4)},
		})
		expected := []model.QueriesV2ResponseElement{
			{RequestIndex: 0, DeviceId: &d1, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(1), series(2)}},
			{RequestIndex: 1, ExportId: &export, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(3), series(4)}},
		}
		if !reflect.DeepEqual(response, expected) {
			t.Errorf("unexpected response %#v", response)
		}
	})

	t.Run("same device and service in two request elements", func(t *testing.T) {
		t.Parallel()
		response := merge([]subQuery{
			{0, elem(&d1, &s1, nil, "a"), 0, series(1)},
			{1, elem(&d1, &s1, nil, "a"), 0, series(2)},
		})
		expected := []model.QueriesV2ResponseElement{
			{RequestIndex: 0, DeviceId: &d1, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(1)}},
			{RequestIndex: 1, DeviceId: &d1, ServiceId: &s1, ColumnNames: []string{"a"}, Data: [][][]interface{}{series(2)}},
		}
		if !reflect.DeepEqual(response, expected) {
			t.Errorf("unexpected response %#v", response)
		}
	})
}
