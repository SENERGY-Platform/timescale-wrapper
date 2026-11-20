/*
 * Copyright 2021 InfAI (CC SES)
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
	"fmt"
	"testing"
	"time"
)

const testDeviceId = "urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4"
const testServiceId = "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050"

// Request bodies are the only thing standing between a caller and the generated SQL, so the
// validator has to reject anything that could change the shape of a query.
func TestRequestElementValidation(t *testing.T) {
	tt := []struct {
		Name     string
		Body     string
		Expected bool
	}{
		{
			Name:     "plain column",
			Body:     `{"columns":[{"name":"value"}]}`,
			Expected: true,
		},
		{
			Name:     "column name with a quote",
			Body:     `{"columns":[{"name":"x\" FROM (SELECT version() AS \"x\") z --"}]}`,
			Expected: false,
		},
		{
			// a criteria based column used to be required to carry an *invalid* name, which
			// left the name unvalidated all the way into the query
			Name:     "column name with a quote next to a criteria",
			Body:     `{"columns":[{"name":"x\" FROM (SELECT version() AS \"x\") z --","criteria":{"function_id":"f","aspect_id":"a"}}]}`,
			Expected: false,
		},
		{
			Name:     "criteria without a name",
			Body:     `{"columns":[{"criteria":{"function_id":"f","aspect_id":"a"}}]}`,
			Expected: true,
		},
		{
			Name:     "valid name next to a criteria",
			Body:     `{"columns":[{"name":"value","criteria":{"function_id":"f","aspect_id":"a"}}]}`,
			Expected: false,
		},
		{
			Name:     "neither name nor criteria",
			Body:     `{"columns":[{}]}`,
			Expected: false,
		},
		{
			Name:     "math",
			Body:     `{"columns":[{"name":"value","math":"+5"}]}`,
			Expected: true,
		},
		{
			Name:     "math with a decimal point",
			Body:     `{"columns":[{"name":"value","math":"*1.5"}]}`,
			Expected: true,
		},
		{
			// postgres reads the comma as a list separator, which appends a select list item
			Name:     "math with a comma",
			Body:     `{"columns":[{"name":"value","math":"+1,5"}]}`,
			Expected: false,
		},
		{
			Name:     "string filter value",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"value","type":"=","value":"abc"}]}`,
			Expected: true,
		},
		{
			Name:     "numeric filter value",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"value","type":">","value":10.5}]}`,
			Expected: true,
		},
		{
			Name:     "null filter value",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"value","type":"!=","value":null}]}`,
			Expected: true,
		},
		{
			Name:     "string filter value with a quote",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"value","type":"=","value":"a' OR 1=1 --"}]}`,
			Expected: false,
		},
		{
			// non string values skipped the allowlist and were rendered with %v
			Name:     "object filter value",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"value","type":"=","value":{"1 OR 1=1 --":1}}]}`,
			Expected: false,
		},
		{
			Name:     "array filter value",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"value","type":"=","value":["1 OR 1=1 --"]}]}`,
			Expected: false,
		},
		{
			Name:     "filter column with a quote",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"a\" --","type":"=","value":1}]}`,
			Expected: false,
		},
		{
			Name:     "list index in a column name",
			Body:     `{"columns":[{"name":"root.list.0.field"}]}`,
			Expected: true,
		},
		{
			Name:     "wildcard column without a group type",
			Body:     `{"columns":[{"name":"root.list.*.field"}]}`,
			Expected: true,
		},
		{
			Name:     "wildcard column with a group type",
			Body:     `{"columns":[{"name":"root.list.*.field","groupType":"sum"}],"groupTime":"1h"}`,
			Expected: true,
		},
		{
			Name:     "wildcard column with first",
			Body:     `{"columns":[{"name":"root.list.*.field","groupType":"difference-last"}],"groupTime":"1h"}`,
			Expected: true,
		},
		{
			Name:     "wildcard as part of a segment",
			Body:     `{"columns":[{"name":"root.list.a*.field"}]}`,
			Expected: false,
		},
		{
			Name:     "wildcard only",
			Body:     `{"columns":[{"name":"*"}]}`,
			Expected: true,
		},
		{
			Name:     "wildcard column with math and no group type",
			Body:     `{"columns":[{"name":"root.list.*.field","math":"+5"}]}`,
			Expected: false,
		},
		{
			Name:     "wildcard column with a target characteristic and no group type",
			Body:     `{"columns":[{"name":"root.list.*.field","targetCharacteristicId":"t","conceptId":"c"}]}`,
			Expected: false,
		},
		{
			Name:     "wildcard column with a target characteristic and a group type",
			Body:     `{"columns":[{"name":"root.list.*.field","groupType":"max","targetCharacteristicId":"t","conceptId":"c"}],"groupTime":"1h"}`,
			Expected: true,
		},
		{
			Name:     "wildcard column with time weighted mean",
			Body:     `{"columns":[{"name":"root.list.*.field","groupType":"time-weighted-mean-linear"}],"groupTime":"1h"}`,
			Expected: false,
		},
		{
			Name:     "wildcard in a filter",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"root.list.*.field","type":"=","value":1}]}`,
			Expected: false,
		},
		{
			Name:     "list index in a filter",
			Body:     `{"columns":[{"name":"value"}],"filters":[{"column":"root.list.0.field","type":"=","value":1}]}`,
			Expected: true,
		},
		{
			Name:     "group time",
			Body:     `{"columns":[{"name":"value","groupType":"mean"}],"groupTime":"1h"}`,
			Expected: true,
		},
		{
			Name:     "group time with a quote",
			Body:     `{"columns":[{"name":"value","groupType":"mean"}],"groupTime":"1h' --"}`,
			Expected: false,
		},
		{
			Name:     "unknown group type",
			Body:     `{"columns":[{"name":"value","groupType":"mean\") FROM x --"}],"groupTime":"1h"}`,
			Expected: false,
		},
		{
			Name:     "limit",
			Body:     `{"columns":[{"name":"value"}],"limit":100}`,
			Expected: true,
		},
		{
			Name:     "zero limit",
			Body:     `{"columns":[{"name":"value"}],"limit":0}`,
			Expected: true,
		},
		{
			// a negative limit is rejected by postgres and would be an invalid slice bound
			// in the post-processing, so it must not get past the validator
			Name:     "negative limit",
			Body:     `{"columns":[{"name":"value"}],"limit":-1}`,
			Expected: false,
		},
	}
	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			var element QueriesRequestElement
			if err := json.Unmarshal([]byte(tc.Body), &element); err != nil {
				t.Fatal(err)
			}
			element.DeviceId = ptr(testDeviceId)
			element.ServiceId = ptr(testServiceId)
			if got := element.Valid(); got != tc.Expected {
				t.Errorf("Valid() = %t, want %t", got, tc.Expected)
			}
		})
	}
}

func TestTimezoneValid(t *testing.T) {
	tt := map[string]bool{
		"UTC":                            true,
		"Europe/Berlin":                  true,
		"America/Argentina/Buenos_Aires": true,
		"Etc/GMT+5":                      true,
		"":                               false,
		"UTC'":                           false,
		`UTC" `:                          false,
		"UTC') AS \"time\" --":           false,
		"UTC\x00":                        false,
		"UTC\n":                          false,
	}
	for tz, expected := range tt {
		t.Run(tz, func(t *testing.T) {
			if got := TimezoneValid(tz); got != expected {
				t.Errorf("TimezoneValid(%q) = %t, want %t", tz, got, expected)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestTimeIntervalValidators(t *testing.T) {
	tt := []struct {
		GroupTime string
		Expected  bool
	}{
		{
			GroupTime: "months",
			Expected:  false,
		},
		{
			GroupTime: "a",
			Expected:  false,
		},
		{
			GroupTime: "",
			Expected:  false,
		},
		{
			GroupTime: "1seconds",
			Expected:  false,
		},
		{
			GroupTime: "1ns",
			Expected:  false,
		},
		{
			GroupTime: "1u",
			Expected:  false,
		},
		{
			GroupTime: "1µ",
			Expected:  false,
		},
		{
			GroupTime: "10ms",
			Expected:  true,
		},
		{
			GroupTime: "1s",
			Expected:  true,
		},
		{
			GroupTime: "1m",
			Expected:  true,
		},
		{
			GroupTime: "1h",
			Expected:  true,
		},
		{
			GroupTime: "1d",
			Expected:  true,
		},
		{
			GroupTime: "4w",
			Expected:  true,
		},
		{
			GroupTime: "1months",
			Expected:  true,
		},
		{
			GroupTime: "100y",
			Expected:  true,
		},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("Test Time Interval Validator: %s", tc.GroupTime), func(t *testing.T) {
			validationResult := timeIntervalValid(tc.GroupTime)
			if validationResult != tc.Expected {
				t.Errorf("Want: %t - Got: %t", tc.Expected, validationResult)
			}
		})
	}

}

// AddTimeInterval reads every unit timeIntervalValid accepts, as postgres adds it to now().
func TestAddTimeInterval(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	tt := []struct {
		interval string
		sign     int
		want     time.Time
	}{
		{"500ms", -1, now.Add(-500 * time.Millisecond)},
		{"90s", -1, now.Add(-90 * time.Second)},
		{"15m", -1, now.Add(-15 * time.Minute)},
		{"17520h", -1, now.Add(-17520 * time.Hour)},
		{"730d", -1, now.AddDate(0, 0, -730)},
		{"2 day", -1, time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)},
		{"1w", 1, time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)},
		{"1mon", -1, time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)},
		{"1 months", 1, time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)},
		{"13months", -1, time.Date(2025, 2, 28, 12, 0, 0, 0, time.UTC)},
		{"2y", -1, time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC)},
		{"1:30:15", -1, now.Add(-(time.Hour + 30*time.Minute + 15*time.Second))},
		{"1:30:15", 1, now.Add(time.Hour + 30*time.Minute + 15*time.Second)},
	}
	for _, tc := range tt {
		got, err := AddTimeInterval(now, tc.interval, tc.sign)
		if err != nil {
			t.Errorf("%s: %v", tc.interval, err)
			continue
		}
		if !got.Equal(tc.want) {
			t.Errorf("%s (%d): got %s, want %s", tc.interval, tc.sign, got, tc.want)
		}
	}
	leap := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	if got, _ := AddTimeInterval(leap, "1y", 1); !got.Equal(time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("1y after %s: got %s", leap, got)
	}
	if got, err := AddTimeInterval(now, "100000000ms", -1); err != nil || !got.Equal(now.Add(-100000*time.Second)) {
		t.Errorf("100000000ms: got %s, %v", got, err)
	}
	for _, interval := range []string{"9999999999h", "3000000000m", "10000000000s", "9999999999999ms", "9999999999:00:00", "300000y", "20000y", "4000000months", "99999999999999999999d", "9223372036854775807y"} {
		if got, err := AddTimeInterval(now, interval, -1); err == nil {
			t.Errorf("%s: expected an error, got %s", interval, got)
		}
	}
	if _, err := AddTimeInterval(now, "1 fortnight", -1); err == nil {
		t.Error("expected an error for an unknown unit")
	}
}
