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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"sort"
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
		return formatted, nil
	default:
		if len(timeFormat) > 0 {
			for i := range results {
				formatTime2D(results[i], timeFormat)
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
			for seriesColumnIndex := range request[seriesIndex].Columns {
				formattedRow[baseIndex[seriesIndex]+seriesColumnIndex] = data[seriesIndex][rowIndex][seriesColumnIndex+1]
			}
			for subSeriesIndex := range data {
				if subSeriesIndex <= seriesIndex {
					continue
				}
				subRowIndex, ok := findFirstElementIndex(data[subSeriesIndex], data[seriesIndex][rowIndex][0].(time.Time), 0, len(data[subSeriesIndex])-1)
				if ok {
					if data[subSeriesIndex][subRowIndex][0] == data[seriesIndex][rowIndex][0] {
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
