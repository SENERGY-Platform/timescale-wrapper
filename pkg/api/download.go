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
	"errors"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gin-gonic/gin"
)

func init() {
	endpoints = append(endpoints, PrepareDownloadEndpoints)
	unauthenticatedEndpoints = append(unauthenticatedEndpoints, DownloadEndpoints)
}

// Query godoc
// @Summary      download
// @Description  download CSV
// @Accept       json
// @Produce      plain
// @Security Bearer
// @Param        query query string true "JSON encoded QueriesRequestElement"
// @Param        time_format query string false "Textual representation of the date 'Mon Jan 2 15:04:05 -0700 MST 2006'. Example: 2006-01-02T15:04:05.000Z07:00 would format timestamps as rfc3339 with ms precision. Find details here: https://golang.org/pkg/time/#Time.Format"
// @Success      200 {file}  CSV file
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /download [GET]
func GetDownload() {} // for doc generation

// Query godoc
// @Summary      prepare download
// @Description  genartes a secret for later download. can be used in native browser downloads
// @Accept       json
// @Produce      plain
// @Security Bearer
// @Param        query query string true "JSON encoded QueriesRequestElement"
// @Param        time_format query string false "Textual representation of the date 'Mon Jan 2 15:04:05 -0700 MST 2006'. Example: 2006-01-02T15:04:05.000Z07:00 would format timestamps as rfc3339 with ms precision. Find details here: https://golang.org/pkg/time/#Time.Format"
// @Success      200 {string} Secret
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /prepare-download [GET]
func GetPrepareDownload() {} // for doc generation

// Query godoc
// @Summary      prepare download
// @Description  genartes a secret for later download. can be used in native browser downloads
// @Accept       json
// @Produce      plain
// @Security Bearer
// @Param        query body string true "JSON encoded QueriesRequestElement"
// @Param        time_format query string false "Textual representation of the date 'Mon Jan 2 15:04:05 -0700 MST 2006'. Example: 2006-01-02T15:04:05.000Z07:00 would format timestamps as rfc3339 with ms precision. Find details here: https://golang.org/pkg/time/#Time.Format"
// @Success      200 {string} Secret
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /prepare-download [POST]
func PostPrepareDownload() {} // for doc generation

func PrepareDownloadEndpoints(router gin.IRouter, config configuration.Config, _ *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, _ *converter.Converter, _ deviceSelection.Client) {
	router.GET("/download", func(c *gin.Context) {
		writer := c.Writer
		request := c.Request
		marshalledQuery := request.URL.Query().Get("query")
		prepared, ok := prepareQueriesRequestElement(c, request, verifier, marshalledQuery)
		if !ok {
			return
		}
		handleCSVDownload(c, prepared.QueriesRequestElement, prepared.TimeFormat, prepared.Token, writer, config)
	})

	handleSecretGeneration := func(c *gin.Context, prepared model.PreparedQueriesRequestElement) {
		writer := c.Writer
		secret, err := remoteCache.StoreSecretQuery(c.Request.Context(), prepared)
		if err != nil {
			c.Error(errors.Join(err, model.ErrInternalServerError))
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, err = writer.Write([]byte(secret))
		if err != nil {
			c.Error(errors.Join(err, model.ErrInternalServerError))
			return
		}
	}

	router.GET("/prepare-download", func(c *gin.Context) {
		request := c.Request
		marshalledQuery := request.URL.Query().Get("query")
		prepared, ok := prepareQueriesRequestElement(c, request, verifier, marshalledQuery)
		if !ok {
			c.Error(model.ErrInternalServerError)
			return
		}

		handleSecretGeneration(c, prepared)
	})

	router.POST("/prepare-download", func(c *gin.Context) {
		request := c.Request
		var marshalledQuery string
		if err := c.ShouldBindPlain(&marshalledQuery); err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return
		}
		prepared, ok := prepareQueriesRequestElement(c, request, verifier, marshalledQuery)
		if !ok {
			c.Error(model.ErrInternalServerError)
			return
		}

		handleSecretGeneration(c, prepared)
	})
}

