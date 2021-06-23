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

/* TODO
func transformMeasurementColumnPairs(pairs []RequestElement) (unique uniqueMeasurementsColumns) {
	unique = uniqueMeasurementsColumns{
		Columns:      make(map[string]map[string]struct{}),
		Measurements: make(map[string]struct{}),
	}
	unique.Columns["time"] = make(map[string]struct{})
	unique.Columns["time"][""] = struct{}{}

	for _, pair := range pairs {
		_, columnKnown := unique.Columns[pair.ColumnName]
		if !columnKnown {
			unique.Columns[pair.ColumnName] = make(map[string]struct{})
		}
		if pair.Math != nil && *pair.Math != "" {
			unique.Columns[pair.ColumnName][*pair.Math] = struct{}{}
		} else {
			unique.Columns[pair.ColumnName][""] = struct{}{}
		}
		unique.Measurements[pair.Measurement] = struct{}{}
	}
	return unique
}

func findSeriesIndex(name string, series []models.Row) (index int, err error) {
	for index, s := range series {
		if s.Name == name {
			return index, nil
		}
	}
	return 0, ErrNotFound
}

func findColumnIndex(name string, series models.Row) (index int, err error) {
	for index, column := range series.Columns {
		if column == name {
			return index, nil
		}
	}
	return 0, ErrNotFound
}

func getColumnName(e RequestElement) (columnName string) {
	columnName = e.ColumnName
	if e.Math != nil {
		columnName += *e.Math
	}
	return columnName
}

 */
