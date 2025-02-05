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
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
)

func init() {
	endpoints = append(endpoints, QueriesEndpoint)
}

func QueriesEndpoint(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	router.POST("/queries", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		start := time.Now()
		requestedFormat, orderColumnIndex, orderDirection, err := queriesParseQueryparams(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		var requestElements []model.QueriesRequestElement
		err = json.NewDecoder(request.Body).Decode(&requestElements)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		for i := range requestElements {
			if !requestElements[i].Valid() {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
			if requestElements[i].DeviceGroupId != nil {
				http.Error(writer, "Setting DeviceGroupId is invalid for /queries, use /queries/v2", http.StatusBadRequest)
				return
			}
		}

		userId, ownerUserIds, err, code := queriesVerify(requestElements, request, start, verifier, config)
		if err != nil {
			http.Error(writer, err.Error(), code)
			return
		}

		raw, dbRequestElements, dbRequestIndices := queriesGetFromCache(requestElements, remoteCache, config, nil)

		beforeQueries := time.Now()
		queries, err := wrapper.GenerateQueries(dbRequestElements, userId, ownerUserIds, "", []models.Device{})
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
		// merge DB results with remoteCache results
		for i := range data {
			raw[dbRequestIndices[i]] = data[i]
		}
		beforePP := time.Now()
		timeFormat := request.URL.Query().Get("time_format")
		response, err := formatResponse(remoteCache, requestedFormat, requestElements, raw, orderColumnIndex, orderDirection, timeFormat, converter)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if request.Header.Get("Accept") == "application/csv" || request.Header.Get("Accept") == "text/csv" {
			writer.Header().Set("Content-Type", request.Header.Get("Accept"))
			csvWriter := csv.NewWriter(writer)
			headers := []string{"time"}
			for i := range requestElements {
				for j := range requestElements[i].Columns {
					headers = append(headers, requestElements[i].Columns[j].Name)
				}
			}
			err = csvWriter.Write(headers)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
			err = writeCsv(response, csvWriter)
			if err != nil {
				fmt.Println("ERROR: " + err.Error())
			}
		} else {
			writer.Header().Set("Content-Type", "application/json")
			err = json.NewEncoder(writer).Encode(response)
			if err != nil {
				fmt.Println("ERROR: " + err.Error())
			}
		}

		if config.Debug {
			log.Println("DEBUG: Postprocessing took " + time.Since(beforePP).String())
		}

	})

}

func queriesParseQueryparams(request *http.Request) (requestedFormat model.Format, orderColumnIndex int, orderDirection model.Direction, err error) {
	requestedFormat = model.Format(request.URL.Query().Get("format"))
	paramOrderColumnIndex := request.URL.Query().Get("order_column_index")
	if paramOrderColumnIndex == "" {
		orderColumnIndex = 0
	} else {
		orderColumnIndex, err = strconv.Atoi(paramOrderColumnIndex)
		if err != nil {
			err = errors.New("invalid param order_column_index")
			return
		}
	}
	orderDirection = model.Direction(request.URL.Query().Get("order_direction"))
	if orderDirection == "" {
		orderDirection = model.Desc
	} else if orderDirection != "asc" && orderDirection != "desc" {
		err = errors.New("invalid param orderDirection")
		return
	}
	return
}

func queriesVerify(requestElements []model.QueriesRequestElement, request *http.Request, start time.Time, verifier *verification.Verifier, config configuration.Config) (userId string, ownerUserIds []string, err error, code int) {
	userId, err = getUserId(request)
	code = http.StatusInternalServerError
	if err != nil {
		return
	}
	ok, ownerUserIds, err := verifier.VerifyAccess(requestElements, getToken(request), userId)
	if err != nil {
		return
	}
	if config.Debug {
		log.Println("DEBUG: Verification took " + time.Since(start).String())
	}
	if !ok {
		code = http.StatusNotFound
		err = errors.New("not found")
		return
	}
	return
}

func queriesGetFromCache(requestElements []model.QueriesRequestElement, remoteCache *cache.RemoteCache, config configuration.Config, forceTz *string) (raw [][][]interface{}, dbRequestElements []model.QueriesRequestElement, dbRequestIndices []int) {
	dbRequestElements = []model.QueriesRequestElement{}
	dbRequestIndices = []int{}

	beforeCache := time.Now()
	raw = make([][][]interface{}, len(requestElements))

	m := sync.Mutex{}
	wg := sync.WaitGroup{}
	wg.Add(len(requestElements))
	for i := range requestElements {
		i := i
		go func() {
			var err error
			raw[i], err = remoteCache.GetLastValuesFromCache(requestElements[i], forceTz)
			if err != nil {
				m.Lock()
				defer m.Unlock()
				dbRequestElements = append(dbRequestElements, requestElements[i])
				dbRequestIndices = append(dbRequestIndices, i)
				if err != cache.NotCachableError {
					log.Println("WARN: Could not get data from remoteCache: " + err.Error())
				}
			}
			wg.Done()
		}()
	}
	wg.Wait()
	if config.Debug {
		log.Println("DEBUG: Cache collection took " + time.Since(beforeCache).String())
		log.Println("DEBUG: Got " + strconv.Itoa(len(requestElements)-len(dbRequestIndices)) + " from remoteCache, requesting " + strconv.Itoa(len(dbRequestIndices)) + " from db")
	}
	return
}
