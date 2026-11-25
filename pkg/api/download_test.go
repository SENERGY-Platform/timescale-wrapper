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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/gin-gonic/gin"
)

// runDownload runs handleCSVDownload against a stand-in for /queries that answers the i-th request with the rows
// respond returns, and gives back the request elements sent and the CSV written.
func runDownload(t *testing.T, element model.QueriesRequestElement, respond func(i int) [][]interface{}) (sent []model.QueriesRequestElement, csv string) {
	t.Helper()
	var mux sync.Mutex
	queries := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var elements []model.QueriesRequestElement
		if err := json.NewDecoder(r.Body).Decode(&elements); err != nil || len(elements) != 1 {
			t.Errorf("unexpected request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mux.Lock()
		i := len(sent)
		sent = append(sent, elements[0])
		mux.Unlock()
		_ = json.NewEncoder(w).Encode(respond(i))
	}))
	defer queries.Close()
	queriesURL, err := url.Parse(queries.URL)
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
	handleCSVDownload(c, element, "", nil, "Bearer token", c.Writer, &configuration.ConfigStruct{ApiPort: queriesURL.Port()})
	return sent, recorder.Body.String()
}

func rawElement(start, end string) model.QueriesRequestElement {
	return model.QueriesRequestElement{
		Columns: []model.QueriesRequestElementColumn{{Name: "reading.value"}},
		Time:    &model.QueriesRequestElementTime{Start: &start, End: &end},
	}
}

func rows(n int, at string) [][]interface{} {
	result := make([][]interface{}, n)
	for i := range result {
		result[i] = []interface{}{at, i}
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
	var mux sync.Mutex
	calls := 0
	queries := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.Lock()
		calls++
		i := calls
		mux.Unlock()
		if i > 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(rows(1, "2025-09-15T20:30:00Z"))
	}))
	defer queries.Close()
	queriesURL, err := url.Parse(queries.URL)
	if err != nil {
		t.Fatal(err)
	}

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
		handleCSVDownload(c, rawElement("2025-09-15T20:00:00Z", "2025-09-16T20:00:00Z"), "", nil, "Bearer token", c.Writer, &configuration.ConfigStruct{ApiPort: queriesURL.Port()})
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
	queries := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(rows(1, "2025-09-15T20:30:00Z"))
	}))
	defer queries.Close()
	queriesURL, err := url.Parse(queries.URL)
	if err != nil {
		t.Fatal(err)
	}

	log.InitForTest()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/download", nil)
	done := make(chan interface{})
	go func() {
		defer func() { done <- recover() }()
		handleCSVDownload(c, rawElement("2025-09-15T20:00:00Z", "2025-09-25T20:00:00Z"), "", nil, "Bearer token", &failingResponseWriter{header: http.Header{}}, &configuration.ConfigStruct{ApiPort: queriesURL.Port()})
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
