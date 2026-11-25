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

package sqlliteral

import (
	"encoding/json"
	"testing"
)

func TestFunctionName(t *testing.T) {
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
		// "difference" without a dash must not panic
		{name: "difference without dash", groupType: "difference", expected: "difference("},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := FunctionName(tt.groupType)
			if actual != tt.expected {
				t.Error("Expected/Actual\n", tt.expected, "\n", actual)
			}
		})
	}
}

func TestValueLiteral(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		expected string
	}{
		{"string", "abc", "'abc'"},
		{"string with embedded quote", "a'bc", "'a''bc'"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"float64", float64(1.5), "1.5"},
		{"float32", float32(1.5), "1.5"},
		{"int", int(42), "42"},
		{"int16", int16(42), "42"},
		{"int32", int32(42), "42"},
		{"int64", int64(42), "42"},
		{"json.Number", json.Number("42.5"), "42.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, err := ValueLiteral(c.value)
			if err != nil {
				t.Fatal(err)
			}
			if actual != c.expected {
				t.Error("Expected/Actual\n", c.expected, "\n", actual)
			}
		})
	}
}

func TestValueLiteralRejectsInvalidJsonNumber(t *testing.T) {
	if _, err := ValueLiteral(json.Number("not-a-number")); err == nil {
		t.Error("expected an error for an invalid json.Number")
	}
}

func TestValueLiteralRejectsUnknownType(t *testing.T) {
	if _, err := ValueLiteral(struct{}{}); err == nil {
		t.Error("expected an error for an unsupported type")
	}
}

func TestFilterValueLiteral(t *testing.T) {
	t.Run("string quotes are doubled", func(t *testing.T) {
		got, err := ValueLiteral(`a' OR 1=1 --`)
		if err != nil {
			t.Fatal(err)
		}
		if got != `'a'' OR 1=1 --'` {
			t.Errorf("unexpected literal %s", got)
		}
	})
	t.Run("unsupported types are rejected", func(t *testing.T) {
		for _, v := range []interface{}{
			map[string]interface{}{"1 OR 1=1 --": 1},
			[]interface{}{"1 OR 1=1 --"},
			struct{}{},
		} {
			if _, err := ValueLiteral(v); err == nil {
				t.Errorf("accepted unsupported filter value %#v", v)
			}
		}
	})
	t.Run("numbers keep their rendering", func(t *testing.T) {
		for value, expected := range map[interface{}]string{
			10:          "10",
			float64(10): "10",
			10.5:        "10.5",
			int64(-3):   "-3",
			true:        "true",
		} {
			got, err := ValueLiteral(value)
			if err != nil {
				t.Fatal(err)
			}
			if got != expected {
				t.Errorf("ValueLiteral(%v) = %s, want %s", value, got, expected)
			}
		}
	})
}
