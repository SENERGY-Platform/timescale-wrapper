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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	dbTestDeviceId  = "urn:infai:ses:device:1b0b8d8c-2a2f-4b3e-9c4a-5d6e7f809192"
	dbTestServiceId = "urn:infai:ses:service:2c1c9e9d-3b3f-4c4e-8d5a-6e7f80919293"
	dbTestTimezone  = "Europe/Berlin"
	dbTestEnergy    = "sensor.ENERGY.Total"
	dbTestWater     = "sensor.WATER.Total"
)

// TestDifferenceQueriesAgainstDatabase executes generated difference-* queries against a real
// TimescaleDB and compares them against readings taken at the bucket edges by separate queries.
// It covers the shape of the result: one row per requested bucket, every bucket carrying the
// difference to its predecessor. Whether lag() survives an unfavourable join plan is a separate
// question, see TestDifferenceQueriesIgnoreJoinStrategy. Skipped when TEST_POSTGRES_DSN is unset.
func TestDifferenceQueriesAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	loc, err := time.LoadLocation(dbTestTimezone)
	if err != nil {
		t.Fatal(err)
	}
	// four days of a monotonically rising meter, sampled every minute. The increments vary, so a
	// difference series built from mispaired rows cannot accidentally match.
	sampleStart := time.Date(2021, 6, 1, 0, 0, 0, 0, loc)
	sampleEnd := time.Date(2021, 6, 5, 0, 0, 0, 0, loc)
	samples := generateReadings(sampleStart, sampleEnd)

	table := dbTestSetup(ctx, t, pool, samples)

	// the readings the caller asks for: 48 hourly buckets
	start := time.Date(2021, 6, 2, 0, 0, 0, 0, loc)
	end := time.Date(2021, 6, 4, 0, 0, 0, 0, loc)
	startS := start.Format(time.RFC3339)
	endS := end.Format(time.RFC3339)
	groupTime := "1h"
	dl := "difference-last"
	last := "last"

	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, pool: pool}

	// reference values, read back bucket by bucket with plain aggregates instead of window functions
	buckets := []time.Time{}
	for b := start.Add(-time.Hour); b.Before(end); b = b.Add(time.Hour) {
		buckets = append(buckets, b)
	}
	energyEdges := dbTestBucketEdges(ctx, t, pool, table, dbTestEnergy, buckets)
	waterEdges := dbTestBucketEdges(ctx, t, pool, table, dbTestWater, buckets)

	cases := []struct {
		name    string
		columns []model.QueriesRequestElementColumn
		// expected[i] holds the expected value of column i per bucket, indexed like buckets[1:]
		expected [][]float64
	}{
		{
			name:     "single difference column",
			columns:  []model.QueriesRequestElementColumn{{Name: dbTestEnergy, GroupType: &dl}},
			expected: [][]float64{diffs(energyEdges)},
		},
		{
			name: "two difference columns",
			columns: []model.QueriesRequestElementColumn{
				{Name: dbTestEnergy, GroupType: &dl},
				{Name: dbTestWater, GroupType: &dl},
			},
			expected: [][]float64{diffs(energyEdges), diffs(waterEdges)},
		},
		{
			name: "difference column joined with a plain aggregate",
			columns: []model.QueriesRequestElementColumn{
				{Name: dbTestEnergy, GroupType: &last},
				{Name: dbTestWater, GroupType: &dl},
			},
			expected: [][]float64{energyEdges[1:], diffs(waterEdges)},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			elementTime := model.QueriesRequestElementTime{Start: &startS, End: &endS}
			deviceId, serviceId := dbTestDeviceId, dbTestServiceId
			elements := []model.QueriesRequestElement{{
				DeviceId:  &deviceId,
				ServiceId: &serviceId,
				Time:      &elementTime,
				GroupTime: &groupTime,
				Columns:   c.columns,
			}}
			queries, err := wrapper.GenerateQueries(ctx, elements, "", []string{""}, dbTestTimezone, []models.Device{})
			if err != nil {
				t.Fatal(err)
			}
			results, err := wrapper.ExecuteQueries(ctx, queries)
			if err != nil {
				t.Fatal(queries[0], err)
			}
			rows := results[0]
			if len(rows) != len(buckets)-1 {
				t.Fatalf("expected %v rows, got %v\n%v", len(buckets)-1, len(rows), queries[0])
			}
			for i, row := range rows {
				// the query orders descending, the reference ascending
				bucket := buckets[len(buckets)-1-i]
				ts, ok := row[0].(time.Time)
				if !ok {
					t.Fatalf("row %v has no timestamp: %v\n%v", i, row, queries[0])
				}
				if !ts.Equal(bucket) {
					t.Errorf("row %v: expected bucket %v, got %v", i, bucket, ts)
				}
				for columnIndex := range c.columns {
					expected := c.expected[columnIndex][len(buckets)-2-i]
					actual, ok := row[columnIndex+1].(float64)
					if !ok {
						t.Fatalf("row %v column %v is not a number: %v\n%v", i, columnIndex, row, queries[0])
					}
					if math.Abs(actual-expected) > 1e-6 {
						t.Errorf("bucket %v column %v: expected %v, got %v\n%v", bucket, columnIndex, expected, actual, queries[0])
					}
				}
			}
			// what a caller actually reads a difference series for: the consumption over the whole
			// window has to match the two readings at its ends
			for columnIndex, column := range c.columns {
				if *column.GroupType != dl {
					continue
				}
				sum := 0.0
				for _, row := range rows {
					sum += row[columnIndex+1].(float64)
				}
				edges := energyEdges
				if column.Name == dbTestWater {
					edges = waterEdges
				}
				expected := edges[len(edges)-1] - edges[0]
				if math.Abs(sum-expected) > 1e-6 {
					t.Errorf("column %v: differences sum to %v, readings at the window edges differ by %v\n%v",
						columnIndex, sum, expected, queries[0])
				}
			}
		})
	}
}