// Query godoc
// @Summary      download
// @Description  downloads CSV file with previously prepared secret
// @Produce      plain
// @Param        secret path string true "secret"
// @Success      200 {file} CSV file
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /download/{secret} [GET]
func DownloadEndpoints(router gin.IRouter, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	router.GET("/download/:secret", func(c *gin.Context) {
		writer := c.Writer
		prepared, err := remoteCache.GetSecretQuery(c.Request.Context(), c.Param("secret"))
		if err != nil {
			if err == memcache.ErrCacheMiss {
				c.Error(errors.Join(errors.New("not found"), model.ErrNotFound))
			} else {
				c.Error(errors.Join(err, model.ErrInternalServerError))
			}
			return
		}

		handleCSVDownload(c, prepared.QueriesRequestElement, prepared.TimeFormat, prepared.Token, writer, config)
	})
}

func prepareQueriesRequestElement(c *gin.Context, request *http.Request, verifier *verification.Verifier, marshalledQuery string) (elem model.PreparedQueriesRequestElement, ok bool) {
	elem = model.PreparedQueriesRequestElement{}
	timeFormat := request.URL.Query().Get("time_format")

	var requestElement model.QueriesRequestElement
	err := json.Unmarshal([]byte(marshalledQuery), &requestElement)
	if !requestElement.Valid() {
		c.Error(errors.Join(errors.New("invalid query"), model.ErrBadRequest))
		return elem, false
	}
	zero := 0
	requestElement.OrderColumnIndex = &zero
	asc := model.Asc
	requestElement.OrderDirection = &asc
	if requestElement.Time == nil {
		c.Error(errors.Join(errors.New("need time element"), model.ErrInternalServerError))
		return elem, false
	}

	if requestElement.Time.Start == nil {
		if requestElement.Time.Last != nil {
			end := time.Now().Format(time.RFC3339)
			requestElement.Time.End = &end
			d, err := time.ParseDuration(*requestElement.Time.Last)
			if err != nil {
				c.Error(errors.Join(err, model.ErrInternalServerError))
				return elem, false
			}
			start := time.Now().Add(-1 * d).Format(time.RFC3339)
			requestElement.Time.Start = &start
		} else if requestElement.Time.Ahead != nil {
			start := time.Now().Format(time.RFC3339)
			requestElement.Time.Start = &start
			d, err := time.ParseDuration(*requestElement.Time.Ahead)
			if err != nil {
				c.Error(errors.Join(err, model.ErrInternalServerError))
				return elem, false
			}
			end := time.Now().Add(d).Format(time.RFC3339)
			requestElement.Time.End = &end
		}
	}

	userId, err := getUserId(request)
	if err != nil {
		c.Error(errors.Join(err, model.ErrBadRequest))
		return elem, false
	}
	ok, _, err = verifier.VerifyAccess(c.Request.Context(), []model.QueriesRequestElement{requestElement}, getToken(request), userId)
	if err != nil {
		c.Error(errors.Join(err, model.ErrInternalServerError))
		return elem, false
	}
	if !ok {
		c.Error(errors.Join(errors.New("not found"), model.ErrNotFound))
		return elem, false
	}

	return model.PreparedQueriesRequestElement{
		QueriesRequestElement: requestElement,
		Token:                 getToken(request),
		TimeFormat:            timeFormat,
	}, true
}

var httpClient = http.Client{
	Timeout: 2 * time.Minute,
}

