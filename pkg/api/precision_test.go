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
	"encoding/json"
	"math"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestParsePrecision(t *testing.T) {
	t.Parallel()
	for param, expected := range map[string]*int{"": nil, "0": ptr(0), "2": ptr(2), "15": ptr(15)} {
		request := httptest.NewRequest("POST", "/queries?precision="+url.QueryEscape(param), nil)
		actual, err := parsePrecision(request)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			t.Errorf("%q: expected %v, got %v, %v", param, expected, actual, err)
		}
	}
	for _, param := range []string{"-1", "16", "two", "1.5"} {
		request := httptest.NewRequest("POST", "/queries?precision="+url.QueryEscape(param), nil)
		if _, err := parsePrecision(request); err == nil {
			t.Errorf("%q: expected an error", param)
		}
	}
}

func ptr(i int) *int { return &i }

func TestRoundValue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value     interface{}
		precision int
		expected  interface{}
	}{
		{1.23456, 2, 1.23},
		{1.235, 0, 1.0},
		{2.5, 0, 3.0},
		{-2.5, 0, -3.0},
		{0.125, 2, 0.13},
		{2.675, 2, 2.68},
		{1.005, 2, 1.0},
		{-0.001, 2, 0.0},
		{123456.789, 1, 123456.8},
		{1e300, 2, 1e300},
		{12.0, 3, 12.0},
		{int64(7), 0, int64(7)},
		{"1.2345", 1, "1.2345"},
		{true, 1, true},
		{nil, 1, nil},
		{[]interface{}{1.2345}, 1, []interface{}{1.2345}},
	}
	for _, c := range cases {
		actual := roundValue(c.value, c.precision)
		if !reflect.DeepEqual(actual, c.expected) {
			t.Errorf("roundValue(%#v, %d): expected %#v, got %#v", c.value, c.precision, c.expected, actual)
		}
	}
	if f := roundValue(-0.001, 2).(float64); math.Signbit(f) {
		t.Error("expected 0, not -0")
	}
	for _, special := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if f := roundValue(special, 2).(float64); !(math.IsNaN(f) && math.IsNaN(special)) && f != special {
			t.Errorf("expected %v unchanged, got %v", special, f)
		}
	}
}

// The time column is never touched, whatever the format.
func TestRoundResponse(t *testing.T) {
	t.Parallel()
	ts := time.Date(2022, 12, 6, 6, 0, 0, 0, time.UTC)
	two := 2

	table := [][]interface{}{{ts, 1.2345, int64(3), nil}}
	roundResponse(table, &two)
	if !reflect.DeepEqual(table, [][]interface{}{{ts, 1.23, int64(3), nil}}) {
		t.Errorf("table: %v", table)
	}

	series := [][][]interface{}{{{ts, 9.876}}, {{"2022-12-06", 0.005}}}
	roundResponse(series, &two)
	actual, _ := json.Marshal(series)
	if string(actual) != `[[["2022-12-06T06:00:00Z",9.88]],[["2022-12-06",0.01]]]` {
		t.Errorf("series: %s", actual)
	}

	unchanged := [][]interface{}{{ts, 1.2345}}
	roundResponse(unchanged, nil)
	if unchanged[0][1] != 1.2345 {
		t.Error("expected no rounding without precision")
	}
}

func TestDownloadQueryParams(t *testing.T) {
	t.Parallel()
	two := 2
	params, err := url.ParseQuery(downloadQueryParams("2006-01-02 15:04:05.000-07:00", &two))
	if err != nil {
		t.Fatal(err)
	}
	if params.Get("time_format") != "2006-01-02 15:04:05.000-07:00" || params.Get("precision") != "2" || params.Get("format") != "table" {
		t.Errorf("unexpected params %v", params)
	}
	params, _ = url.ParseQuery(downloadQueryParams("", nil))
	if params.Has("precision") {
		t.Errorf("unexpected precision in %v", params)
	}
}
