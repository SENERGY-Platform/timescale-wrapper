/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
)

// windowWrapper stands in for the database: the i-th read of a download is answered with the rows respond returns,
// every read from failFrom on fails. It records the element of every read.
type windowWrapper struct {
	mux      sync.Mutex
	sent     []model.QueriesRequestElement
	respond  func(i int) [][]interface{}
	failFrom int // 0: never
}

func (w *windowWrapper) GenerateQueries(_ context.Context, elements []model.QueriesRequestElement, _ string, _ []string, _ string, _ []models.Device) ([]string, error) {
	w.mux.Lock()
	defer w.mux.Unlock()
	queries := make([]string, len(elements))
	for i := range elements {
		queries[i] = strconv.Itoa(len(w.sent))
		w.sent = append(w.sent, elements[i])
	}
	return queries, nil
}

func (w *windowWrapper) ExecuteQueries(_ context.Context, queries []string) ([][][]interface{}, error) {
	data := make([][][]interface{}, len(queries))
	for i, q := range queries {
		n, _ := strconv.Atoi(q)
		if w.failFrom > 0 && n+1 >= w.failFrom {
			return nil, errors.New("boom")
		}
		data[i] = w.respond(n)
	}
	return data, nil
}

// noCache answers no element from the cache.
type noCache struct{}

func (noCache) GetLastValuesFromCache(context.Context, model.QueriesRequestElement, *string) ([][]interface{}, error) {
	return nil, cache.NotCachableError
}

func downloadPipeline(w queryWrapper) *pipeline {
	return &pipeline{config: &configuration.ConfigStruct{}, wrapper: w, lookup: noCache{}}
}

// runDownload runs handleCSVDownload over a pipeline whose database answers the i-th read with the rows respond
// returns, and gives back the elements read and the CSV written.
func runDownload(t *testing.T, element model.QueriesRequestElement, respond func(i int) [][]interface{}) (sent []model.QueriesRequestElement, csv string) {
	t.Helper()
	w := &windowWrapper{respond: respond}
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
	handleCSVDownload(c, element, downloadPipeline(w).downloadSource("user", []string{"user"}, "", nil), c.Writer)
	return w.sent, recorder.Body.String()
}

func rawElement(start, end string) model.QueriesRequestElement {
	return model.QueriesRequestElement{
		Columns: []model.QueriesRequestElementColumn{{Name: "reading.value"}},
		Time:    &model.QueriesRequestElementTime{Start: &start, End: &end},
	}
}

// rows returns n rows of the database at the time at, with the row number as value.
func rows(n int, at string) [][]interface{} {
	ts, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		panic(err)
	}
	result := make([][]interface{}, n)
	for i := range result {
		result[i] = []interface{}{ts, i}
	}
	return result
}

func windowsOf(elements []model.QueriesRequestElement) (windows [][2]string) {
	for _, element := range elements {
		windows = append(windows, [2]string{*element.Time.Start, *element.Time.End})
	}
	return windows
}

// Windows grow while they return few rows, and a point on the boundary of two windows falls into exactly one of
// them: /queries leaves out both ends of a window.
func TestCSVDownloadWindowsOfSparseData(t *testing.T) {
	sent, _ := runDownload(t, rawElement("2025-09-15T20:00:00Z", "2025-09-16T08:00:00Z"), func(int) [][]interface{} { return nil })

	want := [][2]string{
		{"2025-09-15T20:00:00Z", "2025-09-15T21:00:00Z"},
		{"2025-09-15T20:59:59.999999Z", "2025-09-15T23:00:00Z"},
		{"2025-09-15T22:59:59.999999Z", "2025-09-16T03:00:00Z"},
		{"2025-09-16T02:59:59.999999Z", "2025-09-16T08:00:00Z"},
	}
	got := windowsOf(sent)
	if len(got) != len(want) {
		t.Fatalf("sent windows %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("window %d is %v, want %v", i, got[i], want[i])
		}
	}

	for _, point := range []struct {
		at   string
		want int
	}{
		{"2025-09-15T20:00:00Z", 0},
		{"2025-09-15T21:00:00Z", 1},
		{"2025-09-15T23:00:00Z", 1},
		{"2025-09-15T22:59:59.999999Z", 1},
		{"2025-09-16T03:00:00Z", 1},
		{"2025-09-16T07:59:59.999999Z", 1},
		{"2025-09-16T08:00:00Z", 0},
	} {
		at, _ := time.Parse(time.RFC3339Nano, point.at)
		in := 0
		for _, window := range got {
			start, _ := time.Parse(time.RFC3339Nano, window[0])
			end, _ := time.Parse(time.RFC3339Nano, window[1])
			if at.After(start) && at.Before(end) {
				in++
			}
		}
		if in != point.want {
			t.Errorf("point %s is in %d windows, want %d", point.at, in, point.want)
		}
	}
}

