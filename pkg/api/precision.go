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
	"errors"
	"math"
	"net/http"
	"strconv"
)

// maxPrecision is the number of decimal places a float64 still carries in every magnitude a
// measurement has; more would only round the binary representation error.
const maxPrecision = 15

// parsePrecision reads the query parameter precision, the number of decimal places to round
// floating point values to. nil means no rounding.
func parsePrecision(request *http.Request) (*int, error) {
	param := request.URL.Query().Get("precision")
	if param == "" {
		return nil, nil
	}
	precision, err := strconv.Atoi(param)
	if err != nil || precision < 0 || precision > maxPrecision {
		return nil, errors.New("invalid param precision, expected an integer from 0 to " + strconv.Itoa(maxPrecision))
	}
	return &precision, nil
}

// roundValue rounds a float64 to precision decimal places as math.Round(x * 10^n) / 10^n, halves
// away from zero like PostgreSQL's round. A decimal 5 at the cut whose binary value lies just below
// can land on either side in the product: 2.675 rounds to 2.68, 1.005 to 1.0. It runs after the
// characteristic conversion, whose results are as long as any other.
// Integers, text, booleans and JSON values stay as they are, and so do NaN and the infinities.
func roundValue(value interface{}, precision int) interface{} {
	f, ok := value.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return value
	}
	scale := math.Pow10(precision)
	scaled := f * scale
	if math.Abs(scaled) >= 1<<53 {
		return value // a float64 this large has no digits below the requested place
	}
	rounded := math.Round(scaled) / scale
	if rounded == 0 {
		return 0.0 // not -0, which encoding/json writes as such
	}
	return rounded
}

// roundOptional rounds value if a precision is set.
func roundOptional(value interface{}, precision *int) interface{} {
	if precision == nil {
		return value
	}
	return roundValue(value, *precision)
}

// roundRows rounds the values of rows in place. Column 0 is the time.
func roundRows(rows [][]interface{}, precision int) {
	for _, row := range rows {
		for i := 1; i < len(row); i++ {
			row[i] = roundValue(row[i], precision)
		}
	}
}

// roundResponse rounds the result of formatResponse in place, a table or one series per element.
func roundResponse(response interface{}, precision *int) {
	if precision == nil {
		return
	}
	switch data := response.(type) {
	case [][]interface{}:
		roundRows(data, *precision)
	case [][][]interface{}:
		for _, series := range data {
			roundRows(series, *precision)
		}
	}
}
