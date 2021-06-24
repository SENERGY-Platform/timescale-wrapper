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
	"encoding/json"
	"fmt"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/api/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"
)

func init() {
	endpoints = append(endpoints, QueriesEndpoint)
}

func QueriesEndpoint(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper) {
	router.POST("/queries", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		start := time.Now()
		requestedFormat := model.Format(request.URL.Query().Get("format"))

		var requestElements []model.QueriesRequestElement
		err := json.NewDecoder(request.Body).Decode(&requestElements)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		paramOrderColumnIndex := request.URL.Query().Get("order_column_index")
		var orderColumnIndex int
		if paramOrderColumnIndex == "" {
			orderColumnIndex = 0
		} else {
			orderColumnIndex, err = strconv.Atoi(paramOrderColumnIndex)
			if err != nil {
				http.Error(writer, "Invalid param order_column_index", http.StatusBadRequest)
				return
			}
		}
		orderDirection := model.Direction(request.URL.Query().Get("order_direction"))
		if orderDirection == "" {
			orderDirection = model.Desc
		} else if orderDirection != "asc" && orderDirection != "desc" {
			http.Error(writer, "Invalid param orderDirection", http.StatusBadRequest)
			return
		}

		for i := range requestElements {
			if !requestElements[i].Valid(requestedFormat) {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
			// TODO check ownership
		}
		timeDirection := model.Desc
		if orderColumnIndex == 0 {
			timeDirection = orderDirection
		}

		queries, err := timescale.GenerateQueries(requestElements, timeDirection)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if config.Debug {
			log.Println("DEBUG: Query generation took " + time.Since(start).String())
		}
		beforeQuery := time.Now()
		data, err := wrapper.ExecuteQueries(queries)
		if err != nil {
			http.Error(writer, err.Error(), timescale.GetHTTPErrorCode(err))
			return
		}
		if config.Debug {
			log.Println("DEBUG: Fetching took " + time.Since(beforeQuery).String())
		}

		/* TODO timeFormat := request.URL.Query().Get("time_format")
		response, err := formatResponse(requestedFormat, requestElements, data, orderColumnIndex, orderDirection, timeFormat)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}

		*/

		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(data)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})

}

func formatResponse(f model.Format, request []model.QueriesRequestElement, results [][]interface{},
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
		formatted, err := formatResponsePerQuery(request, results)
		if err != nil {
			return nil, err
		}
		if len(timeFormat) > 0 {
			for i := range formatted {
				formatTime2D(formatted[i], timeFormat)
			}
		}
		return formatted, nil
	}
}

func formatResponsePerQuery(request []model.QueriesRequestElement, results [][]interface{}) (formatted [][][]interface{}, err error) {
	/* TODO
	for index, result := range results {
		if result.Series == nil {
			// add empty column
			formatted = append(formatted, [][]interface{}{})
			continue
		}
		if len(result.Series) != 1 {
			return nil, errors.New("unexpected number of series")
		}
		// add data
		for rowIndex := range result.Series[0].Values {
			result.Series[0].Values[rowIndex][0], err = time.Parse(time.RFC3339, result.Series[0].Values[rowIndex][0].(string))
			if err != nil {
				return nil, err
			}
		}
		err = model.Sort2D(result.Series[0].Values, *request[index].OrderColumnIndex, *request[index].OrderDirection)
		if err != nil {
			return nil, err
		}
		formatted = append(formatted, result.Series[0].Values)
	}
	*/
	return
}

func formatResponseAsTable(request []model.QueriesRequestElement, results [][]interface{}, orderColumnIndex int, orderDirection model.Direction) (formatted [][]interface{}, err error) {
	data, err := formatResponsePerQuery(request, results)
	if err != nil {
		return nil, err
	}

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