func handleCSVDownload(c *gin.Context, requestElement model.QueriesRequestElement, timeFormat string, token string, writer http.ResponseWriter, config configuration.Config) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		log.Logger.ErrorContext(c, "not a flusher")
		c.Error(errors.Join(errors.New("not a flusher"), model.ErrInternalServerError))
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
		c.Error(errors.Join(err, model.ErrInternalServerError))
		return
	}
	flusher.Flush() // no http.Error from now on! Use panic(http.ErrAbortHandler)

	endTime, err := time.Parse(time.RFC3339, *requestElement.Time.End)
	if err != nil {
		log.Logger.ErrorContext(c, "failed to parse end time", attributes.ErrorKey, err)
		panic(http.ErrAbortHandler)
	}
	initialEndValue := endTime.Unix()
	startTime, err := time.Parse(time.RFC3339, *requestElement.Time.Start)
	if err != nil {
		log.Logger.ErrorContext(c, "failed to parse start time", attributes.ErrorKey, err)
		panic(http.ErrAbortHandler)
	}

	startValue := startTime.Unix()
	var chunkSize int64 = int64((time.Hour * 1).Seconds())
	endValue := int64(math.Min(float64(startValue+chunkSize), float64(initialEndValue)))

	/* channel setup:

	chunks: for sending query results to the csv writer goroutine. the main routine will query the db and writes result to the channel, the csv writer goroutine will read from the channel and write to the response. this allows to query the next chunk while the previous one is still being written to the response, which can improve performance for large downloads.

	writeErr: for receiving errors from the csv writer goroutine. if an error is received, the main goroutine will stop processing and return an error response. if nil is received, the main goroutine will stop processing and finish the request successfully. the csv writer goroutine will send nil if the chunks channel is closed and all chunks have been written successfully, signalling completion.
	*/
	chunks := make(chan [][]interface{}, 1)
	writeErr := make(chan error, 1)
	go func() {
		for chunk := range chunks {
			if err := writeCsv(chunk, csvWriter); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	for startValue < initialEndValue { // loop over time chunks
		start := time.Unix(startValue, 0).Format(time.RFC3339)
		requestElement.Time.Start = &start
		end := time.Unix(endValue, 0).Format(time.RFC3339)
		requestElement.Time.End = &end
		// post /queries
		b, err := json.Marshal([]model.QueriesRequestElement{requestElement})
		if err != nil {
			log.Logger.ErrorContext(c, "failed to marshal query request", attributes.ErrorKey, err)
			panic(http.ErrAbortHandler)
		}
		req, err := http.NewRequest(http.MethodPost, "http://localhost:"+config.ApiPort+"/queries?format=table&order_column_index=0&order_direction=asc&time_format="+timeFormat, bytes.NewBuffer(b))
		if err != nil {
			log.Logger.ErrorContext(c, "failed to create query request", attributes.ErrorKey, err)
			panic(http.ErrAbortHandler)
		}
		req.Header.Set("Authorization", token)
		resp, err := httpClient.Do(req)
		if err != nil {
			log.Logger.ErrorContext(c, "failed to execute query request", attributes.ErrorKey, err)
			panic(http.ErrAbortHandler)
		}
		if resp.StatusCode != 200 {
			reason, err := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if closeErr != nil {
				log.Logger.ErrorContext(c, "failed to close query error response body", attributes.ErrorKey, closeErr)
				panic(http.ErrAbortHandler)
			}
			if err != nil {
				log.Logger.ErrorContext(c, "failed to read query error response", attributes.ErrorKey, err)
				panic(http.ErrAbortHandler)
			}
			log.Logger.ErrorContext(c, "query request returned non-200 status", attributes.ErrorKey, string(reason))
			panic(http.ErrAbortHandler)
		}
		var respData [][]interface{}
		err = json.NewDecoder(resp.Body).Decode(&respData)
		closeErr := resp.Body.Close()
		if closeErr != nil {
			log.Logger.ErrorContext(c, "failed to close query response body", attributes.ErrorKey, closeErr)
			panic(http.ErrAbortHandler)
		}
		if err != nil {
			log.Logger.ErrorContext(c, "failed to decode query response", attributes.ErrorKey, err)
			panic(http.ErrAbortHandler)
		}

		select {
		case err = <-writeErr:
			if err != nil {
				log.Logger.ErrorContext(c, "failed to write csv", attributes.ErrorKey, err)
			} else {
				log.Logger.ErrorContext(c, "csv writer stopped unexpectedly")
			}
			panic(http.ErrAbortHandler)
		case chunks <- respData:
		}

		startValue = endValue
		endValue = int64(math.Min(float64(endValue+chunkSize), float64(initialEndValue)))
	}

	close(chunks)
	err = <-writeErr
	if err != nil {
		log.Logger.ErrorContext(c, "failed to write csv", attributes.ErrorKey, err)
		panic(http.ErrAbortHandler)
	}
}
