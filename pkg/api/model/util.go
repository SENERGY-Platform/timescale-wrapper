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
	"encoding/json"
	"errors"
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

// Sorts a 2D array by the specified column index in the specified direction. Panics if index is out of bounds.
// Allowed direction values are Desc and Asc. Supported types are nil, time.Time, string, json.Number and bool.
// Errors are given if direction or type are unknown.
func Sort2D(array [][]interface{}, index int, direction Direction) error {
	if direction != Desc && direction != Asc {
		return errors.New("unknown direction")
	}
	errFlag := false
	sort.Slice(array, func(i, j int) bool {
		if errFlag {
			return false
		}

		if array[i][index] == nil && array[j][index] == nil {
			return true
		}

		if array[i][index] == nil {
			return direction == Asc
		}

		if array[j][index] == nil {
			return direction == Desc
		}

		_, ok := array[i][index].(time.Time)
		if ok {
			if direction == Desc {
				return array[i][index].(time.Time).After(array[j][index].(time.Time))
			} else {
				return array[i][index].(time.Time).Before(array[j][index].(time.Time))
			}
		}
		_, ok = array[i][index].(string)
		if ok {
			if direction == Desc {
				return strings.Compare(array[i][index].(string), array[j][index].(string)) > 0
			} else {
				return strings.Compare(array[i][index].(string), array[j][index].(string)) < 0
			}
		}
		_, ok = array[i][index].(json.Number)
		if ok {
			valI, err := array[i][index].(json.Number).Float64()
			if err != nil {
				errFlag = true
				return false
			}
			valJ, err := array[j][index].(json.Number).Float64()
			if err != nil {
				errFlag = true
				return false
			}
			if direction == Desc {
				return valI > valJ
			} else {
				return valI < valJ
			}
		}
		_, ok = array[i][index].(bool)
		if ok {
			if array[i][index].(bool) == array[j][index].(bool) {
				return true
			}
			if direction == Desc {
				return array[i][index].(bool)
			} else {
				return !array[i][index].(bool)
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
