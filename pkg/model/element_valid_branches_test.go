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

import (
	"encoding/json"
	"testing"
)

// Branches of QueriesRequestElement.Valid that TestRequestElementValidation does not reach: the
// source of the element (device group, location, export), the time window, the order and the
// characteristic handling of export columns.
func TestRequestElementValidBranches(t *testing.T) {
	// ids sets the source fields of the element. "device" sets device and service together.
	setIds := func(e *QueriesRequestElement, ids []string) {
		for _, id := range ids {
			switch id {
			case "device":
				e.DeviceId = ptr(testDeviceId)
				e.ServiceId = ptr(testServiceId)
			case "group":
				e.DeviceGroupId = ptr("group-id")
			case "location":
				e.LocationId = ptr("location-id")
			case "export":
				e.ExportId = ptr("export-id")
			}
		}
	}
	tt := []struct {
		Name     string
		Ids      []string
		Body     string
		Expected bool
	}{
		{
			Name:     "device group with a criteria column",
			Ids:      []string{"group"},
			Body:     `{"columns":[{"criteria":{"function_id":"f","aspect_id":"a"}}]}`,
			Expected: true,
		},
		{
			Name:     "device group with a device",
			Ids:      []string{"group", "device"},
			Body:     `{"columns":[{"name":"value.x"}]}`,
			Expected: false,
		},
		{
			Name:     "device group with a location",
			Ids:      []string{"group", "location"},
			Body:     `{"columns":[{"name":"value.x"}]}`,
			Expected: false,
		},
		{
			Name:     "location with a criteria column",
			Ids:      []string{"location"},
			Body:     `{"columns":[{"criteria":{"function_id":"f","aspect_id":"a"}}]}`,
			Expected: true,
		},
		{
			Name:     "location with an export",
			Ids:      []string{"location", "export"},
			Body:     `{"columns":[{"name":"value.x"}]}`,
			Expected: false,
		},
		{
			Name:     "criteria column with a name",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x","criteria":{"function_id":"f","aspect_id":"a"}}]}`,
			Expected: false,
		},
		{
			Name:     "time with only a start",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"time":{"start":"2026-01-01T00:00:00Z"}}`,
			Expected: false,
		},
		{
			Name:     "no columns",
			Ids:      []string{"device"},
			Body:     `{"columns":[]}`,
			Expected: false,
		},
		{
			Name:     "export column with target and concept but no source",
			Ids:      []string{"export"},
			Body:     `{"columns":[{"name":"value.x","targetCharacteristicId":"t","conceptId":"c"}]}`,
			Expected: false,
		},
		{
			Name:     "export column with target, concept and source",
			Ids:      []string{"export"},
			Body:     `{"columns":[{"name":"value.x","sourceCharacteristicId":"s","targetCharacteristicId":"t","conceptId":"c"}]}`,
			Expected: true,
		},
		{
			Name:     "group time that is not an interval",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"groupTime":"1x"}`,
			Expected: false,
		},
		{
			Name:     "group time with a group type",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x","groupType":"mean"}],"groupTime":"1h"}`,
			Expected: true,
		},
		{
			Name:     "order direction that is neither asc nor desc",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"orderDirection":"up"}`,
			Expected: false,
		},
		{
			Name:     "negative order column index",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"orderColumnIndex":-1}`,
			Expected: false,
		},
		{
			Name:     "order column index equal to the number of columns",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"orderColumnIndex":1}`,
			Expected: true,
		},
		{
			Name:     "order column index beyond the number of columns",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"orderColumnIndex":2}`,
			Expected: false,
		},
		// The two limit cases repeat TestRequestElementValidation on purpose, as listed for this test.
		{
			Name:     "negative limit",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"limit":-1}`,
			Expected: false,
		},
		{
			Name:     "zero limit",
			Ids:      []string{"device"},
			Body:     `{"columns":[{"name":"value.x"}],"limit":0}`,
			Expected: true,
		},
	}
	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			var element QueriesRequestElement
			if err := json.Unmarshal([]byte(tc.Body), &element); err != nil {
				t.Fatal(err)
			}
			setIds(&element, tc.Ids)
			if got := element.Valid(); got != tc.Expected {
				t.Errorf("Valid() = %t, want %t", got, tc.Expected)
			}
		})
	}
}
