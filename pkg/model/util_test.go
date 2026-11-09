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
	"math"
	"reflect"
	"testing"
	"time"
)

func TestSort2D(t *testing.T) {
	t.Parallel()
	t.Run("sort time.Time", func(t *testing.T) {
		small := time.Now()
		medium := small.Add(time.Minute)
		large := medium.Add(time.Minute)
		err := testBasicSort(small, medium, large)
		if err != nil {
			t.Error(err)
		}

	})

	t.Run("sort string", func(t *testing.T) {
		small := "a"
		medium := "b"
		large := "c"
		err := testBasicSort(small, medium, large)
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("sort bool & nil", func(t *testing.T) {
		var small bool // nil
		err := testBasicSort(small, false, true)
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("sort json.Number", func(t *testing.T) {
		small := json.Number("-1")
		medium := json.Number("0")
		large := json.Number("1")
		err := testBasicSort(small, medium, large)
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("sort int64", func(t *testing.T) {
		if err := testBasicSort(int64(-1), int64(0), int64(1)); err != nil {
			t.Error(err)
		}
	})

	// bigint columns and integral numeric results arrive as int64, fractional ones as float64
	t.Run("sort mixed int64 and float64", func(t *testing.T) {
		if err := testBasicSort(int64(2), 2.5, int64(3)); err != nil {
			t.Error(err)
		}
	})

	t.Run("sort json.Number by value", func(t *testing.T) {
		if err := testBasicSort(json.Number("9"), json.Number("10"), json.Number("11.5")); err != nil {
			t.Error(err)
		}
	})

	// two values beyond 2^53 are equal as float64
	t.Run("sort large int64 exactly", func(t *testing.T) {
		if err := testBasicSort(int64(1700000000000000000), int64(1700000000000000001), int64(1700000000000000002)); err != nil {
			t.Error(err)
		}
	})

	t.Run("sort int16", func(t *testing.T) {
		if err := testBasicSort(int16(-1), int16(0), int16(1)); err != nil {
			t.Error(err)
		}
	})

	// reflect.DeepEqual never matches NaN, so the positions are checked one by one
	t.Run("sort NaN before numbers", func(t *testing.T) {
		array := [][]interface{}{{2.0}, {math.NaN()}, {-1.0}, {math.NaN()}, {0.5}}
		if err := Sort2D(array, 0, Asc); err != nil {
			t.Fatal(err)
		}
		if !math.IsNaN(array[0][0].(float64)) || !math.IsNaN(array[1][0].(float64)) ||
			array[2][0] != -1.0 || array[3][0] != 0.5 || array[4][0] != 2.0 {
			t.Errorf("unexpected order %v", array)
		}
	})

	t.Run("index outside a row is an error", func(t *testing.T) {
		array := [][]interface{}{{1.0, 2.0}, {3.0}}
		if err := Sort2D(array, 1, Asc); err == nil {
			t.Error("expected an error")
		}
	})

	// a JSONB value can hold different types in one column; that must not panic
	t.Run("mixed types are an error", func(t *testing.T) {
		for _, values := range [][]interface{}{
			{"a", 1.0, "b"},
			{1.0, "a", 2.0},
			{true, "a", false},
			{time.Now(), "a", time.Now()},
			{map[string]interface{}{}, 1.0, 2.0},
		} {
			array := [][]interface{}{{values[0]}, {values[1]}, {values[2]}}
			if err := Sort2D(array, 0, Asc); err == nil {
				t.Errorf("expected an error for %#v", values)
			}
		}
	})

	t.Run("ensure correct index", func(t *testing.T) {
		small := "a"
		medium := "b"
		large := "c"
		array := [][]interface{}{
			{2, small},
			{1, medium},
			{0, large},
		}

		err := Sort2D(array, 1, Desc)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(array, [][]interface{}{{0, large}, {1, medium}, {2, small}}) {
			t.Error("sort failed")
		}

		err = Sort2D(array, 1, Asc)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(array, [][]interface{}{{2, small}, {1, medium}, {0, large}}) {
			t.Error("sort failed")
		}
	})
}

func testBasicSort(small interface{}, medium interface{}, large interface{}) error {
	array := [][]interface{}{
		{small},
		{medium},
		{large},
	}

	err := Sort2D(array, 0, Desc)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(array, [][]interface{}{{large}, {medium}, {small}}) {
		return errors.New("sort failed")
	}

	err = Sort2D(array, 0, Asc)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(array, [][]interface{}{{small}, {medium}, {large}}) {
		return errors.New("sort failed")
	}

	return nil
}

func TestLocalTime(t *testing.T) {
	t.Parallel()
	offset := time.Date(2022, 12, 6, 7, 0, 0, 0, time.FixedZone("", 3600))
	if local := LocalTime(offset); !local.Equal(offset) || local.Location() != time.Local {
		t.Errorf("expected %v in the local time zone, got %v", offset, local)
	}
	if zero := LocalTime(time.Time{}); !zero.IsZero() || zero.Location() != time.UTC {
		t.Errorf("expected the zero time unchanged, got %v", zero)
	}
}
