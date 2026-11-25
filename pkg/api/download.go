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
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gin-gonic/gin"
)

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

func PrepareDownloadEndpoints(router gin.IRouter, d deps) {
	p := d.pipeline

	router.GET("/download", func(c *gin.Context) {
		prepared, userId, ownerUserIds, err := prepareQueriesRequestElement(c.Request, d.verifier, c.Request.URL.Query().Get("query"))
		if err != nil {
			c.Error(err)
			return
		}
		serveDownload(c, p, prepared, userId, ownerUserIds)
	})

	// prepareSecret verifies the query, stores it and answers with the secret of its download.
	prepareSecret := func(c *gin.Context, marshalledQuery string) {
		prepared, _, _, err := prepareQueriesRequestElement(c.Request, d.verifier, marshalledQuery)
		if err != nil {
			c.Error(err)
			return
		}
		secret, err := d.remoteCache.StoreSecretQuery(c.Request.Context(), prepared)
		if err != nil {
			c.Error(errors.Join(err, model.ErrInternalServerError))
			return
		}
		c.Writer.Header().Set("Content-Type", "text/plain")
		if _, err = c.Writer.Write([]byte(secret)); err != nil {
			c.Error(errors.Join(err, model.ErrInternalServerError))
			return
		}
	}

	router.GET("/prepare-download", func(c *gin.Context) {
		prepareSecret(c, c.Request.URL.Query().Get("query"))
	})

	router.POST("/prepare-download", func(c *gin.Context) {
		var marshalledQuery string
		if err := c.ShouldBindPlain(&marshalledQuery); err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return
		}
		prepareSecret(c, marshalledQuery)
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
func DownloadEndpoints(router gin.IRouter, d deps) {
	p := d.pipeline
	router.GET("/download/:secret", func(c *gin.Context) {
		prepared, err := d.remoteCache.GetSecretQuery(c.Request.Context(), c.Param("secret"))
		if err != nil {
			if errors.Is(err, memcache.ErrCacheMiss) {
				c.Error(errors.Join(errors.New("not found"), model.ErrNotFound))
			} else {
				c.Error(classify(err, model.ErrInternalServerError))
			}
			return
		}
		// The download reads the data with the rights of the user who prepared it. The user and the owners of the
		// element are not part of the stored query, so access is checked once more here, once per download and
		// before the first byte is sent.
		tokenRequest, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "/", nil)
		if err != nil {
			c.Error(classify(err, model.ErrInternalServerError))
			return
		}
		tokenRequest.Header.Set("Authorization", prepared.Token)
		userId, ownerUserIds, err := verifyRequest(c.Request.Context(), tokenRequest, d.verifier, []model.QueriesRequestElement{prepared.QueriesRequestElement})
		if err != nil {
			c.Error(err)
			return
		}
		serveDownload(c, p, prepared, userId, ownerUserIds)
	})
}

// prepareQueriesRequestElement reads and checks the query of a download. The errors it returns carry one status
// class. userId and ownerUserIds are the result of the access check, see verifyRequest.
func prepareQueriesRequestElement(request *http.Request, verifier accessVerifier, marshalledQuery string) (elem model.PreparedQueriesRequestElement, userId string, ownerUserIds []string, err error) {
	timeFormat := request.URL.Query().Get("time_format")
	precision, err := parsePrecision(request)
	if err != nil {
		return elem, "", nil, errors.Join(err, model.ErrBadRequest)
	}

	var requestElement model.QueriesRequestElement
	err = json.Unmarshal([]byte(marshalledQuery), &requestElement)
	if err != nil {
		return elem, "", nil, errors.Join(fmt.Errorf("invalid query: %w", err), model.ErrBadRequest)
	}
	if !requestElement.Valid() {
		return elem, "", nil, errors.Join(errors.New("invalid query"), model.ErrBadRequest)
	}
	// the download reads through the pipeline of /queries, which has no device groups either
	if err = rejectDeviceGroups([]model.QueriesRequestElement{requestElement}); err != nil {
		return elem, "", nil, err
	}
	zero := 0
	requestElement.OrderColumnIndex = &zero
	asc := model.Asc
	requestElement.OrderDirection = &asc
	if requestElement.Time == nil {
		return elem, "", nil, errors.Join(errors.New("need time element"), model.ErrBadRequest)
	}

	if requestElement.GroupTime == nil {
		// raw values are read window by window, which takes a start and an end
		err = resolveRelativeDownloadTime(requestElement.Time, time.Now())
		if err != nil {
			return elem, "", nil, errors.Join(err, model.ErrBadRequest)
		}
	}

	userId, ownerUserIds, err = verifyRequest(request.Context(), request, verifier, []model.QueriesRequestElement{requestElement})
	if err != nil {
		return elem, "", nil, err
	}

	return model.PreparedQueriesRequestElement{
		QueriesRequestElement: requestElement,
		Token:                 getToken(request),
		TimeFormat:            timeFormat,
		Precision:             precision,
	}, userId, ownerUserIds, nil
}

// resolveRelativeDownloadTime replaces last or ahead by the start and end they mean at now: /queries refuses an
// element with last or ahead and a start.
func resolveRelativeDownloadTime(t *model.QueriesRequestElementTime, now time.Time) error {
	if t.Start != nil {
		return nil
	}
	if t.Last == nil && t.Ahead == nil {
		return nil
	}
	from, to, err := t.Bounds(now)
	if err != nil {
		return err
	}
	start := from.UTC().Format(time.RFC3339)
	end := to.UTC().Format(time.RFC3339)
	t.Start, t.End, t.Last, t.Ahead = &start, &end, nil, nil
	return nil
}

// The windows a download of raw values is read in follow the density of the data. The first window
// is an hour long; a window that returned fewer than half of downloadWindowRows rows is followed by one twice as
// long, one that returned more by one half as long, within downloadWindowMin and downloadWindowMax. Sparse data
// takes few reads (with windows of an hour, two years took 17520 requests, longer than the gateway waits for a
// byte), dense data keeps a read small.
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

// serveDownload resolves the json paths of a verified download and streams it. The user and the owners are those
// of the access check of the element.
func serveDownload(c *gin.Context, p *pipeline, prepared model.PreparedQueriesRequestElement, userId string, ownerUserIds []string) {
	elements := []model.QueriesRequestElement{prepared.QueriesRequestElement}
	if err := p.resolvePaths(c.Request.Context(), p.newLookups(), elements); err != nil {
		c.Error(err)
		return
	}
	source := p.downloadSource(userId, ownerUserIds, prepared.TimeFormat, prepared.Precision)
	handleCSVDownload(c, elements[0], source, c.Writer)
}

// windowSource returns the rows of the table of one request element, as the CSV of a download takes them.
type windowSource func(ctx context.Context, element model.QueriesRequestElement) ([][]interface{}, error)

// downloadSource reads a window of a download through the steps of /queries with format=table, ascending by time:
// cache, database, table, time format and precision. The access check is not repeated per window, see
// prepareQueriesRequestElement.
func (p *pipeline) downloadSource(userId string, ownerUserIds []string, timeFormat string, precision *int) windowSource {
	return func(ctx context.Context, element model.QueriesRequestElement) ([][]interface{}, error) {
		elements := []model.QueriesRequestElement{element}
		raw, err := p.fetch(ctx, elements, userId, ownerUserIds, nil, []models.Device{})
		if err != nil {
			return nil, err
		}
		rows, err := p.formatTable(ctx, p.newLookups(), elements, raw, 0, model.Asc, timeFormat)
		if err != nil {
			return nil, err
		}
		if precision != nil {
			roundRows(rows, *precision)
		}
		for _, row := range rows {
			for i := range row {
				row[i] = csvCell(row[i])
			}
		}
		return rows, nil
	}
}

// csvCell gives a cell the value it had when the rows of a download passed through JSON, as they did when /queries
// answered over HTTP: writeCsv encodes a cell as JSON, and a number that went through JSON was a float64. Simple
// values encode the same before and after that round trip and are returned as they are. Integers are written
// exactly, also beyond 2^53, where the round trip had cut digits off. Values that are neither (maps, lists,
// structs) may change their keys or their numbers, so those take the round trip and keep its float64 numbers.
func csvCell(value interface{}) interface{} {
	switch value.(type) {
	case nil, string, bool, float64, time.Time, int, int8, int16, int32, int64, uint8, uint16, uint32:
		return value
	}
	b, err := json.Marshal(value)
	if err != nil {
		return value // writeCsv reports it
	}
	var decoded interface{}
	if err = json.Unmarshal(b, &decoded); err != nil {
		return value
	}
	return decoded
}

// downloadWindows hands out the requests a download is made of and keeps their bookkeeping. A download of raw
// values is read window by window, see nextDownloadWindow; a grouped download is one window. next returns the
// element of the next window, record takes the number of rows it returned.
type downloadWindows struct {
	element model.QueriesRequestElement
	grouped bool
	started bool

	start, end time.Time
	from       time.Time
	size       time.Duration
	// remaining is the number of rows the limit still allows, -1 without a limit. /queries drops rows whose columns
	// are all null after its LIMIT, so the download may hold rows from beyond the first limit rows of the table.
	remaining int
}

// newDownloadWindows returns the windows of element. An element of raw values needs a start and an end.
func newDownloadWindows(element model.QueriesRequestElement) (*downloadWindows, error) {
	w := &downloadWindows{element: element, size: time.Hour, remaining: -1}
	if element.GroupTime != nil {
		w.grouped = true
		return w, nil
	}
	if element.Time == nil || element.Time.Start == nil || element.Time.End == nil {
		return nil, errors.Join(errors.New("need start and end time"), model.ErrBadRequest)
	}
	var err error
	if w.end, err = time.Parse(time.RFC3339, *element.Time.End); err != nil {
		return nil, errors.Join(fmt.Errorf("invalid end time: %w", err), model.ErrBadRequest)
	}
	if w.start, err = time.Parse(time.RFC3339, *element.Time.Start); err != nil {
		return nil, errors.Join(fmt.Errorf("invalid start time: %w", err), model.ErrBadRequest)
	}
	w.from = w.start
	if element.Limit != nil && *element.Limit >= 0 {
		w.remaining = *element.Limit
	}
	return w, nil
}

// next returns the element of the next window, or false when the download is complete.
func (w *downloadWindows) next() (model.QueriesRequestElement, bool) {
	if w.grouped {
		// One request over the whole window: a bucket split between two requests came out once per request, and
		// the limit of a grouped query counts the buckets of the whole window.
		if w.started {
			return model.QueriesRequestElement{}, false
		}
		w.started = true
		return w.element, true
	}
	if !w.from.Before(w.end) || w.remaining == 0 {
		return model.QueriesRequestElement{}, false
	}
	to := w.from.Add(w.size)
	if to.After(w.end) {
		to = w.end
	}
	// /queries leaves out both ends of its window, so every window after the first starts one microsecond,
	// the precision of timestamptz, before the end of the previous one: a point on the boundary of two
	// windows falls into the later one only.
	queryStart := w.from
	if !w.from.Equal(w.start) {
		queryStart = w.from.Add(-time.Microsecond)
	}
	start := queryStart.UTC().Format(time.RFC3339Nano)
	end := to.UTC().Format(time.RFC3339Nano)
	element := w.element
	window := *w.element.Time
	window.Start, window.End = &start, &end
	element.Time = &window
	if w.remaining > 0 {
		limit := w.remaining
		element.Limit = &limit
	}
	w.from = to
	return element, true
}

// record takes the number of rows the window handed out last returned.
func (w *downloadWindows) record(rows int) {
	if w.grouped {
		return
	}
	if w.remaining > 0 {
		w.remaining = max(w.remaining-rows, 0)
	}
	w.size = nextDownloadWindow(w.size, rows)
}

// csvStream writes chunks of rows to a CSV writer on a goroutine of its own, so that the next window can be read
// while the previous one is written. The goroutine ends only when the chunks are closed: finish and abort, the
// only ways out, close them and take the one result of the writer. Nothing is left running, and nothing writes to
// the response after the handler returned.
type csvStream struct {
	chunks   chan [][]interface{}
	writeErr chan error
	once     sync.Once
	result   error
}

func newCSVStream(csvWriter *csv.Writer) *csvStream {
	s := &csvStream{chunks: make(chan [][]interface{}, 1), writeErr: make(chan error, 1)}
	go writeCSVChunks(s.chunks, csvWriter, s.writeErr)
	return s
}

// send hands rows to the writer. It fails if the writer has ended, which it has done on its own only when a write
// failed.
func (s *csvStream) send(rows [][]interface{}) error {
	select {
	case err := <-s.writeErr:
		// the writer has ended and its one result is taken here, so finish must not wait for it
		s.once.Do(func() {
			close(s.chunks)
			s.result = err
		})
		if err == nil {
			err = errors.New("csv writer stopped unexpectedly")
		}
		return fmt.Errorf("failed to write csv: %w", err)
	case s.chunks <- rows:
		return nil
	}
}

// finish waits until everything sent is written and returns the error of the writer. It is safe to call it again
// and after send failed.
func (s *csvStream) finish() error {
	s.once.Do(func() {
		close(s.chunks)
		s.result = <-s.writeErr
	})
	if s.result != nil {
		return fmt.Errorf("failed to write csv: %w", s.result)
	}
	return nil
}

// streamCSV reads the windows from source and writes their rows as CSV. It returns the first error and no longer
// has the writer goroutine running when it does. It does not panic itself, but a panic of source still ends the
// writer.
func streamCSV(ctx context.Context, windows *downloadWindows, source windowSource, csvWriter *csv.Writer) error {
	stream := newCSVStream(csvWriter)
	defer stream.finish() //nolint:errcheck // the error is returned below on the regular path
	for {
		element, ok := windows.next()
		if !ok {
			break
		}
		rows, err := source(ctx, element)
		if err != nil {
			return fmt.Errorf("failed to read window: %w", err)
		}
		if err = stream.send(rows); err != nil {
			return err
		}
		windows.record(len(rows))
	}
	return stream.finish()
}

// handleCSVDownload answers with the CSV of requestElement, the rows of which source returns. Everything that can
// be refused is refused before the headers are sent, as an error of c. From then on the status is 200 and a
// failure can only cut the response off: panic(http.ErrAbortHandler) below is the one place that does it, so that
// the client sees a broken download instead of a short file that looks complete.
func handleCSVDownload(c *gin.Context, requestElement model.QueriesRequestElement, source windowSource, writer http.ResponseWriter) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		log.Logger.ErrorContext(c, "not a flusher")
		c.Error(errors.Join(errors.New("not a flusher"), model.ErrInternalServerError))
		return
	}
	windows, err := newDownloadWindows(requestElement)
	if err != nil {
		c.Error(err)
		return
	}
	writer.Header().Set("Content-Type", "application/csv")
	writer.Header().Set("Content-Disposition", "attachment; filename=\"download.csv\"")
	csvWriter := csv.NewWriter(writer)
	headers := []string{"time"}
	for j := range requestElement.Columns {
		headers = append(headers, requestElement.Columns[j].Name)
	}
	err = csvWriter.Write(headers)
	if err != nil {
		c.Error(errors.Join(err, model.ErrInternalServerError))
		return
	}
	flusher.Flush() // no http.Error from now on!

	if err = streamCSV(c.Request.Context(), windows, source, csvWriter); err != nil {
		log.Logger.ErrorContext(c, "download aborted", attributes.ErrorKey, err)
		panic(http.ErrAbortHandler)
	}
}

// writeCSVChunks writes every chunk until chunks is closed, then sends nil to result. On the first
// error it sends the error and returns. It sends exactly once.
func writeCSVChunks(chunks <-chan [][]interface{}, csvWriter *csv.Writer, result chan<- error) {
	for chunk := range chunks {
		if err := writeCsv(chunk, csvWriter); err != nil {
			result <- err
			return
		}
	}
	result <- nil
}
