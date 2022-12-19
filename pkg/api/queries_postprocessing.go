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

package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"sort"
	"strings"
	"time"
)

func formatResponse(f model.Format, request []model.QueriesRequestElement, results [][][]interface{},
	orderColumnIndex int, orderDirection model.Direction, timeFormat string) (data interface{}, err error) {

	switch f {
	case model.Table:
		formatted, err := formatResponseAsTable(request, results, orderColumnIndex, orderDirection)
		if err != nil {
			return nil, err
		}
		if len(timeFormat) > 0 {
			formatTime2D(formatted, timeFormat)
		}
		for len(formatted) > 0 && isRowEmpty(formatted[len(formatted)-1]) {
			formatted = formatted[:len(formatted)-1]
		}
		return formatted, nil
	default:
		if len(timeFormat) > 0 {
			for i := range results {
				formatTime2D(results[i], timeFormat)
			}
		}
		for i := range results {
			for len(results[i]) > 0 && isRowEmpty(results[i][len(results[i])-1]) {
				results[i] = results[i][:len(results[i])-1]
			}
		}
		return results, nil
	}
}

func formatResponseAsTable(request []model.QueriesRequestElement, data [][][]interface{}, orderColumnIndex int, orderDirection model.Direction) (formatted [][]interface{}, err error) {
	totalColumns := 1
	baseIndex := map[int]int{}
	for requestIndex, element := range request {
		baseIndex[requestIndex] = totalColumns
		totalColumns += len(element.Columns)
	}

	for seriesIndex := range data {
		for rowIndex := range data[seriesIndex] {
			formattedRow := make([]interface{}, totalColumns)
			formattedRow[0] = data[seriesIndex][rowIndex][0]
			if formattedRow[0] == nil { // no data in series
				continue
			}
			for seriesColumnIndex := range request[seriesIndex].Columns {
				formattedRow[baseIndex[seriesIndex]+seriesColumnIndex] = data[seriesIndex][rowIndex][seriesColumnIndex+1]
			}
			for subSeriesIndex := range data {
				if subSeriesIndex <= seriesIndex {
					continue
				}
				timestamp, ok := data[seriesIndex][rowIndex][0].(time.Time)
				if !ok {
					continue
				}
				subRowIndex, ok := findFirstElementIndex(data[subSeriesIndex], timestamp, 0, len(data[subSeriesIndex])-1)
				if ok {
					for subSeriesColumnIndex := range request[subSeriesIndex].Columns {
						formattedRow[baseIndex[subSeriesIndex]+subSeriesColumnIndex] = data[subSeriesIndex][subRowIndex][subSeriesColumnIndex+1]
					}
					data[subSeriesIndex] = model.RemoveElementFrom2D(data[subSeriesIndex], subRowIndex)
					// sorting required for binary search
					sort.Slice(data[subSeriesIndex], func(i, j int) bool {
						return data[subSeriesIndex][i][0].(time.Time).After(data[subSeriesIndex][j][0].(time.Time))
					})
					continue
				}
			}
			formatted = append(formatted, formattedRow)
		}
	}
	err = model.Sort2D(formatted, orderColumnIndex, orderDirection)
	if err != nil {
		return formatted, err
	}
	return
}

func findFirstElementIndex(array [][]interface{}, t time.Time, left int, right int) (int, bool) {
	if left > right {
		return 0, false
	}
	mid := left + (right-left)/2
	if t.Equal(array[mid][0].(time.Time)) {
		for t.Equal(array[mid][0].(time.Time)) && mid > 0 {
			mid--
		}
		return mid, true
	}

	if array[mid][0].(time.Time).Before(t) {
		return findFirstElementIndex(array, t, left, mid-1)
	}
	return findFirstElementIndex(array, t, mid+1, right)
}

func formatTime2D(data [][]interface{}, timeFormat string) {
	for i := range data {
		data[i][0] = data[i][0].(time.Time).Format(timeFormat)
	}
}

func isRowEmpty(data []interface{}) bool {
	for i := range data {
		if i == 0 {
			continue
		}
		if data[i] != nil {
			return false
		}
	}
	return true
}

func writeCsv(data interface{}, writer *csv.Writer) (err error) {
	data3D, ok := data.([][][]interface{})
	if ok {
		for i := range data3D {
			for j := range data3D[i] {
				vals := make([]string, len(data3D[i][j]))
				for k := range data3D[i][j] {
					b, err := json.Marshal(data3D[i][j][k])
					if err != nil {
						return err
					}
					vals[k] = strings.Replace(string(b), "\"", "", -1)
				}
				err = writer.Write(vals)
				if err != nil {
					return err
				}
			}
		}
		writer.Flush()
		return nil
	}
	data2D, ok := data.([][]interface{})
	if !ok {
		return errors.New("data is not 2d or 3d")
	}
	for i := range data2D {
		vals := make([]string, len(data2D[i]))
		for j := range data2D[i] {
			b, err := json.Marshal(data2D[i][j])
			if err != nil {
				return err
			}
			vals[j] = strings.Replace(string(b), "\"", "", -1)
		}
		err = writer.Write(vals)
		if err != nil {
			return err
		}
	}
	writer.Flush()
	return nil
}
