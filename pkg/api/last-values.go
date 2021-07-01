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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
	"time"
)

func init() {
	endpoints = append(endpoints, LastValuesEndpoint)
}

func LastValuesEndpoint(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper) {
	router.POST("/last-values", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		start := time.Now()

		var requestElements []model.LastValuesRequestElement
		err := json.NewDecoder(request.Body).Decode(&requestElements)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		one := 1
		fullRequestElements := make([]model.QueriesRequestElement, len(requestElements))
		for i := range requestElements {
			fullRequestElements[i] = model.QueriesRequestElement{
				ExportId:  requestElements[i].ExportId,
				DeviceId:  requestElements[i].DeviceId,
				ServiceId: requestElements[i].ServiceId,
				Time:      nil,
				Limit:     &one,
				Columns: []model.QueriesRequestElementColumn{{
					Name: requestElements[i].ColumnName,
					Math: requestElements[i].Math,
				}},
			}
			if !fullRequestElements[i].Valid() {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
		}
		ok, err := verification.VerifyAccess(fullRequestElements, getToken(request), getUserId(request), config)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(writer, "not found", http.StatusNotFound)
			return
		}
		if config.Debug {
			log.Println("DEBUG: Verification took " + time.Since(start).String())
		}
		beforeQueries := time.Now()

		queries, err := timescale.GenerateQueries(fullRequestElements)
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
		beforePP := time.Now()
		timeFormat := request.URL.Query().Get("time_format")
		if timeFormat == "" {
			timeFormat = time.RFC3339Nano
		}
		responseElements := make([]model.LastValuesResponseElement, len(data))
		for i := range data {
			if len(data[i]) != 1 {
				responseElements[i] = model.LastValuesResponseElement{
					Time:  nil,
					Value: nil,
				}
				continue
			}
			if len(data[i][0]) != 2 {
				http.Error(writer, "invalid data from db", http.StatusBadGateway)
				return
			}
			dt, ok := data[i][0][0].(time.Time)
			if !ok {
				http.Error(writer, "invalid data from db", http.StatusBadGateway)
				return
			}
			timeString := dt.Format(timeFormat)
			responseElements[i] = model.LastValuesResponseElement{
				Time:  &timeString,
				Value: data[0][0][1],
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