// A window that returned more than downloadWindowRows rows is followed by one half as long.
func TestCSVDownloadWindowsOfDenseData(t *testing.T) {
	sent, csv := runDownload(t, rawElement("2025-09-15T20:00:00Z", "2025-09-15T22:00:00Z"), func(i int) [][]interface{} {
		if i == 0 {
			return rows(downloadWindowRows+1, "2025-09-15T20:30:00Z")
		}
		return nil
	})
	got := windowsOf(sent)
	if len(got) < 2 || got[1] != [2]string{"2025-09-15T20:59:59.999999Z", "2025-09-15T21:30:00Z"} {
		t.Errorf("sent windows %v, want a second window of half an hour", got)
	}
	if lines := strings.Count(csv, "\n"); lines != downloadWindowRows+2 {
		t.Errorf("csv has %d lines, want header and %d rows", lines, downloadWindowRows+1)
	}
}

// The limit counts the rows of the whole download.
func TestCSVDownloadLimit(t *testing.T) {
	element := rawElement("2025-09-15T20:00:00Z", "2025-09-16T20:00:00Z")
	limit := 3
	element.Limit = &limit
	sent, csv := runDownload(t, element, func(i int) [][]interface{} {
		if i == 0 {
			return rows(2, "2025-09-15T20:30:00Z")
		}
		return rows(1, "2025-09-15T21:30:00Z")
	})
	if len(sent) != 2 || *sent[0].Limit != 3 || *sent[1].Limit != 1 {
		t.Fatalf("expected two requests with limits 3 and 1, got %d", len(sent))
	}
	if lines := strings.Count(csv, "\n"); lines != 4 {
		t.Errorf("csv has %d lines, want header and 3 rows", lines)
	}
}

// A grouped download is one request over the whole window: a bucket is not split, and the limit is the one of
// /queries.
func TestCSVDownloadGrouped(t *testing.T) {
	last := "last"
	groupTime := "1d"
	limit := 5
	last7d := "7d"
	element := model.QueriesRequestElement{
		Columns:   []model.QueriesRequestElementColumn{{Name: "reading.value", GroupType: &last}},
		GroupTime: &groupTime,
		Limit:     &limit,
		Time:      &model.QueriesRequestElementTime{Last: &last7d},
	}
	sent, csv := runDownload(t, element, func(int) [][]interface{} { return rows(2, "2025-09-15T00:00:00Z") })
	if len(sent) != 1 {
		t.Fatalf("expected one request, got %d", len(sent))
	}
	if sent[0].Time.Last == nil || *sent[0].Time.Last != "7d" || sent[0].Time.Start != nil || *sent[0].Limit != 5 {
		t.Errorf("expected the time and limit of the request unchanged, got %+v", sent[0])
	}
	if csv != "time,reading.value\n2025-09-15T00:00:00Z,0\n2025-09-15T00:00:00Z,1\n" {
		t.Errorf("unexpected csv %q", csv)
	}
}

