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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
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
			if !requestElements[i].Valid() {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
		}
		ok, err := verification.VerifyAccess(requestElements, getToken(request), getUserId(request), config)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if config.Debug {
			log.Println("DEBUG: Verification took " + time.Since(start).String())
		}
		beforeQueries := time.Now()
		if !ok {
			http.Error(writer, "not found", http.StatusNotFound)
			return
		}

		queries, err := timescale.GenerateQueries(requestElements)
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
		response, err := formatResponse(requestedFormat, requestElements, data, orderColumnIndex, orderDirection, timeFormat)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if config.Debug {
			log.Println("DEBUG: Postprocessing took " + time.Since(beforePP).String())
		}

		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(response)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})

}
