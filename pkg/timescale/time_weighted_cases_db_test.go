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
	"os"
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/sqlliteral"
	"github.com/jackc/pgx/v5/pgxpool"
)

// twDbTest connects to TEST_POSTGRES_DSN, or skips the test when it is unset.
func twDbTest(t *testing.T) (ctx context.Context, pool *pgxpool.Pool, loc *time.Location, wrapper *Wrapper) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx = t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	loc, err = time.LoadLocation(dbTestTimezone)
	if err != nil {
		t.Fatal(err)
	}
	wrapper = &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, readPool: pool}
	return ctx, pool, loc, wrapper
}

// twDbRun generates and executes the queries of one element and returns its rows.
func twDbRun(ctx context.Context, t *testing.T, wrapper *Wrapper, element model.QueriesRequestElement) [][]interface{} {
	t.Helper()
	queries, err := wrapper.GenerateQueries(ctx, []model.QueriesRequestElement{element}, "", []string{""}, dbTestTimezone, []models.Device{})
	if err != nil {
		t.Fatal(err)
	}
	results, err := wrapper.ExecuteQueries(ctx, queries)
	if err != nil {
		t.Fatal(queries[0], err)
	}
	return results[0]
}

// twDbCheckMeans compares column 1 and on of rows, one row per bucket in ascending order, with
// the sampled means of expected[column-1] over buckets.
func twDbCheckMeans(t *testing.T, rows [][]interface{}, buckets []time.Time, expected [][]dbTestReading, interpolate []bool) {
	t.Helper()
	if len(rows) != len(buckets)-1 {
		t.Fatalf("expected %v rows, got %v: %v", len(buckets)-1, len(rows), rows)
	}
	for i, row := range rows {
		start, end := buckets[i], buckets[i+1]
		if ts, ok := row[0].(time.Time); !ok || !ts.Equal(start) {
			t.Fatalf("row %v: expected bucket %v, got %v", i, start, row[0])
		}
		for column := range expected {
			want := dbTestSampledMean(expected[column], interpolate[column], start, end, time.Now())
			got, ok := row[column+1].(float64)
			if !ok {
				t.Fatalf("bucket %v column %v is not a number: %v", start, column, row)
			}
			if math.Abs(got-want) > 1e-6 {
				t.Errorf("bucket %v column %v: expected %v, got %v", start, column, want, got)
			}
		}
	}
}

// TestTimeWeightedWithFilter checks that a filter removes points from the stretches: a point the
// filter rejects neither starts nor ends a stretch. Skipped when TEST_POSTGRES_DSN is unset.
func TestTimeWeightedWithFilter(t *testing.T) {
	ctx, pool, loc, wrapper := twDbTest(t)
	at := func(hour, minute int) time.Time { return time.Date(2021, 6, 2, hour, minute, 0, 0, loc) }
	readings := []dbTestReading{
		{time: at(0, 30), energy: 10, water: 1},
		{time: at(1, 15), energy: 99, water: 500}, // rejected
		{time: at(2, 45), energy: 30, water: 2},
		{time: at(3, 10), energy: 77, water: 600}, // rejected
		{time: at(5, 20), energy: 5, water: 3},
	}
	dbTestSetup(ctx, t, pool, readings)
	accepted := []dbTestReading{readings[0], readings[2], readings[4]}

	deviceId, serviceId := dbTestDeviceId, dbTestServiceId
	groupTime := "1h"
	window := dbTestWindow(at(1, 0), at(6, 0))
	locf, linear := "time-weighted-mean-locf", "time-weighted-mean-linear"
	zero, asc := 0, model.Asc
	rows := twDbRun(ctx, t, wrapper, model.QueriesRequestElement{
		DeviceId:  &deviceId,
		ServiceId: &serviceId,
		Time:      &window,
		GroupTime: &groupTime,
		Columns: []model.QueriesRequestElementColumn{
			{Name: dbTestEnergy, GroupType: &locf},
			{Name: dbTestEnergy, GroupType: &linear},
		},
		Filters:          &[]model.QueriesRequestElementFilter{{Column: dbTestWater, Type: "<", Value: 100.0}},
		OrderColumnIndex: &zero,
		OrderDirection:   &asc,
	})
	twDbCheckMeans(t, rows, dbTestSteps(at(1, 0), 6, func(t time.Time) time.Time { return t.Add(time.Hour) }),
		[][]dbTestReading{accepted, accepted}, []bool{false, true})
}