func TestResolveRelativeDownloadTime(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	last, ahead, start := "2d", "1:30:00", "2026-11-01T00:00:00Z"
	end := "2026-11-02T00:00:00Z"
	for _, tc := range []struct {
		time       model.QueriesRequestElementTime
		start, end string
	}{
		{model.QueriesRequestElementTime{Last: &last}, "2026-11-17T12:00:00Z", "2026-11-19T12:00:00Z"},
		{model.QueriesRequestElementTime{Ahead: &ahead}, "2026-11-19T12:00:00Z", "2026-11-19T13:30:00Z"},
		{model.QueriesRequestElementTime{Start: &start, End: &end}, start, end},
	} {
		if err := resolveRelativeDownloadTime(&tc.time, now); err != nil {
			t.Fatal(err)
		}
		if *tc.time.Start != tc.start || *tc.time.End != tc.end || tc.time.Last != nil || tc.time.Ahead != nil {
			t.Errorf("got %+v, want start %s and end %s without last and ahead", tc.time, tc.start, tc.end)
		}
		if !tc.time.Valid() {
			t.Errorf("%+v is not a valid time of /queries", tc.time)
		}
	}
}

func TestNextDownloadWindow(t *testing.T) {
	for _, tc := range []struct {
		size time.Duration
		rows int
		want time.Duration
	}{
		{time.Hour, 0, 2 * time.Hour},
		{16 * time.Hour, 0, downloadWindowMax},
		{downloadWindowMax, 0, downloadWindowMax},
		{time.Hour, downloadWindowRows / 2, time.Hour},
		{time.Hour, downloadWindowRows, time.Hour},
		{time.Hour, downloadWindowRows + 1, 30 * time.Minute},
		{downloadWindowMin, 10 * downloadWindowRows, downloadWindowMin},
	} {
		if got := nextDownloadWindow(tc.size, tc.rows); got != tc.want {
			t.Errorf("after %s with %d rows: got %s, want %s", tc.size, tc.rows, got, tc.want)
		}
	}
}

// writerGoroutines counts the CSV writer goroutines of handleCSVDownload that are still alive.
func writerGoroutines() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	return strings.Count(string(buf), "api.writeCSVChunks(")
}