type dbTestReading struct {
	time   time.Time
	energy float64
	water  float64
}

func generateReadings(start time.Time, end time.Time) (readings []dbTestReading) {
	energy, water := 1000.0, 500.0
	for i := 0; start.Add(time.Duration(i) * time.Minute).Before(end); i++ {
		energy += float64(1 + i%5)
		water += float64(2 + i%3)
		readings = append(readings, dbTestReading{
			// half a bucket off the boundary: the widened window starts one unit before the first
			// requested bucket and filters with a strict >, so a reading sitting exactly on that
			// boundary would be excluded and the predecessor bucket would come back empty
			time:   start.Add(time.Duration(i)*time.Minute + 30*time.Second),
			energy: energy,
			water:  water,
		})
	}
	return readings
}

func diffs(values []float64) (result []float64) {
	for i := 1; i < len(values); i++ {
		result = append(result, values[i]-values[i-1])
	}
	return result
}

func dbTestSetup(ctx context.Context, t *testing.T, pool *pgxpool.Pool, readings []dbTestReading) (table string) {
	t.Helper()
	shortDeviceId, err := models.ShortenId(dbTestDeviceId)
	if err != nil {
		t.Fatal(err)
	}
	shortServiceId, err := models.ShortenId(dbTestServiceId)
	if err != nil {
		t.Fatal(err)
	}
	table = "device:" + shortDeviceId + "_service:" + shortServiceId
	quoted := quoteIdentifier(table)

	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(query, err)
		}
	}
	exec("CREATE EXTENSION IF NOT EXISTS timescaledb")
	exec("DROP TABLE IF EXISTS " + quoted)
	exec("CREATE TABLE " + quoted + " (\"time\" timestamptz NOT NULL, " +
		quoteIdentifier(dbTestEnergy) + " double precision, " + quoteIdentifier(dbTestWater) + " double precision)")
	// create_hypertable takes a regclass, so the quoted identifier has to survive as such
	exec("SELECT create_hypertable(" + quoteSQLString(quoted) + ", 'time')")
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+quoted); err != nil {
			t.Log("could not drop test table:", err)
		}
	})
	rows := make([][]interface{}, len(readings))
	for i, reading := range readings {
		rows[i] = []interface{}{reading.time, reading.energy, reading.water}
	}
	_, err = pool.CopyFrom(ctx, pgx.Identifier{table}, []string{"time", dbTestEnergy, dbTestWater}, pgx.CopyFromRows(rows))
	if err != nil {
		t.Fatal(err)
	}
	return table
}

// dbTestBucketEdges reads the last value of each bucket with a separate plain query, independent of
// the window functions under test.
func dbTestBucketEdges(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string, column string, buckets []time.Time) (values []float64) {
	t.Helper()
	query := "SELECT last(" + quoteIdentifier(column) + ", \"time\") FROM " + quoteIdentifier(table) +
		" WHERE \"time\" >= $1 AND \"time\" < $2"
	for _, bucket := range buckets {
		var value float64
		if err := pool.QueryRow(ctx, query, bucket, bucket.Add(time.Hour)).Scan(&value); err != nil {
			t.Fatal(bucket, err)
		}
		values = append(values, value)
	}
	return values
}