// TestTimeWeightedJSONPathAndInteger reads a time-weighted mean from inside a JSONB list column,
// where a message without the element contributes no point, and from a bigint column. Skipped when
// TEST_POSTGRES_DSN is unset.
func TestTimeWeightedJSONPathAndInteger(t *testing.T) {
	ctx, pool, _, wrapper := twDbTest(t)
	at := func(hour, minute int) time.Time { return time.Date(2021, 6, 1, hour, minute, 0, 0, time.UTC) }
	listDbSetup(ctx, t, pool, []listDbRow{
		{at(8, 40), `[{"ratio":2}]`, 1, 0},
		{at(10, 10), `[{"ratio":4.5},{"ratio":100}]`, 4, 0},
		{at(10, 50), `[]`, 9, 0},
		{at(11, 30), nil, 2, 0},
		{at(12, 20), `[{"ratio":1}]`, 6, 0},
	})
	// the same values as readings of energy, for dbTestSampledMean
	ratios := []dbTestReading{{time: at(8, 40), energy: 2}, {time: at(10, 10), energy: 4.5}, {time: at(12, 20), energy: 1}}
	fixed := []dbTestReading{{time: at(8, 40), energy: 1}, {time: at(10, 10), energy: 4}, {time: at(10, 50), energy: 9},
		{time: at(11, 30), energy: 2}, {time: at(12, 20), energy: 6}}

	deviceId, serviceId := listDbDeviceId, listDbServiceId
	groupTime := "1h"
	window := dbTestWindow(at(10, 0), at(14, 0))
	locf, linear := "time-weighted-mean-locf", "time-weighted-mean-linear"
	ratio := listDbList + ".0.ratio"
	zero, asc := 0, model.Asc
	rows := twDbRun(ctx, t, wrapper, model.QueriesRequestElement{
		DeviceId:  &deviceId,
		ServiceId: &serviceId,
		Time:      &window,
		GroupTime: &groupTime,
		Columns: []model.QueriesRequestElementColumn{
			{Name: ratio, GroupType: &locf},
			{Name: ratio, GroupType: &linear},
			{Name: listDbFixed, GroupType: &locf},
			{Name: listDbFixed, GroupType: &linear},
		},
		JSONPaths: map[string]model.JSONPath{
			ratio: {Column: listDbList, Segments: []string{"0", "ratio"}, SQLType: "double precision"},
		},
		OrderColumnIndex: &zero,
		OrderDirection:   &asc,
	})
	twDbCheckMeans(t, rows, dbTestSteps(at(10, 0), 5, func(t time.Time) time.Time { return t.Add(time.Hour) }),
		[][]dbTestReading{ratios, ratios, fixed, fixed}, []bool{false, true, false, true})
}

