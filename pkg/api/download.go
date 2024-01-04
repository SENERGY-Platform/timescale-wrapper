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
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/api/util"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/julienschmidt/httprouter"
)

func init() {
	endpoints = append(endpoints, PrepareDownloadEndpoints)
	unauthenticatedEndpoints = append(unauthenticatedEndpoints, DownloadEndpoints)
}

func PrepareDownloadEndpoints(router *httprouter.Router, config configuration.Config, _ *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, _ *converter.Converter, _ deviceSelection.Client) {
	router.GET("/download", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		prepared, ok := prepareQueriesRequestElement(writer, request, verifier)
		if !ok {
			return
		}
		handleCSVDownload(prepared.QueriesRequestElement, prepared.TimeFormat, prepared.Token, writer, config)
	})

	router.GET("/prepare-download", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		prepared, ok := prepareQueriesRequestElement(writer, request, verifier)
		if !ok {
			return
		}

		secret, err := remoteCache.StoreSecretQuery(prepared)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, err = writer.Write([]byte(secret))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
	})
}

func DownloadEndpoints(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	router.GET("/download/:secret", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		prepared, err := remoteCache.GetSecretQuery(params.ByName("secret"))
		if err != nil {
			if err == memcache.ErrCacheMiss {
				http.Error(writer, "not found", http.StatusNotFound)
			} else {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		handleCSVDownload(prepared.QueriesRequestElement, prepared.TimeFormat, prepared.Token, writer, config)
	})
}

func prepareQueriesRequestElement(writer http.ResponseWriter, request *http.Request, verifier *verification.Verifier) (elem model.PreparedQueriesRequestElement, ok bool) {
	elem = model.PreparedQueriesRequestElement{}
	query := request.URL.Query().Get("query")
	timeFormat := request.URL.Query().Get("time_format")

	var requestElement model.QueriesRequestElement
	err := json.Unmarshal([]byte(query), &requestElement)
	if !requestElement.Valid() {
		http.Error(writer, "invalid query", http.StatusBadRequest)
		return elem, false
	}
	zero := 0
	requestElement.OrderColumnIndex = &zero
	asc := model.Asc
	requestElement.OrderDirection = &asc
	if requestElement.Time == nil {
		http.Error(writer, "need time element", http.StatusInternalServerError)
		return elem, false
	}

	if requestElement.Time.Start == nil {
		if requestElement.Time.Last != nil {
			end := time.Now().Format(time.RFC3339)
			requestElement.Time.End = &end
			d, err := time.ParseDuration(*requestElement.Time.Last)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return elem, false
			}
			start := time.Now().Add(-1 * d).Format(time.RFC3339)
			requestElement.Time.Start = &start
		} else if requestElement.Time.Ahead != nil {
			start := time.Now().Format(time.RFC3339)
			requestElement.Time.Start = &start
			d, err := time.ParseDuration(*requestElement.Time.Ahead)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return elem, false
			}
			end := time.Now().Add(d).Format(time.RFC3339)
			requestElement.Time.End = &end
		}
	}

	userId, err := getUserId(request)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return elem, false
	}
	ok, err = verifier.VerifyAccess([]model.QueriesRequestElement{requestElement}, getToken(request), userId)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return elem, false
	}
	if !ok {
		http.Error(writer, "not found", http.StatusNotFound)
		return elem, false
	}

	return model.PreparedQueriesRequestElement{
		QueriesRequestElement: requestElement,
		Token:                 getToken(request),
		TimeFormat:            timeFormat,
	}, true
}

func handleCSVDownload(requestElement model.QueriesRequestElement, timeFormat string, token string, writer http.ResponseWriter, config configuration.Config) {
	responseWriterWithStatusCodeLog, ok := writer.(*util.ResponseWriterWithStatusCodeLog)
	if !ok {
		log.Println("ERROR: not a ResponseWriterWithStatusCodeLog")
		http.Error(writer, "", http.StatusInternalServerError)
		return
	}
	flusher, ok := responseWriterWithStatusCodeLog.Parent.(http.Flusher)
	if !ok {
		log.Println("ERROR: not a flusher")
		http.Error(writer, "", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/csv")
	writer.Header().Set("Content-Disposition", "attachment; filename=\"download.csv\"")
	csvWriter := csv.NewWriter(writer)
	headers := []string{"time"}
	for j := range requestElement.Columns {
		headers = append(headers, requestElement.Columns[j].Name)
	}
	err := csvWriter.Write(headers)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	flusher.Flush() // no http.Error from now on! Use panic(http.ErrAbortHandler)

	endTime, err := time.Parse(time.RFC3339, *requestElement.Time.End)
	if err != nil {
		log.Println("ERROR:", err.Error())
		panic(http.ErrAbortHandler)
	}
	initialEndValue := endTime.Unix()
	startTime, err := time.Parse(time.RFC3339, *requestElement.Time.Start)
	if err != nil {
		log.Println("ERROR:", err.Error())
		panic(http.ErrAbortHandler)
	}

	startValue := startTime.Unix()
	var chunkSize int64 = 30 * 60 * 60 * 24
	endValue := int64(math.Min(float64(startValue+chunkSize), float64(initialEndValue)))

	for startValue < initialEndValue { // loop over time chunks
		start := time.Unix(startValue, 0).Format(time.RFC3339)
		requestElement.Time.Start = &start
		end := time.Unix(endValue, 0).Format(time.RFC3339)
		requestElement.Time.End = &end
		// post /queries
		b, err := json.Marshal([]model.QueriesRequestElement{requestElement})
		if err != nil {
			log.Println("ERROR:", err.Error())
			panic(http.ErrAbortHandler)
		}
		req, err := http.NewRequest(http.MethodPost, "http://localhost:"+config.ApiPort+"/queries?format=table&order_column_index=0&order_direction=asc&time_format="+timeFormat, bytes.NewBuffer(b))
		if err != nil {
			log.Println("ERROR:", err.Error())
			panic(http.ErrAbortHandler)
		}
		req.Header.Set("Authorization", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Println("ERROR:", err.Error())
			panic(http.ErrAbortHandler)
		}
		if resp.StatusCode != 200 {
			reason, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Println("ERROR:", err.Error())
				panic(http.ErrAbortHandler)
			}
			log.Println("ERROR:", string(reason))
			panic(http.ErrAbortHandler)
		}
		var respData [][]interface{}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		if err != nil {
			log.Println("ERROR:", err.Error())
			panic(http.ErrAbortHandler)
		}
		err = writeCsv(respData, csvWriter)
		if err != nil {
			log.Println("ERROR:", err.Error())
			panic(http.ErrAbortHandler)
		}

		startValue = endValue
		endValue = int64(math.Min(float64(endValue+chunkSize), float64(initialEndValue)))
	}
}
