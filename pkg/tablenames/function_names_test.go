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

package tablenames

import "testing"

func TestTranslateFunctionName(t *testing.T) {
	tests := []struct {
		name      string
		groupType string
		expected  string
	}{
		{name: "mean", groupType: "mean", expected: "avg("},
		{name: "median", groupType: "median", expected: "percentile_disc(0.5) WITHIN GROUP (ORDER BY "},
		{name: "time-weighted-mean-linear", groupType: "time-weighted-mean-linear", expected: "average(time_weight('Linear', \"time\", "},
		{name: "time-weighted-mean-locf", groupType: "time-weighted-mean-locf", expected: "average(time_weight('LOCF', \"time\", "},
		{name: "difference-last", groupType: "difference-last", expected: "last("},
		// Current behavior: the "difference-" prefix is stripped and the remaining part is used as the function name.
		{name: "difference-mean", groupType: "difference-mean", expected: "mean("},
		{name: "max", groupType: "max", expected: "max("},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := TranslateFunctionName(tt.groupType)
			if actual != tt.expected {
				t.Error("Expected/Actual\n", tt.expected, "\n", actual)
			}
		})
	}
}

func TestTableNamesRejectInvalidIds(t *testing.T) {
	validDevice := "urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4"
	validService := "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050"
	invalid := []string{
		"urn:infai:ses:device:not-hex",
		"urn:infai:ses:device:zz",
		"urn:infai:ses:device:abc",
	}

	for _, bad := range invalid {
		t.Run("DeviceTableName first id "+bad, func(t *testing.T) {
			if _, err := DeviceTableName(bad, validService); err == nil {
				t.Error("expected an error for invalid device id", bad)
			}
		})
		t.Run("DeviceTableName second id "+bad, func(t *testing.T) {
			if _, err := DeviceTableName(validDevice, bad); err == nil {
				t.Error("expected an error for invalid service id", bad)
			}
		})
		t.Run("ExportTableName first id "+bad, func(t *testing.T) {
			if _, err := ExportTableName(bad, validService); err == nil {
				t.Error("expected an error for invalid user id", bad)
			}
		})
		t.Run("ExportTableName second id "+bad, func(t *testing.T) {
			if _, err := ExportTableName(validDevice, bad); err == nil {
				t.Error("expected an error for invalid export id", bad)
			}
		})
	}
}
