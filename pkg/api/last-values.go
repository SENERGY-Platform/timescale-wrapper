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

package api

import (
	"encoding/json"
	"fmt"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func init() {
	endpoints = append(endpoints, LastValuesEndpoint)
}

type queriesRequestElementColumn struct {
	*model.QueriesRequestElementColumn
	requestIndex int
}

func LastValuesEndpoint(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, lastValueCache *cache.LastValueCache) {
	router.POST("/last-values", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		start := time.Now()

		var requestElements []model.LastValuesRequestElement
		err := json.NewDecoder(request.Body).Decode(&requestElements)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		one := 1
		exportQueriesRequestElementColumnMap := map[string][]queriesRequestElementColumn{}
		deviceQueriesRequestElementColumnMap := map[string][]queriesRequestElementColumn{}

		for i := range requestElements {
			if requestElements[i].ExportId != nil {
				fillColumnMap(exportQueriesRequestElementColumnMap, *requestElements[i].ExportId, i, requestElements[i].ColumnName, requestElements[i].Math)
			} else if requestElements[i].DeviceId != nil && requestElements[i].ServiceId != nil {
				fillColumnMap(deviceQueriesRequestElementColumnMap, *requestElements[i].DeviceId+","+*requestElements[i].ServiceId, i, requestElements[i].ColumnName, requestElements[i].Math)
			} else {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
		}
		fullRequestElements := make([]model.QueriesRequestElement, len(exportQueriesRequestElementColumnMap)+len(deviceQueriesRequestElementColumnMap))
		outputIndexToInputIndex := map[int]int{} // Remember order of requested columns
		fullRequestElementsIndex := 0
		inserted := 0 // Count the number of requested columns
		for k, v := range exportQueriesRequestElementColumnMap {
			var cols []model.QueriesRequestElementColumn
			cols, inserted = prepColumns(inserted, outputIndexToInputIndex, v)
			fullRequestElements[fullRequestElementsIndex] = model.QueriesRequestElement{
				ExportId: &k,
				Time:     nil,
				Limit:    &one,
				Columns:  cols,
			}
			if !fullRequestElements[fullRequestElementsIndex].Valid() {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
			fullRequestElementsIndex++
		}
		for k, v := range deviceQueriesRequestElementColumnMap {
			s := strings.Split(k, ",")
			var cols []model.QueriesRequestElementColumn
			cols, inserted = prepColumns(inserted, outputIndexToInputIndex, v)
			fullRequestElements[fullRequestElementsIndex] = model.QueriesRequestElement{
				DeviceId:  &s[0],
				ServiceId: &s[1],
				Time:      nil,
				Limit:     &one,
				Columns:   cols,
			}
			if !fullRequestElements[fullRequestElementsIndex].Valid() {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
			fullRequestElementsIndex++
		}

		ok, err := verifier.VerifyAccess(fullRequestElements, getToken(request), getUserId(request))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if config.Debug {
			log.Println("DEBUG: Verification took " + time.Since(start).String())
		}
		if !ok {
			http.Error(writer, "not found", http.StatusNotFound)
			return
		}

		dbRequestElements := []model.QueriesRequestElement{}
		dbRequestIndices := []int{}

		beforeCache := time.Now()
		raw := make([][][]interface{}, len(fullRequestElements))

		wg := sync.WaitGroup{}
		wg.Add(len(fullRequestElements))
		for i := range fullRequestElements {
			i := i
			go func() {
				raw[i], err = lastValueCache.GetLastValuesFromCache(fullRequestElements[i])
				if err != nil {
					dbRequestElements = append(dbRequestElements, fullRequestElements[i])
					dbRequestIndices = append(dbRequestIndices, i)
					if err != cache.NotCachableError {
						log.Println("WARN: Could not get data from cache: " + err.Error())
					}
				}
				wg.Done()
			}()
		}
		wg.Wait()
		if config.Debug {
			log.Println("DEBUG: Cache collection took " + time.Since(beforeCache).String())
			log.Println("DEBUG: Got " + strconv.Itoa(len(fullRequestElements)-len(dbRequestIndices)) + " from cache, requesting " + strconv.Itoa(len(dbRequestIndices)) + " from db")
		}

		beforeQueries := time.Now()
		queries, err := timescale.GenerateQueries(dbRequestElements)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if config.Debug {
			log.Println("DEBUG: Query generation took " + time.Since(beforeQueries).String())
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
		// merge DB results with cache results
		for i := range data {
			raw[dbRequestIndices[i]] = data[i]
		}
		beforePP := time.Now()
		timeFormat := request.URL.Query().Get("time_format")
		if timeFormat == "" {
			timeFormat = time.RFC3339Nano
		}
		responseElements := make([]model.LastValuesResponseElement, len(requestElements))

		inserted = 0
		for i := range raw {
			var timeString string
			if len(raw[i]) > 0 && len(raw[i][0]) > 0 {
				dt, ok := raw[i][0][0].(time.Time)
				if ok {
					timeString = dt.Format(timeFormat)
				}
			}

			for j := range raw[i][0] {
				if j == 0 {
					continue
				}
				var timePointer *string
				if len(timeString) > 0 {
					timePointer = &timeString
				}
				// use known order to insert result at correct location
				responseElements[outputIndexToInputIndex[inserted]] = model.LastValuesResponseElement{
					Time:  timePointer,
					Value: raw[i][0][j],
				}
				inserted++
			}
		}

		if config.Debug {
			log.Println("DEBUG: Postprocessing took " + time.Since(beforePP).String())
		}

		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(responseElements)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})

}

func fillColumnMap(m map[string][]queriesRequestElementColumn, id string, i int, columnName string, columnMath *string) {
	existing, ok := m[id]
	if ok {
		existing = append(existing, queriesRequestElementColumn{
			requestIndex: i,
			QueriesRequestElementColumn: &model.QueriesRequestElementColumn{
				Name: columnName,
				Math: columnMath,
			}})
	} else {
		existing = []queriesRequestElementColumn{{
			requestIndex: i,
			QueriesRequestElementColumn: &model.QueriesRequestElementColumn{
				Name: columnName,
				Math: columnMath,
			}}}
	}
	m[id] = existing
}

func prepColumns(inserted int, outputIndexToInputIndex map[int]int, v []queriesRequestElementColumn) ([]model.QueriesRequestElementColumn, int) {
	cols := make([]model.QueriesRequestElementColumn, len(v))
	for j := range v {
		cols[j] = *v[j].QueriesRequestElementColumn
		outputIndexToInputIndex[inserted] = v[j].requestIndex
		inserted++
	}
	return cols, inserted
}
