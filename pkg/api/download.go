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
	"net/http"
	"net/url"
	"strconv"
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
// @Param        precision query int false "Number of decimal places floating point values are rounded to, 0 to 15. Integers and other types are returned as they are."
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
// @Param        precision query int false "Number of decimal places floating point values are rounded to, 0 to 15. Integers and other types are returned as they are."
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
// @Param        precision query int false "Number of decimal places floating point values are rounded to, 0 to 15. Integers and other types are returned as they are."
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
		handleCSVDownload(c, prepared.QueriesRequestElement, prepared.TimeFormat, prepared.Precision, prepared.Token, writer, config)
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
			return // prepareQueriesRequestElement has set the error
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
			return // prepareQueriesRequestElement has set the error
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

		handleCSVDownload(c, prepared.QueriesRequestElement, prepared.TimeFormat, prepared.Precision, prepared.Token, writer, config)
	})
}

func prepareQueriesRequestElement(c *gin.Context, request *http.Request, verifier *verification.Verifier, marshalledQuery string) (elem model.PreparedQueriesRequestElement, ok bool) {
	elem = model.PreparedQueriesRequestElement{}
	timeFormat := request.URL.Query().Get("time_format")
	precision, err := parsePrecision(request)
	if err != nil {
		c.Error(errors.Join(err, model.ErrBadRequest))
		return elem, false
	}

	var requestElement model.QueriesRequestElement
	err = json.Unmarshal([]byte(marshalledQuery), &requestElement)
	if !requestElement.Valid() {
		c.Error(errors.Join(errors.New("invalid query"), model.ErrBadRequest))
		return elem, false
	}
	zero := 0
	requestElement.OrderColumnIndex = &zero
	asc := model.Asc
	requestElement.OrderDirection = &asc
	if requestElement.Time == nil {
		c.Error(errors.Join(errors.New("need time element"), model.ErrBadRequest))
		return elem, false
	}

	if requestElement.GroupTime == nil {
		// raw values are read window by window, which takes a start and an end
		err = resolveRelativeDownloadTime(requestElement.Time, time.Now())
		if err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return elem, false
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
		Precision:             precision,
	}, true
}

