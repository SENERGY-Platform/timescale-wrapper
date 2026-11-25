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

package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSort2DBranches(t *testing.T) {
	t.Parallel()
	column := func(array [][]interface{}) []interface{} {
		values := make([]interface{}, 0, len(array))
		for _, row := range array {
			values = append(values, row[0])
		}
		return values
	}

	t.Run("unknown direction is an error", func(t *testing.T) {
		array := [][]interface{}{{1.0}, {2.0}}
		err := Sort2D(array, 0, Direction("up"))
		if err == nil || err.Error() != "unknown direction" {
			t.Errorf("expected unknown direction error, got %v", err)
		}
	})

	t.Run("nil sorts first in asc and last in desc", func(t *testing.T) {
		asc := [][]interface{}{{2.0}, {nil}, {1.0}}
		if err := Sort2D(asc, 0, Asc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(column(asc), []interface{}{nil, 1.0, 2.0}) {
			t.Errorf("asc: unexpected order %v", column(asc))
		}

		desc := [][]interface{}{{nil}, {1.0}, {2.0}}
		if err := Sort2D(desc, 0, Desc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(column(desc), []interface{}{2.0, 1.0, nil}) {
			t.Errorf("desc: unexpected order %v", column(desc))
		}
	})

	t.Run("two nil values are equal", func(t *testing.T) {
		for _, direction := range []Direction{Asc, Desc} {
			array := [][]interface{}{{nil}, {nil}}
			if err := Sort2D(array, 0, direction); err != nil {
				t.Errorf("%s: unexpected error %v", direction, err)
			}
			if !reflect.DeepEqual(column(array), []interface{}{nil, nil}) {
				t.Errorf("%s: unexpected rows %v", direction, array)
			}
		}
	})

	// a string and an int in one column must end in an error, not a panic
	t.Run("string and int in one column is an error", func(t *testing.T) {
		for _, direction := range []Direction{Asc, Desc} {
			array := [][]interface{}{{"a"}, {1}, {"b"}}
			if err := Sort2D(array, 0, direction); err == nil {
				t.Errorf("%s: expected an error", direction)
			}
		}
	})

	t.Run("bool asc puts false first and desc puts true first", func(t *testing.T) {
		asc := [][]interface{}{{true}, {false}, {true}, {false}}
		if err := Sort2D(asc, 0, Asc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(column(asc), []interface{}{false, false, true, true}) {
			t.Errorf("asc: unexpected order %v", column(asc))
		}

		desc := [][]interface{}{{false}, {true}, {false}, {true}}
		if err := Sort2D(desc, 0, Desc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(column(desc), []interface{}{true, true, false, false}) {
			t.Errorf("desc: unexpected order %v", column(desc))
		}
	})
}

func TestCompareNumbersTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		a, b   interface{}
		want   int
		wantOK bool
	}{
		{"int8 below int64", int8(-3), int64(2), -1, true},
		{"int16 above float64", int16(7), float64(6.5), 1, true},
		{"int32 equal int64", int32(4), int64(4), 0, true},
		{"uint8 above int64", uint8(200), int64(199), 1, true},
		{"uint16 below float64", uint16(10), float64(10.5), -1, true},
		{"uint32 equal int64", uint32(1 << 31), int64(1 << 31), 0, true},
		{"uint equal int64", uint(3), int64(3), 0, true},
		{"uint64 above float64", uint64(42), float64(41.9), 1, true},
		// above MaxInt64 the integer path does not apply, the comparison runs over float64
		{"uint64 above MaxInt64 against int64", uint64(1) << 63, int64(1) << 62, 1, true},
		{"uint64 above MaxInt64 below float64", uint64(1) << 63, float64(1e19), -1, true},
		{"json.Number compares by value, not text", json.Number("10"), json.Number("9"), 1, true},
		{"json.Number against float64", json.Number("1.5"), float64(1.4), 1, true},
		{"non-numeric string is not ok", "abc", int64(1), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := compareNumbers(tt.a, tt.b)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("compareNumbers(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestRemoveElementFrom2D(t *testing.T) {
	t.Parallel()
	array := [][]interface{}{{"a"}, {"b"}, {"c"}}
	got := RemoveElementFrom2D(array, 0)
	// the last row takes the place of the removed one, so the order changes on purpose
	want := [][]interface{}{{"c"}, {"b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RemoveElementFrom2D(index 0) = %v, want %v", got, want)
	}
}
