/*
 *    Copyright 2026 InfAI (CC SES)
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

package timescale

import (
	"context"
	"math"
	"net/http"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	listDbDeviceId  = "urn:infai:ses:device:3d2d0f0e-4c4a-4d5f-8e6b-7f8091a2b3c4"
	listDbServiceId = "urn:infai:ses:service:4e3e1a1f-5d5b-4e6a-9f7c-8091a2b3c4d5"
	listDbList      = "root.module_1_processdata"
	listDbFixed     = "root.fixed.0.value"
	listDbLevel     = "root.level"
)

type listDbRow struct {
	time  time.Time
	list  interface{} // JSON text, nil for NULL
	fixed int64
	level float64
}

// listDbSetup creates the table of a device whose service has a variable list of structures
// (the shape of the SITEC ECM output), a leaf of a fixed list, which is a column of its own, and a
// plain value.
func listDbSetup(ctx context.Context, t *testing.T, pool *pgxpool.Pool, rows []listDbRow) {
	t.Helper()
	shortDeviceId, err := models.ShortenId(listDbDeviceId)
	if err != nil {
		t.Fatal(err)
	}
	shortServiceId, err := models.ShortenId(listDbServiceId)
	if err != nil {
		t.Fatal(err)
	}
	table := "device:" + shortDeviceId + "_service:" + shortServiceId
	quoted := quoteIdentifier(table)
	exec := func(query string) {
		t.Helper()
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(query, err)
		}
	}
	exec("CREATE EXTENSION IF NOT EXISTS timescaledb")
	exec("DROP TABLE IF EXISTS " + quoted)
	exec("CREATE TABLE " + quoted + " (\"time\" timestamptz NOT NULL, " + quoteIdentifier(listDbList) + " jsonb, " +
		quoteIdentifier(listDbFixed) + " bigint, " + quoteIdentifier(listDbLevel) + " double precision)")
	exec("SELECT create_hypertable(" + quoteSQLString(quoted) + ", 'time')")
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+quoted); err != nil {
			t.Log("could not drop test table:", err)
		}
	})
	copyRows := make([][]interface{}, len(rows))
	for i, row := range rows {
		var list interface{}
		if text, ok := row.list.(string); ok {
			list = []byte(text)
		}
		copyRows[i] = []interface{}{row.time, list, row.fixed, row.level}
	}
	_, err = pool.CopyFrom(ctx, pgx.Identifier{table}, []string{"time", listDbList, listDbFixed, listDbLevel}, pgx.CopyFromRows(copyRows))
	if err != nil {
		t.Fatal(err)
	}
}

// TestListPathsAgainstDatabase reads values from inside a JSONB list column. The elements carry
// result and ratio (result / 2) so that sums are exact and means are floats.
// Skipped when TEST_POSTGRES_DSN is unset.
func TestListPathsAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	log.InitForTest() // tableName logs when no continuous aggregate can be looked up
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	at := func(hour, minute int) time.Time { return time.Date(2021, 6, 1, hour, minute, 0, 0, time.UTC) }
	listDbSetup(ctx, t, pool, []listDbRow{
		// a JSON null element is present and stays in the array, like in the cache
		{at(9, 20), `[{"result":1},{"result":null},{"result":3}]`, 0, -0.7},
		// before the window of most queries, the predecessor bucket of a difference
		{at(9, 30), `[{"station":0,"result":20,"errorcode":0,"main_id":"z","ratio":10}]`, 0, -0.5},
		{at(10, 10), `[{"station":1,"result":5,"errorcode":0,"main_id":"a","ratio":2.5},{"station":2,"result":7,"errorcode":3,"main_id":"b","ratio":3.5}]`, 1, 0.5},
		{at(10, 40), `[{"station":3,"result":11,"errorcode":0,"main_id":"c","ratio":5.5}]`, 2, 1.5},
		{at(11, 20), `[]`, 3, 2.5},
		{at(11, 50), nil, 4, 3.5},
		{at(12, 10), `[{"station":4,"result":2,"errorcode":1,"main_id":"d","ratio":1},{"station":5,"result":4,"errorcode":0,"main_id":"e","ratio":2},{"station":6,"result":6,"errorcode":9,"main_id":"f","ratio":3}]`, 5, 4.5},
	})

	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: "UTC"}, readPool: pool}
	startS, endS := "2021-06-01T10:00:00Z", "2021-06-01T13:00:00Z"
	hour := "1h"
	asc := model.Asc
	zero := 0
	p := func(segments []string, sqlType string) model.JSONPath {
		return model.JSONPath{Column: listDbList, Segments: segments, SQLType: sqlType}
	}
	paths := map[string]model.JSONPath{
		listDbList + ".0.errorcode": p([]string{"0", "errorcode"}, "bigint"),
		listDbList + ".1.errorcode": p([]string{"1", "errorcode"}, "bigint"),
		listDbList + ".0.main_id":   p([]string{"0", "main_id"}, "text"),
		listDbList + ".1.main_id":   p([]string{"1", "main_id"}, "text"),
		listDbList + ".0.ratio":     p([]string{"0", "ratio"}, "double precision"),
		listDbList + ".0":           p([]string{"0"}, ""),
		listDbList + ".*.result":    p([]string{"*", "result"}, "bigint"),
		listDbList + ".*.ratio":     p([]string{"*", "ratio"}, "double precision"),
		listDbList + ".*":           p([]string{"*"}, ""),
	}

	run := func(t *testing.T, element model.QueriesRequestElement) [][]interface{} {
		t.Helper()
		deviceId, serviceId := listDbDeviceId, listDbServiceId
		element.DeviceId = &deviceId
		element.ServiceId = &serviceId
		element.JSONPaths = paths
		element.OrderColumnIndex = &zero
		element.OrderDirection = &asc
		queries, err := wrapper.GenerateQueries(ctx, []model.QueriesRequestElement{element}, "", []string{""}, "UTC", []models.Device{})
		if err != nil {
			t.Fatal(err)
		}
		results, err := wrapper.ExecuteQueries(ctx, queries)
		if err != nil {
			t.Fatal(queries[0], err)
		}
		rows := results[0]
		sort.SliceStable(rows, func(i, j int) bool {
			a, aok := rows[i][0].(time.Time)
			b, bok := rows[j][0].(time.Time)
			return aok && bok && a.Before(b)
		})
		return rows
	}
	window := func() *model.QueriesRequestElementTime {
		return &model.QueriesRequestElementTime{Start: &startS, End: &endS}
	}
	grouped := func(groupType string, name string) model.QueriesRequestElement {
		return model.QueriesRequestElement{
			Time:      window(),
			GroupTime: &hour,
			Columns:   []model.QueriesRequestElementColumn{{Name: name, GroupType: &groupType}},
		}
	}
	plain := func(names ...string) model.QueriesRequestElement {
		element := model.QueriesRequestElement{Time: window()}
		for _, name := range names {
			element.Columns = append(element.Columns, model.QueriesRequestElementColumn{Name: name})
		}
		return element
	}
	// values of the n-th column of every row
	column := func(rows [][]interface{}, n int) []interface{} {
		values := []interface{}{}
		for _, row := range rows {
			values = append(values, row[n+1])
		}
		return values
	}
	// buckets by start hour, of the n-th column
	byBucket := func(t *testing.T, rows [][]interface{}, n int) map[int]interface{} {
		t.Helper()
		values := map[int]interface{}{}
		for _, row := range rows {
			ts, ok := row[0].(time.Time)
			if !ok {
				t.Fatalf("row without a time: %v", row)
			}
			values[ts.UTC().Hour()] = row[n+1]
		}
		return values
	}
	same := func(t *testing.T, name string, actual interface{}, expected interface{}) {
		t.Helper()
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("%s: expected %#v, got %#v", name, expected, actual)
		}
	}
	near := func(t *testing.T, name string, actual interface{}, expected float64) {
		t.Helper()
		value, ok := actual.(float64)
		if !ok || math.Abs(value-expected) > 1e-9 {
			t.Errorf("%s: expected %v, got %#v", name, expected, actual)
		}
	}

	t.Run("index and field are typed", func(t *testing.T) {
		rows := run(t, plain(listDbList+".0.errorcode", listDbList+".0.main_id", listDbList+".0.ratio"))
		same(t, "errorcode", column(rows, 0), []interface{}{int64(0), int64(0), nil, nil, int64(1)})
		same(t, "main_id", column(rows, 1), []interface{}{"a", "c", nil, nil, "d"})
		same(t, "ratio", column(rows, 2), []interface{}{2.5, 5.5, nil, nil, 1.0})
	})

	t.Run("index outside of the list is null", func(t *testing.T) {
		rows := run(t, plain(listDbList+".1.errorcode", listDbList+".1.main_id"))
		same(t, "errorcode", column(rows, 0), []interface{}{int64(3), nil, nil, nil, int64(0)})
		same(t, "main_id", column(rows, 1), []interface{}{"b", nil, nil, nil, "e"})
	})

	t.Run("element that is a structure", func(t *testing.T) {
		rows := run(t, plain(listDbList+".0"))
		first, ok := rows[0][1].(map[string]interface{})
		if !ok || first["main_id"] != "a" {
			t.Errorf("unexpected element %#v", rows[0][1])
		}
		same(t, "empty list", rows[2][1], nil)
	})

	t.Run("leaf of a fixed list is a regular column", func(t *testing.T) {
		rows := run(t, plain(listDbFixed, listDbLevel))
		same(t, "fixed", column(rows, 0), []interface{}{int64(1), int64(2), int64(3), int64(4), int64(5)})
		same(t, "level", column(rows, 1), []interface{}{0.5, 1.5, 2.5, 3.5, 4.5})
	})

	t.Run("math on an indexed value", func(t *testing.T) {
		plus := "+10"
		element := plain(listDbList + ".0.errorcode")
		element.Columns[0].Math = &plus
		rows := run(t, element)
		same(t, "errorcode", column(rows, 0), []interface{}{int64(10), int64(10), nil, nil, int64(11)})
	})

	t.Run("wildcard without an aggregation is an array per message", func(t *testing.T) {
		rows := run(t, plain(listDbList+".*.result"))
		same(t, "results", column(rows, 0), []interface{}{
			[]interface{}{float64(5), float64(7)},
			[]interface{}{float64(11)},
			[]interface{}{},
			nil,
			[]interface{}{float64(2), float64(4), float64(6)},
		})
	})

	t.Run("wildcard keeps JSON null elements", func(t *testing.T) {
		startNine, endTen := "2021-06-01T09:00:00Z", "2021-06-01T09:25:00Z"
		element := plain(listDbList + ".*.result")
		element.Time = &model.QueriesRequestElementTime{Start: &startNine, End: &endTen}
		rows := run(t, element)
		same(t, "results", column(rows, 0), []interface{}{[]interface{}{float64(1), nil, float64(3)}})
	})

	t.Run("wildcard on the elements returns them", func(t *testing.T) {
		rows := run(t, plain(listDbList+".*"))
		list, ok := rows[1][1].([]interface{})
		if !ok || len(list) != 1 {
			t.Fatalf("unexpected value %#v", rows[1][1])
		}
		same(t, "element", list[0].(map[string]interface{})["main_id"], "c")
	})

	t.Run("aggregation over an index", func(t *testing.T) {
		rows := run(t, grouped("mean", listDbList+".0.ratio"))
		got := byBucket(t, rows, 0)
		near(t, "10:00", got[10], 4.0)
		near(t, "12:00", got[12], 1.0)
		same(t, "11:00", got[11], nil)
		rows = run(t, grouped("max", listDbList+".0.errorcode"))
		got = byBucket(t, rows, 0)
		same(t, "max 10:00", got[10], int64(0))
		same(t, "max 12:00", got[12], int64(1))
	})

	t.Run("aggregation over all elements", func(t *testing.T) {
		cases := []struct {
			groupType string
			column    string
			// expected per bucket. 11:00 has messages without elements and keeps its row
			ten, eleven, twelve interface{}
		}{
			{"sum", listDbList + ".*.result", int64(23), nil, int64(12)},
			{"count", listDbList + ".*.result", int64(3), int64(0), int64(3)},
			{"min", listDbList + ".*.result", int64(5), nil, int64(2)},
			{"max", listDbList + ".*.result", int64(11), nil, int64(6)},
			{"first", listDbList + ".*.result", int64(5), nil, int64(2)},
			{"last", listDbList + ".*.result", int64(11), nil, int64(6)},
		}
		for _, c := range cases {
			t.Run(c.groupType, func(t *testing.T) {
				rows := run(t, grouped(c.groupType, c.column))
				got := byBucket(t, rows, 0)
				same(t, "10:00", got[10], c.ten)
				same(t, "11:00", got[11], c.eleven)
				same(t, "12:00", got[12], c.twelve)
				if len(got) != 3 {
					t.Errorf("expected the buckets 10:00 to 12:00, got %v", got)
				}
			})
		}
		t.Run("mean of floats", func(t *testing.T) {
			rows := run(t, grouped("mean", listDbList+".*.ratio"))
			got := byBucket(t, rows, 0)
			near(t, "10:00", got[10], (2.5+3.5+5.5)/3)
			near(t, "12:00", got[12], 2.0)
		})
		t.Run("median", func(t *testing.T) {
			rows := run(t, grouped("median", listDbList+".*.result"))
			got := byBucket(t, rows, 0)
			same(t, "10:00", got[10], int64(7))
			same(t, "11:00", got[11], nil)
			same(t, "12:00", got[12], int64(4))
		})
	})

	t.Run("aggregations of different columns share the buckets", func(t *testing.T) {
		sum, count := "sum", "count"
		element := model.QueriesRequestElement{
			Time:      window(),
			GroupTime: &hour,
			Columns: []model.QueriesRequestElementColumn{
				{Name: listDbList + ".*.result", GroupType: &sum},
				{Name: listDbList + ".0.errorcode", GroupType: &count},
			},
		}
		rows := run(t, element)
		same(t, "sum", byBucket(t, rows, 0)[10], int64(23))
		same(t, "count", byBucket(t, rows, 1)[10], int64(2))
	})

	t.Run("difference over all elements", func(t *testing.T) {
		rows := run(t, grouped("difference-last", listDbList+".*.result"))
		got := byBucket(t, rows, 0)
		// the last result is 20 at 9:00, 11 at 10:00 and 6 at 12:00. 11:00 has no element, so it
		// has no value to be differenced against, and neither has the bucket after it
		same(t, "10:00", got[10], int64(-9))
		same(t, "11:00", got[11], nil)
		same(t, "12:00", got[12], nil)
	})

	t.Run("filter on a list path", func(t *testing.T) {
		nonZero := model.QueriesRequestElementFilter{Column: listDbList + ".0.errorcode", Type: "!=", Value: 0}
		element := plain(listDbLevel)
		element.Filters = &[]model.QueriesRequestElementFilter{nonZero}
		same(t, "errorcode != 0", column(run(t, element), 0), []interface{}{4.5})

		element = plain(listDbLevel)
		element.Filters = &[]model.QueriesRequestElementFilter{{Column: listDbList + ".1.main_id", Type: "!=", Value: nil}}
		same(t, "second element exists", column(run(t, element), 0), []interface{}{0.5, 4.5})

		element = plain(listDbLevel)
		element.Filters = &[]model.QueriesRequestElementFilter{{Column: listDbList + ".0.main_id", Type: "=", Value: "c"}}
		same(t, "main_id = c", column(run(t, element), 0), []interface{}{1.5})

		element = plain(listDbLevel)
		element.Filters = &[]model.QueriesRequestElementFilter{
			{Column: listDbList + ".0.errorcode", Type: "=", Value: 0},
			{Column: listDbList + ".1.main_id", Type: "!=", Value: nil},
		}
		same(t, "both", column(run(t, element), 0), []interface{}{0.5})
	})

	t.Run("filter on a list path in an aggregation", func(t *testing.T) {
		element := grouped("sum", listDbList+".*.result")
		element.Filters = &[]model.QueriesRequestElementFilter{{Column: listDbList + ".0.errorcode", Type: "=", Value: 0}}
		got := byBucket(t, run(t, element), 0)
		same(t, "10:00", got[10], int64(23))
		if v, ok := got[12]; ok {
			t.Errorf("12:00 has errorcode 1 in its first element, got %#v", v)
		}
	})

	t.Run("aggregating the jsonb column itself is a bad request", func(t *testing.T) {
		mean := "mean"
		deviceId, serviceId := listDbDeviceId, listDbServiceId
		element := model.QueriesRequestElement{
			DeviceId:  &deviceId,
			ServiceId: &serviceId,
			Time:      window(),
			GroupTime: &hour,
			Columns:   []model.QueriesRequestElementColumn{{Name: listDbList, GroupType: &mean}},
		}
		queries, err := wrapper.GenerateQueries(ctx, []model.QueriesRequestElement{element}, "", []string{""}, "UTC", []models.Device{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = wrapper.ExecuteQueries(ctx, queries)
		if err == nil {
			t.Fatal("expected an error")
		}
		if code := GetHTTPErrorCode(err); code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %v", code, err)
		}
	})
}