// An aborted download must not leave its CSV writer goroutine behind.
func TestCSVDownloadAbortStopsWriter(t *testing.T) {
	w := &windowWrapper{failFrom: 2, respond: func(int) [][]interface{} { return rows(1, "2025-09-15T20:30:00Z") }}

	log.InitForTest()
	before := writerGoroutines()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
	func() {
		defer func() {
			if r := recover(); r != http.ErrAbortHandler {
				t.Fatalf("expected panic(http.ErrAbortHandler), got %v", r)
			}
		}()
		handleCSVDownload(c, rawElement("2025-09-15T20:00:00Z", "2025-09-16T20:00:00Z"), downloadPipeline(w).downloadSource("user", []string{"user"}, "", nil), c.Writer)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for writerGoroutines() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := writerGoroutines(); after > before {
		t.Errorf("%d csv writer goroutine(s) still running after the abort", after-before)
	}
}

// failingResponseWriter accepts headers and flushes but fails every write of the body.
type failingResponseWriter struct{ header http.Header }

func (w *failingResponseWriter) Header() http.Header       { return w.header }
func (w *failingResponseWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }
func (w *failingResponseWriter) WriteHeader(int)           {}
func (w *failingResponseWriter) Flush()                    {}

// When the writer fails, the download aborts without waiting for a writer result it has already taken.
func TestCSVDownloadWriterErrorAborts(t *testing.T) {
	w := &windowWrapper{respond: func(int) [][]interface{} { return rows(1, "2025-09-15T20:30:00Z") }}

	log.InitForTest()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
	done := make(chan interface{})
	go func() {
		defer func() { done <- recover() }()
		handleCSVDownload(c, rawElement("2025-09-15T20:00:00Z", "2025-09-25T20:00:00Z"), downloadPipeline(w).downloadSource("user", []string{"user"}, "", nil), &failingResponseWriter{header: http.Header{}})
	}()
	select {
	case r := <-done:
		if r != http.ErrAbortHandler {
			t.Fatalf("expected panic(http.ErrAbortHandler), got %v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("download did not return after the writer failed")
	}
}

// What is refused before the first byte is an error with a status, not a cut-off response.
func TestCSVDownloadRefusesBeforeHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
	element := rawElement("2025-09-15T20:00:00Z", "not a time")
	handleCSVDownload(c, element, func(context.Context, model.QueriesRequestElement) ([][]interface{}, error) {
		t.Error("source must not be called")
		return nil, nil
	}, c.Writer)
	if len(c.Errors) != 1 || model.GetStatusCode(c.Errors[0].Err) != http.StatusBadRequest {
		t.Errorf("expected one bad request, got %v", c.Errors)
	}
	if recorder.Body.Len() != 0 || recorder.Header().Get("Content-Type") != "" {
		t.Errorf("expected nothing written, got %q", recorder.Body.String())
	}
}

func TestPrepareQueriesRequestElement(t *testing.T) {
	t.Parallel()
	token := "Bearer " + helperToken(t, jwt.MapClaims{"sub": "user-1"})
	valid := `{"deviceId":"d","serviceId":"` + badRequestServiceId + `","columns":[{"name":"a"}],"time":{"start":"2025-09-15T20:00:00Z","end":"2025-09-15T21:00:00Z"}}`
	request := func(query string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/prepare-download?"+query, nil)
		r.Header.Set("Authorization", token)
		return r
	}
	for _, tc := range []struct {
		name    string
		query   string
		wantErr string
	}{
		{name: "not JSON", query: "{", wantErr: "unexpected end of JSON input"},
		{name: "wrong type", query: `{"columns":5}`, wantErr: "cannot unmarshal"},
		{name: "no columns", query: `{"deviceId":"d"}`, wantErr: "invalid query"},
		{name: "device group", query: `{"deviceGroupId":"g","columns":[{"name":"a"}],"time":{"last":"1h"}}`, wantErr: "DeviceGroupId"},
		{name: "no time", query: `{"deviceId":"d","serviceId":"` + badRequestServiceId + `","columns":[{"name":"a"}]}`, wantErr: "need time element"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := prepareQueriesRequestElement(request(""), &fakeVerifier{ok: true, owners: []string{"o"}}, tc.query)
			if err == nil || model.GetStatusCode(err) != http.StatusBadRequest || countClasses(err) != 1 || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected one bad request containing %q, got %v", tc.wantErr, err)
			}
		})
	}
	t.Run("valid", func(t *testing.T) {
		verifier := &fakeVerifier{ok: true, owners: []string{"owner-1"}}
		prepared, userId, owners, err := prepareQueriesRequestElement(request("time_format=x&precision=2"), verifier, valid)
		if err != nil {
			t.Fatal(err)
		}
		if userId != "user-1" || len(owners) != 1 || owners[0] != "owner-1" || prepared.Token != token ||
			prepared.TimeFormat != "x" || prepared.Precision == nil || *prepared.Precision != 2 {
			t.Errorf("unexpected result %+v %q %v", prepared, userId, owners)
		}
	})
}