// resolveRelativeDownloadTime replaces last or ahead by the start and end they mean at now: /queries refuses an
// element with last or ahead and a start.
func resolveRelativeDownloadTime(t *model.QueriesRequestElementTime, now time.Time) error {
	if t.Start != nil {
		return nil
	}
	from, to := now, now
	var err error
	switch {
	case t.Last != nil:
		from, err = model.AddTimeInterval(now, *t.Last, -1)
	case t.Ahead != nil:
		to, err = model.AddTimeInterval(now, *t.Ahead, 1)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	start := from.UTC().Format(time.RFC3339)
	end := to.UTC().Format(time.RFC3339)
	t.Start, t.End, t.Last, t.Ahead = &start, &end, nil, nil
	return nil
}

// downloadQueryParams builds the query string of the /queries requests a download is made of.
// time_format is user input and needs escaping: a "+" or a space in it broke the request.
func downloadQueryParams(timeFormat string, precision *int) string {
	params := url.Values{}
	params.Set("format", "table")
	params.Set("order_column_index", "0")
	params.Set("order_direction", "asc")
	params.Set("time_format", timeFormat)
	if precision != nil {
		params.Set("precision", strconv.Itoa(*precision))
	}
	return params.Encode()
}

// The windows of the /queries requests of a download of raw values follow the density of the data. The first window
// is an hour long; a window that returned fewer than half of downloadWindowRows rows is followed by one twice as
// long, one that returned more by one half as long, within downloadWindowMin and downloadWindowMax. Sparse data
// takes few requests (with windows of an hour, two years took 17520 requests, longer than the gateway waits for a
// byte), dense data keeps a request small.
const (
	downloadWindowRows = 10000
	downloadWindowMin  = time.Minute
	downloadWindowMax  = 24 * time.Hour
)

// nextDownloadWindow is the length of the window after one of length size that returned rows rows.
func nextDownloadWindow(size time.Duration, rows int) time.Duration {
	switch {
	case rows > downloadWindowRows:
		return max(size/2, downloadWindowMin)
	case rows < downloadWindowRows/2:
		return min(size*2, downloadWindowMax)
	}
	return size
}

var httpClient = http.Client{
	Timeout: 2 * time.Minute,
}

func handleCSVDownload(c *gin.Context, requestElement model.QueriesRequestElement, timeFormat string, precision *int, token string, writer http.ResponseWriter, config configuration.Config) {
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

	// query runs one /queries request for requestElement as it is at the call and returns its rows.
	query := func() [][]interface{} {
		// post /queries
		b, err := json.Marshal([]model.QueriesRequestElement{requestElement})
		if err != nil {
			log.Logger.ErrorContext(c, "failed to marshal query request", attributes.ErrorKey, err)
			panic(http.ErrAbortHandler)
		}
		req, err := http.NewRequest(http.MethodPost, "http://localhost:"+config.ApiPort+"/queries?"+downloadQueryParams(timeFormat, precision), bytes.NewBuffer(b))
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

		return respData
	}
	send := func(rows [][]interface{}) {
		select {
		case err := <-writeErr:
			if err != nil {
				log.Logger.ErrorContext(c, "failed to write csv", attributes.ErrorKey, err)
			} else {
				log.Logger.ErrorContext(c, "csv writer stopped unexpectedly")
			}
			panic(http.ErrAbortHandler)
		case chunks <- rows:
		}
	}

	if requestElement.GroupTime != nil {
		// One request over the whole window: a bucket split between two requests came out once per request, and
		// the limit of a grouped query counts the buckets of the whole window.
		send(query())
	} else {
		endTime, err := time.Parse(time.RFC3339, *requestElement.Time.End)
		if err != nil {
			log.Logger.ErrorContext(c, "failed to parse end time", attributes.ErrorKey, err)
			panic(http.ErrAbortHandler)
		}
		startTime, err := time.Parse(time.RFC3339, *requestElement.Time.Start)
		if err != nil {
			log.Logger.ErrorContext(c, "failed to parse start time", attributes.ErrorKey, err)
			panic(http.ErrAbortHandler)
		}
		// rows the limit still allows, -1 without a limit. /queries drops rows whose columns are all null after its
		// LIMIT, so the download may hold rows from beyond the first limit rows of the table.
		remaining := -1
		if requestElement.Limit != nil && *requestElement.Limit >= 0 {
			remaining = *requestElement.Limit
		}
		size := time.Hour
		for from := startTime; from.Before(endTime) && remaining != 0; {
			to := from.Add(size)
			if to.After(endTime) {
				to = endTime
			}
			// /queries leaves out both ends of its window, so every window after the first starts one microsecond,
			// the precision of timestamptz, before the end of the previous one: a point on the boundary of two
			// windows falls into the later one only.
			queryStart := from
			if !from.Equal(startTime) {
				queryStart = from.Add(-time.Microsecond)
			}
			start := queryStart.UTC().Format(time.RFC3339Nano)
			requestElement.Time.Start = &start
			end := to.UTC().Format(time.RFC3339Nano)
			requestElement.Time.End = &end
			if remaining > 0 {
				limit := remaining
				requestElement.Limit = &limit
			}
			rows := query()
			send(rows)
			if remaining > 0 {
				remaining = max(remaining-len(rows), 0)
			}
			from = to
			size = nextDownloadWindow(size, len(rows))
		}
	}

	close(chunks)
	err = <-writeErr
	if err != nil {
		log.Logger.ErrorContext(c, "failed to write csv", attributes.ErrorKey, err)
		panic(http.ErrAbortHandler)
	}
}
