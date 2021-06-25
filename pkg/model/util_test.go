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
