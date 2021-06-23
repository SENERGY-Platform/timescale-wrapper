/*
 *    Copyright 2020 InfAI (CC SES)
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

package timescale
/*
import (
	"testing"
)

func TestUtil(t *testing.T) {
	t.Run("transformMeasurementColumnPairs", func(t *testing.T) {
		t.Run("empty pairs", func(t *testing.T) {
			actual := transformMeasurementColumnPairs([]RequestElement{})

			columns := make(map[string]map[string]struct{})
			columns["time"] = make(map[string]struct{})
			columns["time"][""] = struct{}{}
			expect := uniqueMeasurementsColumns{Columns: columns}

			if !uniqueMeasurementsColumnsEquals(actual, expect) {
				t.Fail()
			}
		})
		t.Run("invalid pair", func(t *testing.T) {
			actual := transformMeasurementColumnPairs([]RequestElement{
				{Measurement: ""},
			})

			columns := make(map[string]map[string]struct{})
			columns["time"] = make(map[string]struct{})
			columns["time"][""] = struct{}{}
			columns[""] = make(map[string]struct{})
			columns[""][""] = struct{}{}
			measurements := make(map[string]struct{})
			measurements[""] = struct{}{}
			expect := uniqueMeasurementsColumns{Columns: columns, Measurements: measurements}

			if !uniqueMeasurementsColumnsEquals(actual, expect) {
				t.Fail()
			}
		})
		t.Run("single pair", func(t *testing.T) {
			actual := transformMeasurementColumnPairs([]RequestElement{
				{
					Measurement: "m1",
					ColumnName:  "c1",
				},
			})

			columns := make(map[string]map[string]struct{})
			columns["c1"] = make(map[string]struct{})
			columns["c1"][""] = struct{}{}
			columns["time"] = make(map[string]struct{})
			columns["time"][""] = struct{}{}
			measurements := make(map[string]struct{})
			measurements["m1"] = struct{}{}
			expect := uniqueMeasurementsColumns{Columns: columns, Measurements: measurements}

			if !uniqueMeasurementsColumnsEquals(actual, expect) {
				t.Fail()
			}
		})
		t.Run("single pair with math", func(t *testing.T) {
			math := "+3"
			actual := transformMeasurementColumnPairs([]RequestElement{
				{
					Measurement: "m1",
					ColumnName:  "c1",
					Math:        &math,
				},
			})

			columns := make(map[string]map[string]struct{})
			columns["c1"] = make(map[string]struct{})
			columns["c1"][math] = struct{}{}
			columns["time"] = make(map[string]struct{})
			columns["time"][""] = struct{}{}
			measurements := make(map[string]struct{})
			measurements["m1"] = struct{}{}
			expect := uniqueMeasurementsColumns{Columns: columns, Measurements: measurements}

			if !uniqueMeasurementsColumnsEquals(actual, expect) {
				t.Fail()
			}
		})
		t.Run("multiple pairs", func(t *testing.T) {
			actual := transformMeasurementColumnPairs([]RequestElement{
				{
					Measurement: "m1",
					ColumnName:  "c1",
				},
				{
					Measurement: "m2",
					ColumnName:  "c2",
				},
			})

			columns := make(map[string]map[string]struct{})
			columns["time"] = make(map[string]struct{})
			columns["time"][""] = struct{}{}
			columns["c1"] = make(map[string]struct{})
			columns["c1"][""] = struct{}{}
			columns["c2"] = make(map[string]struct{})
			columns["c2"][""] = struct{}{}
			measurements := make(map[string]struct{})
			measurements["m1"] = struct{}{}
			measurements["m2"] = struct{}{}
			expect := uniqueMeasurementsColumns{Columns: columns, Measurements: measurements}

			if !uniqueMeasurementsColumnsEquals(actual, expect) {
				t.Fail()
			}
		})
		t.Run("multiple pairs with math", func(t *testing.T) {
			math1 := "-25"
			math2 := "-13"
			actual := transformMeasurementColumnPairs([]RequestElement{
				{
					Measurement: "m1",
					ColumnName:  "c1",
					Math:        &math1,
				},
				{
					Measurement: "m2",
					ColumnName:  "c2",
					Math:        &math2,
				},
				{
					Measurement: "m2",
					ColumnName:  "c2",
				},
			})

			columns := make(map[string]map[string]struct{})
			columns["time"] = make(map[string]struct{})
			columns["time"][""] = struct{}{}
			columns["c1"] = make(map[string]struct{})
			columns["c1"][math1] = struct{}{}
			columns["c2"] = make(map[string]struct{})
			columns["c2"][""] = struct{}{}
			columns["c2"][math2] = struct{}{}
			measurements := make(map[string]struct{})
			measurements["m1"] = struct{}{}
			measurements["m2"] = struct{}{}
			expect := uniqueMeasurementsColumns{Columns: columns, Measurements: measurements}

			if !uniqueMeasurementsColumnsEquals(actual, expect) {
				t.Fail()
			}
		})
	})

	t.Run("findSeriesIndex", func(t *testing.T) {
		series := []models.Row{
			{
				Name: "test",
			},
			{
				Name: "test2",
			},
		}
		t.Run("empty name", func(t *testing.T) {
			n, err := findSeriesIndex("", series)
			if err != ErrNotFound {
				t.Fail()
			}
			if n != 0 {
				t.Fail()
			}
		})
		t.Run("empty series", func(t *testing.T) {
			n, err := findSeriesIndex("test", []models.Row{})
			if err != ErrNotFound {
				t.Fail()
			}
			if n != 0 {
				t.Fail()
			}
		})
		t.Run("not found", func(t *testing.T) {
			n, err := findSeriesIndex("no", series)
			if err != ErrNotFound {
				t.Fail()
			}
			if n != 0 {
				t.Fail()
			}
		})
		t.Run("found", func(t *testing.T) {
			n, err := findSeriesIndex("test2", series)
			if err != nil {
				t.Fail()
			}
			if n != 1 {
				t.Fail()
			}
		})
	})

	t.Run("findColumnIndex", func(t *testing.T) {
		series := models.Row{
			Columns: []string{
				"c1", "c2",
			},
		}
		t.Run("empty name", func(t *testing.T) {
			n, err := findColumnIndex("", series)
			if err != ErrNotFound {
				t.Fail()
			}
			if n != 0 {
				t.Fail()
			}
		})
		t.Run("empty series", func(t *testing.T) {
			n, err := findColumnIndex("test", models.Row{})
			if err != ErrNotFound {
				t.Fail()
			}
			if n != 0 {
				t.Fail()
			}
		})
		t.Run("not found", func(t *testing.T) {
			n, err := findColumnIndex("no", series)
			if err != ErrNotFound {
				t.Fail()
			}
			if n != 0 {
				t.Fail()
			}
		})
		t.Run("found", func(t *testing.T) {
			n, err := findColumnIndex("c2", series)
			if err != nil {
				t.Fail()
			}
			if n != 1 {
				t.Fail()
			}
		})
	})
}

type netError struct {
	error
}

func (n netError) Error() string {
	return n.error.Error()
}
func (n netError) Timeout() bool {
	return true
}
func (n netError) Temporary() bool {
	return true
}

func uniqueMeasurementsColumnsEquals(u1 uniqueMeasurementsColumns, u2 uniqueMeasurementsColumns) bool {
	if !mapStringStructEquals(u1.Measurements, u2.Measurements) {
		return false
	}
	if len(u1.Columns) != len(u2.Columns) {
		return false
	}
	for column := range u1.Columns {
		u2Column, ok := u2.Columns[column]
		if !ok {
			return false
		}
		if !mapStringStructEquals(u1.Columns[column], u2Column) {
			return false
		}
	}
	return true
}

func mapStringStructEquals(m1 map[string]struct{}, m2 map[string]struct{}) bool {
	if len(m1) != len(m2) {
		return false
	}
	for key := range m1 {
		_, ok := m2[key]
		if !ok {
			return false
		}
	}
	return true
}


 */