// The CSV of a download is the one the rows had when they were read over HTTP from /queries: formatted as a table
// and rounded there, encoded as JSON, decoded, and encoded again by writeCsv. This builds that path from the same
// steps and compares it to the direct read.
func TestDownloadSourceCSVIdenticalToHTTPPath(t *testing.T) {
	t.Parallel()
	ts := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			panic(err)
		}
		return v
	}
	cet := time.FixedZone("CET", 3600)
	fixture := func() [][]interface{} {
		return [][]interface{}{
			{ts("2025-09-15T20:00:00Z"), int64(5), 1.5},
			{ts("2025-09-15T20:00:01.123456789Z"), int64(-7), 0.1 + 0.2},
			{ts("2025-09-15T20:00:02Z").In(cet), nil, 2.675},
			{ts("2025-09-15T20:00:03Z"), int64(1<<53 - 1), 1e21},
			{ts("2025-09-15T20:00:04Z"), int64(-(1<<53 - 1)), 1e-7},
			{ts("2025-09-15T20:00:05Z"), 7, float64(5)},
			{ts("2025-09-15T20:00:06Z"), int32(-3), -0.0000012345},
			{ts("2025-09-15T20:00:07Z"), `a "quoted" <b> & c,d`, true},
			{ts("2025-09-15T20:00:08Z"), map[string]interface{}{"z": 1, "a": []interface{}{int64(2), 3.5, nil}}, "line\nbreak"},
			{ts("2025-09-15T20:00:09Z"), []interface{}{int64(1 << 60), "x"}, float32(0.1)},
			{ts("2025-09-15T20:00:10Z"), nil, nil},
			{ts("2025-09-15T20:00:11Z"), uint8(200), 123456789.123456789},
		}
	}
	element := model.QueriesRequestElement{
		Columns: []model.QueriesRequestElementColumn{{Name: "a"}, {Name: "b"}},
		Time:    &model.QueriesRequestElementTime{Start: strPtr("2025-09-15T20:00:00Z"), End: strPtr("2025-09-15T21:00:00Z")},
	}
	two, zero := 2, 0
	for _, tc := range []struct {
		name       string
		timeFormat string
		precision  *int
	}{
		{name: "plain"},
		{name: "time format", timeFormat: "2006-01-02 15:04:05.000-07:00"},
		{name: "precision", precision: &two},
		{name: "precision 0 and time format", timeFormat: "Mon Jan 2 15:04:05 2006", precision: &zero},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := downloadPipeline(&windowWrapper{respond: func(int) [][]interface{} { return fixture() }})
			ctx := context.Background()

			// the path of the HTTP request: what /queries does for these params, then the CSV of the download
			elements := []model.QueriesRequestElement{element}
			raw, err := p.fetch(ctx, elements, "user", []string{"user"}, nil, []models.Device{})
			if err != nil {
				t.Fatal(err)
			}
			table, err := p.formatTable(ctx, p.newLookups(), elements, raw, 0, model.Asc, tc.timeFormat)
			if err != nil {
				t.Fatal(err)
			}
			roundResponse(table, tc.precision)
			body, err := json.Marshal(table) // writeJSON of /queries
			if err != nil {
				t.Fatal(err)
			}
			var decoded [][]interface{}
			if err = json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			var want strings.Builder
			if err = writeCsv(decoded, csv.NewWriter(&want)); err != nil {
				t.Fatal(err)
			}

			direct, err := p.downloadSource("user", []string{"user"}, tc.timeFormat, tc.precision)(ctx, element)
			if err != nil {
				t.Fatal(err)
			}
			var got strings.Builder
			if err = writeCsv(direct, csv.NewWriter(&got)); err != nil {
				t.Fatal(err)
			}
			if got.String() != want.String() {
				t.Errorf("csv differs\ndirect:\n%s\nover http:\n%s", got.String(), want.String())
			}
			if strings.Count(want.String(), "\n") < 10 {
				t.Errorf("fixture produced too few rows: %q", want.String())
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// Integers are written exactly, also beyond 2^53, where the JSON round trip of the HTTP path cut digits off.
func TestDownloadSourceLargeIntegersExact(t *testing.T) {
	t.Parallel()
	row := []interface{}{time.Date(2025, 9, 15, 20, 0, 0, 0, time.UTC), int64(1<<53 + 1), int64(1 << 60), int64(-(1<<62 + 1))}
	p := downloadPipeline(&windowWrapper{respond: func(int) [][]interface{} {
		return [][]interface{}{append([]interface{}(nil), row...)}
	}})
	element := rawElement("2025-09-15T20:00:00Z", "2025-09-15T21:00:00Z")
	element.Columns = append(element.Columns, model.QueriesRequestElementColumn{Name: "b"}, model.QueriesRequestElementColumn{Name: "c"})
	rows, err := p.downloadSource("user", []string{"user"}, "", nil)(context.Background(), element)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err = writeCsv(rows, csv.NewWriter(&out)); err != nil {
		t.Fatal(err)
	}
	if want := "2025-09-15T20:00:00Z,9007199254740993,1152921504606846976,-4611686018427387905\n"; out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}
