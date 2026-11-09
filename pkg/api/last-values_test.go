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
	"reflect"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// Only the elements the cache could not serve go to the database. Their owners are indexed like
// the full elements, so they have to be picked by the remaining indices.
func TestOwnersOfIndices(t *testing.T) {
	owners := []string{"owner0", "owner1", "owner2"}
	if got := ownersOfIndices(owners, []int{1}); !reflect.DeepEqual(got, []string{"owner1"}) {
		t.Errorf("miss on element 1 only: %v", got)
	}
	if got := ownersOfIndices(owners, []int{2, 0}); !reflect.DeepEqual(got, []string{"owner2", "owner0"}) {
		t.Errorf("unordered misses: %v", got)
	}
	if got := ownersOfIndices(owners, nil); len(got) != 0 {
		t.Errorf("all hits: %v", got)
	}
}

func TestBuildLastValuesElements(t *testing.T) {
	serviceA := "urn:infai:ses:service:00000000-0000-0000-0000-00000000000a"
	deviceA := "urn:infai:ses:device:a"
	deviceB := "urn:infai:ses:device:b"

	t.Run("not_null filters only the element of its own column", func(t *testing.T) {
		elements, outputIndexToInputIndex, err := buildLastValuesElements([]model.LastValuesRequestElement{
			{DeviceId: &deviceA, ServiceId: &serviceA, ColumnName: "root.a", NotNull: true},
			{DeviceId: &deviceB, ServiceId: &serviceA, ColumnName: "root.b"},
			{DeviceId: &deviceA, ServiceId: &serviceA, ColumnName: "root.c"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(elements) != 2 {
			t.Fatalf("expected 2 elements, got %d", len(elements))
		}
		found := 0
		for _, element := range elements {
			switch *element.DeviceId {
			case deviceA:
				found++
				expected := []model.QueriesRequestElementFilter{{Column: "root.a", Type: "!=", Value: nil}}
				if element.Filters == nil || !reflect.DeepEqual(*element.Filters, expected) {
					t.Errorf("unexpected filters on device a: %#v", element.Filters)
				}
			case deviceB:
				found++
				if element.Filters != nil {
					t.Errorf("expected nil filters on device b, got %#v", *element.Filters)
				}
			}
		}
		if found != 2 {
			t.Errorf("expected both devices, found %d", found)
		}

		// every result column maps back to a distinct request index
		seen := map[int]bool{}
		for _, requestIndex := range outputIndexToInputIndex {
			seen[requestIndex] = true
		}
		if len(outputIndexToInputIndex) != 3 || len(seen) != 3 {
			t.Errorf("unexpected index mapping: %v", outputIndexToInputIndex)
		}
	})

	t.Run("no not_null keeps filters nil", func(t *testing.T) {
		elements, _, err := buildLastValuesElements([]model.LastValuesRequestElement{
			{DeviceId: &deviceA, ServiceId: &serviceA, ColumnName: "root.a"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(elements) != 1 || elements[0].Filters != nil {
			t.Errorf("unexpected elements: %#v", elements)
		}
	})

	t.Run("invalid element", func(t *testing.T) {
		_, _, err := buildLastValuesElements([]model.LastValuesRequestElement{{ColumnName: "root.a"}})
		if err == nil {
			t.Error("expected an error")
		}
	})
}
