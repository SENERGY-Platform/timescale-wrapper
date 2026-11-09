/*
 *    Copyright 2021 InfAI (CC SES)
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

package model

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

func ElementInArray(element interface{}, array []interface{}) bool {
	for _, comparison := range array {
		if comparison == element {
			return true
		}
	}
	return false
}

/*
Removes an element form an array. If the array was ordered before, it will loose that order.
*/
func RemoveElementFrom2D(array [][]interface{}, index int) [][]interface{} {
	array[len(array)-1], array[index] = array[index], array[len(array)-1]
	return array[:len(array)-1]
}

// Sorts a 2D array by the specified column index in the specified direction.
// Allowed direction values are Desc and Asc. Supported types are nil, time.Time, string, bool and the numbers
// compareNumbers accepts; numbers of different types compare by value.
// Errors are given if direction or type are unknown, or if index is outside a row.
func Sort2D(array [][]interface{}, index int, direction Direction) error {
	if direction != Desc && direction != Asc {
		return errors.New("unknown direction")
	}
	for _, row := range array {
		if index < 0 || index >= len(row) {
			return errors.New("sort index out of range")
		}
	}
	errFlag := false
	sort.Slice(array, func(i, j int) bool {
		if errFlag {
			return false
		}

		a, b := array[i][index], array[j][index]
		if a == nil && b == nil {
			return false
		}
		if a == nil {
			return direction == Asc
		}
		if b == nil {
			return direction == Desc
		}
		// every assertion on b is checked: a column of mixed types, as a JSONB value can be,
		// must end in an error and not in a panic, the sort runs on goroutines without recover
		switch valA := a.(type) {
		case time.Time:
			if valB, ok := b.(time.Time); ok {
				if direction == Desc {
					return valA.After(valB)
				}
				return valA.Before(valB)
			}
		case string:
			if valB, ok := b.(string); ok {
				if direction == Desc {
					return strings.Compare(valA, valB) > 0
				}
				return strings.Compare(valA, valB) < 0
			}
		case bool:
			if valB, ok := b.(bool); ok {
				if valA == valB {
					return false
				}
				if direction == Desc {
					return valA
				}
				return !valA
			}
		default:
			if c, ok := compareNumbers(a, b); ok {
				if direction == Desc {
					return c > 0
				}
				return c < 0
			}
		}
		errFlag = true
		return false
	})
	if errFlag {
		return errors.New("slice could not be sorted")
	}
	return nil
}

// compareNumbers compares a and b by value. Database rows carry int64 for bigint columns and for
// integral numeric results next to float64, and request bodies decoded with UseNumber carry
// json.Number, which must not compare as text ("10" before "9"). Two integers compare exactly, so
// bigint values beyond 2^53 keep their order; any other pair compares as float64 with cmp.Compare,
// which orders NaN before every other number.
func compareNumbers(a, b interface{}) (int, bool) {
	if intA, ok := integerValue(a); ok {
		if intB, ok := integerValue(b); ok {
			return cmp.Compare(intA, intB), true
		}
	}
	floatA, ok := floatValue(a)
	if !ok {
		return 0, false
	}
	floatB, ok := floatValue(b)
	if !ok {
		return 0, false
	}
	return cmp.Compare(floatA, floatB), true
}

func integerValue(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint:
		if uint64(n) <= math.MaxInt64 {
			return int64(n), true
		}
	case uint64:
		if n <= math.MaxInt64 {
			return int64(n), true
		}
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

func floatValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	if i, ok := integerValue(v); ok {
		return float64(i), true
	}
	return 0, false
}

// LocalTime returns t in the local time zone, the zone pgx returns timestamps in, so that the same
// instant reads the same from the cache and from the database. The zero time, which a cache entry
// without a time decodes to, stays as it is: in the local zone it would carry the local mean time
// offset of year 1, such as +00:53:28 for Europe/Berlin.
func LocalTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.Local()
}