// TestTimeWeightedWithDifferenceColumn puts a time-weighted column next to a difference column,
// whose window is widened by one bucket and whose subqueries are limited to the widened bucket
// count (see widenTimeWindowForDifference). The time-weighted means have to be those of the same
// column queried alone, for every requested bucket. Skipped when TEST_POSTGRES_DSN is unset.
func TestTimeWeightedWithDifferenceColumn(t *testing.T) {
	ctx, pool, loc, wrapper := twDbTest(t)
	readings := []dbTestReading{}
	for i := 0; i < 80; i++ {
		// every 37 minutes, so some buckets hold no point
		readings = append(readings, dbTestReading{
			time:   time.Date(2021, 6, 1, 0, 0, 0, 0, loc).Add(time.Duration(37*i) * time.Minute),
			energy: float64(100 + 3*i),
			water:  float64(i % 5),
		})
	}
	dbTestSetup(ctx, t, pool, readings)

	deviceId, serviceId := dbTestDeviceId, dbTestServiceId
	groupTime := "1h"
	dl, locf := "difference-last", "time-weighted-mean-locf"
	cases := map[string]model.QueriesRequestElementTime{
		"fixed window": dbTestWindow(time.Date(2021, 6, 1, 6, 0, 0, 0, loc), time.Date(2021, 6, 1, 18, 0, 0, 0, loc)),
	}
	for name, window := range cases {
		t.Run(name, func(t *testing.T) {
			combinedWindow, aloneWindow := window, window
			combined := twDbRun(ctx, t, wrapper, model.QueriesRequestElement{
				DeviceId:  &deviceId,
				ServiceId: &serviceId,
				Time:      &combinedWindow,
				GroupTime: &groupTime,
				Columns: []model.QueriesRequestElementColumn{
					{Name: dbTestEnergy, GroupType: &dl},
					{Name: dbTestWater, GroupType: &locf},
				},
			})
			alone := twDbRun(ctx, t, wrapper, model.QueriesRequestElement{
				DeviceId:  &deviceId,
				ServiceId: &serviceId,
				Time:      &aloneWindow,
				GroupTime: &groupTime,
				Columns:   []model.QueriesRequestElementColumn{{Name: dbTestWater, GroupType: &locf}},
			})
			if len(combined) != 12 || len(alone) != 12 {
				t.Fatalf("expected 12 rows each, got %v combined and %v alone:\n%v\n%v", len(combined), len(alone), combined, alone)
			}
			// the combined query orders descending, see widenTimeWindowForDifference
			means := map[time.Time]interface{}{}
			for _, row := range alone {
				means[row[0].(time.Time)] = row[1]
			}
			for _, row := range combined {
				bucket := row[0].(time.Time)
				want, ok := means[bucket]
				if !ok {
					t.Errorf("bucket %v missing alone", bucket)
					continue
				}
				if got, ok := row[2].(float64); !ok || math.Abs(got-want.(float64)) > 1e-9 {
					t.Errorf("bucket %v: mean %v combined, %v alone", bucket, row[2], want)
				}
			}
		})
	}
}

