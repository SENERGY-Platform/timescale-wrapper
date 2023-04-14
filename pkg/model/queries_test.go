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
	"fmt"
	"testing"
)

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