// TestDifferenceQueriesIgnoreJoinStrategy runs a difference series over enough buckets that the
// FULL OUTER JOIN of the column subqueries spills to disk, and forces the planner onto a hash join
// to get there. A multi batch hash join emits its rows batch by batch instead of in bucket order,
// which is legal: the row order of a join is unspecified. A window function that does not order its
// own frame then pairs unrelated buckets and returns plausible wrong numbers without any error.
// Skipped when TEST_POSTGRES_DSN is unset.
func TestDifferenceQueriesIgnoreJoinStrategy(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := t.Context()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// the smallest work_mem postgres accepts, so a few thousand buckets are enough to spill. Without
	// disabling merge joins the planner keeps picking the one plan that happens to preserve the
	// bucket order, and the defect stays invisible.
	poolConfig.ConnConfig.RuntimeParams["work_mem"] = "64kB"
	poolConfig.ConnConfig.RuntimeParams["enable_mergejoin"] = "off"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	loc, err := time.LoadLocation(dbTestTimezone)
	if err != nil {
		t.Fatal(err)
	}
	sampleStart := time.Date(2021, 6, 1, 0, 0, 0, 0, loc)
	samples := generateReadings(sampleStart, sampleStart.AddDate(0, 0, 4))
	dbTestSetup(ctx, t, pool, samples)

	// one bucket per sample, so the reference is the difference between consecutive readings
	start := time.Date(2021, 6, 2, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 3)
	startS := start.Format(time.RFC3339)
	endS := end.Format(time.RFC3339)
	groupTime := "1m"
	dl := "difference-last"
	deviceId, serviceId := dbTestDeviceId, dbTestServiceId
	elementTime := model.QueriesRequestElementTime{Start: &startS, End: &endS}
	elements := []model.QueriesRequestElement{{
		DeviceId:  &deviceId,
		ServiceId: &serviceId,
		Time:      &elementTime,
		GroupTime: &groupTime,
		Columns: []model.QueriesRequestElementColumn{
			{Name: dbTestEnergy, GroupType: &dl},
			{Name: dbTestWater, GroupType: &dl},
		},
	}}

	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, pool: pool}
	queries, err := wrapper.GenerateQueries(ctx, elements, "", []string{""}, dbTestTimezone, []models.Device{})
	if err != nil {
		t.Fatal(err)
	}
	dbTestRequireSpillingHashJoin(ctx, t, pool, queries[0])

	results, err := wrapper.ExecuteQueries(ctx, queries)
	if err != nil {
		t.Fatal(queries[0], err)
	}
	rows := results[0]
	expectedRows := int(end.Sub(start).Minutes())
	if len(rows) != expectedRows {
		t.Fatalf("expected %v rows, got %v", expectedRows, len(rows))
	}
	byTime := map[time.Time]dbTestReading{}
	for i, reading := range samples {
		if i > 0 {
			byTime[reading.time.Truncate(time.Minute)] = dbTestReading{
				time:   reading.time,
				energy: reading.energy - samples[i-1].energy,
				water:  reading.water - samples[i-1].water,
			}
		}
	}
	wrong := 0
	for _, row := range rows {
		ts, ok := row[0].(time.Time)
		if !ok {
			t.Fatalf("row without a timestamp: %v", row)
		}
		expected, ok := byTime[ts.In(loc)]
		if !ok {
			t.Fatalf("unexpected bucket %v", ts)
		}
		energy, energyOk := row[1].(float64)
		water, waterOk := row[2].(float64)
		if !energyOk || !waterOk {
			t.Fatalf("bucket %v is not numeric: %v", ts, row)
		}
		if math.Abs(energy-expected.energy) > 1e-6 || math.Abs(water-expected.water) > 1e-6 {
			if wrong < 3 {
				t.Errorf("bucket %v: expected %v/%v, got %v/%v", ts, expected.energy, expected.water, energy, water)
			}
			wrong++
		}
	}
	if wrong > 0 {
		t.Errorf("%v of %v buckets carry a difference between unrelated buckets\n%v", wrong, len(rows), queries[0])
	}
}

// dbTestRequireSpillingHashJoin fails the test if the query does not reach the plan it is meant to
// cover. Postgres is free to choose another one, and a green test on a merge join would prove
// nothing about the row order.
func dbTestRequireSpillingHashJoin(ctx context.Context, t *testing.T, pool *pgxpool.Pool, query string) {
	t.Helper()
	rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, FORMAT TEXT) "+query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		line := ""
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += line + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "Hash Full Join") {
		t.Fatal("expected a hash full join, got:\n" + plan)
	}
	if !regexp.MustCompile(`Batches: [2-9]|Batches: \d\d`).MatchString(plan) {
		t.Fatal("expected the hash join to spill into several batches, got:\n" + plan)
	}
}