// TestTimeWeightedMatchesToolkit compares the means with the toolkit's interpolated_average over
// time_weight summaries, which covers whole buckets the same way when every bucket holds a point.
// Skipped when TEST_POSTGRES_DSN is unset or the database lacks timescaledb_toolkit, unless
// TEST_REQUIRE_TOOLKIT is set, as tests/docker-compose.yml does.
func TestTimeWeightedMatchesToolkit(t *testing.T) {
	ctx, pool, loc, wrapper := twDbTest(t)
	if _, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS timescaledb_toolkit"); err != nil {
		if os.Getenv("TEST_REQUIRE_TOOLKIT") != "" {
			t.Fatal("timescaledb_toolkit not available:", err)
		}
		t.Skip("timescaledb_toolkit not available:", err)
	}
	readings := []dbTestReading{}
	start := time.Date(2021, 3, 27, 0, 0, 0, 0, loc)
	for hour := 0; hour < 72; hour++ {
		// one to three points per hour at uneven minutes, across the start of daylight saving time
		for j := 0; j <= hour%3; j++ {
			readings = append(readings, dbTestReading{
				time:   start.Add(time.Duration(hour)*time.Hour + time.Duration(7+19*j+hour%11)*time.Minute + 13*time.Second),
				energy: float64((hour*7+j*13)%17) - 4.5,
			})
		}
	}
	table := dbTestSetup(ctx, t, pool, readings)

	deviceId, serviceId := dbTestDeviceId, dbTestServiceId
	groupTime := "1h"
	window := dbTestWindow(start.Add(2*time.Hour), start.Add(70*time.Hour))
	locf, linear := "time-weighted-mean-locf", "time-weighted-mean-linear"
	ours := twDbRun(ctx, t, wrapper, model.QueriesRequestElement{
		DeviceId:  &deviceId,
		ServiceId: &serviceId,
		Time:      &window,
		GroupTime: &groupTime,
		Columns: []model.QueriesRequestElementColumn{
			{Name: dbTestEnergy, GroupType: &locf},
			{Name: dbTestEnergy, GroupType: &linear},
		},
	})

	reference := map[time.Time][2]float64{}
	query := "SELECT bucket," +
		" interpolated_average(l, bucket, '1h', lag(l) OVER o, lead(l) OVER o)," +
		" interpolated_average(i, bucket, '1h', lag(i) OVER o, lead(i) OVER o)" +
		" FROM (SELECT time_bucket('1h', \"time\", " + sqlliteral.QuoteString(dbTestTimezone) + ") AS bucket," +
		" time_weight('LOCF', \"time\", " + sqlliteral.QuoteIdentifier(dbTestEnergy) + ") AS l," +
		" time_weight('Linear', \"time\", " + sqlliteral.QuoteIdentifier(dbTestEnergy) + ") AS i" +
		" FROM " + sqlliteral.QuoteIdentifier(table) + " GROUP BY 1) x WINDOW o AS (ORDER BY bucket)"
	rows, err := pool.Query(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var bucket time.Time
		var l, i float64
		if err := rows.Scan(&bucket, &l, &i); err != nil {
			t.Fatal(err)
		}
		reference[bucket] = [2]float64{l, i}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// 68 hours of real time, also on the day the clock skips one
	if len(ours) != 68 {
		t.Fatalf("expected 68 rows, got %v", len(ours))
	}
	for _, row := range ours {
		bucket := row[0].(time.Time)
		want, ok := reference[bucket]
		if !ok {
			t.Errorf("bucket %v missing in the toolkit reference", bucket)
			continue
		}
		for column := range want {
			got, ok := row[column+1].(float64)
			if !ok || math.Abs(got-want[column]) > 1e-6 {
				t.Errorf("bucket %v column %v: toolkit %v, got %v", bucket, column, want[column], row[column+1])
			}
		}
	}
}

// TestTimeWeightedEmptyWindow checks windows that do not end after their start: they hold no
// bucket, like they hold no point for a last column, so neither column yields a value. Skipped
// when TEST_POSTGRES_DSN is unset.
func TestTimeWeightedEmptyWindow(t *testing.T) {
	ctx, pool, loc, wrapper := twDbTest(t)
	at := func(hour, minute int) time.Time { return time.Date(2021, 6, 2, hour, minute, 0, 0, loc) }
	dbTestSetup(ctx, t, pool, []dbTestReading{{time: at(0, 30), energy: 10}, {time: at(2, 0), energy: 20}})
	windows := map[string][2]time.Time{
		"start equals end": {at(1, 30), at(1, 30)},
		"start after end":  {at(1, 30), at(1, 0)},
	}
	for name, window := range windows {
		t.Run(name, func(t *testing.T) {
			deviceId, serviceId := dbTestDeviceId, dbTestServiceId
			elementTime := dbTestWindow(window[0], window[1])
			groupTime := "1h"
			locf, last := "time-weighted-mean-locf", "last"
			rows := twDbRun(ctx, t, wrapper, model.QueriesRequestElement{
				DeviceId:  &deviceId,
				ServiceId: &serviceId,
				Time:      &elementTime,
				GroupTime: &groupTime,
				Columns: []model.QueriesRequestElementColumn{
					{Name: dbTestEnergy, GroupType: &locf},
					{Name: dbTestEnergy, GroupType: &last},
				},
			})
			for _, row := range rows {
				if row[1] != nil || row[2] != nil {
					t.Errorf("expected no values, got %v", row)
				}
			}
		})
	}
}
