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
