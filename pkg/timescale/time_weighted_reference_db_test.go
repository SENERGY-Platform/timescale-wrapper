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
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// twDbReference computes the time-weighted means of the energy column the slow way: it samples
// every second in the middle, assigns the sample to its bucket with time_bucket and averages per
// bucket. A sample takes the value of the newest point at or before it, or with linear the value
// interpolated towards the next point; points sharing a time count with their mean. Buckets are
// those time_bucket assigns some second of [time_bucket(start), end) to, each with all of its
// seconds. Seconds before the first point and from now on are left out.
func twDbReference(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string, groupTime string, timezone string, start time.Time, end time.Time, linear bool) map[time.Time]float64 {
	t.Helper()
	width := quoteSQLString(groupTime) + "::interval"
	tz := quoteSQLString(timezone)
	quoted := quoteIdentifier(table)
	energy := quoteIdentifier(dbTestEnergy)
	value := "prev.v"
	if linear {
		value = "CASE WHEN next.t IS NULL THEN prev.v ELSE prev.v + (next.v - prev.v) * extract(epoch FROM x - prev.t) / extract(epoch FROM next.t - prev.t) END"
	}
	query := "WITH sec AS (SELECT s + interval '500 milliseconds' AS x, time_bucket(" + width + ", s + interval '500 milliseconds', " + tz + ") AS label" +
		" FROM generate_series(time_bucket(" + width + ", $1::timestamptz, " + tz + ") - " + width + " - interval '2 hours', $2::timestamptz + 2 * " + width + " + interval '2 hours', interval '1 second') s)," +
		" labels AS (SELECT DISTINCT label FROM sec WHERE x >= time_bucket(" + width + ", $1::timestamptz, " + tz + ") AND x < $2)" +
		" SELECT sec.label, avg(" + value + ")::double precision FROM sec" +
		" CROSS JOIN LATERAL (SELECT \"time\" AS t, avg(" + energy + ") AS v FROM " + quoted + " WHERE \"time\" = (SELECT max(\"time\") FROM " + quoted + " WHERE \"time\" <= sec.x) GROUP BY 1) prev" +
		" LEFT JOIN LATERAL (SELECT \"time\" AS t, avg(" + energy + ") AS v FROM " + quoted + " WHERE \"time\" = (SELECT min(\"time\") FROM " + quoted + " WHERE \"time\" > sec.x) GROUP BY 1) next ON TRUE" +
		" WHERE sec.label IN (SELECT label FROM labels) AND sec.x < now() GROUP BY 1"
	rows, err := pool.Query(ctx, query, start, end)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	means := map[time.Time]float64{}
	for rows.Next() {
		var label time.Time
		var mean float64
		if err := rows.Scan(&label, &mean); err != nil {
			t.Fatal(err)
		}
		means[label] = mean
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return means
}

// TestTimeWeightedMatchesReference compares generated queries with twDbReference around changes of
// the offset to UTC, in zones that move by an hour, by half an hour, across midnight, or have a
// half hour offset, with group times that are no whole number of minutes, and with windows that
// end just after the change. Skipped when TEST_POSTGRES_DSN is unset.
func TestTimeWeightedMatchesReference(t *testing.T) {
	ctx, pool, _, wrapper := twDbTest(t)
	changes := []struct {
		zone string
		at   time.Time
	}{
		{"Europe/Berlin", time.Date(2021, 3, 28, 1, 0, 0, 0, time.UTC)},
		{"Europe/Berlin", time.Date(2021, 10, 31, 1, 0, 0, 0, time.UTC)},
		{"Europe/Dublin", time.Date(2021, 10, 31, 1, 0, 0, 0, time.UTC)},
		{"Australia/Lord_Howe", time.Date(2021, 4, 3, 15, 0, 0, 0, time.UTC)},
		{"Australia/Lord_Howe", time.Date(2021, 10, 2, 15, 30, 0, 0, time.UTC)},
		{"America/St_Johns", time.Date(2021, 3, 14, 5, 30, 0, 0, time.UTC)},
		{"America/St_Johns", time.Date(2021, 11, 7, 4, 30, 0, 0, time.UTC)},
		{"America/Sao_Paulo", time.Date(2018, 11, 4, 3, 0, 0, 0, time.UTC)},
	}
	for _, change := range changes {
		// uneven gaps between 7 and 47 minutes from a day before the change to a day after it
		readings := []dbTestReading{}
		for i, at := 0, change.at.Add(-26*time.Hour+13*time.Second); at.Before(change.at.Add(26 * time.Hour)); i++ {
			readings = append(readings, dbTestReading{time: at, energy: float64((i * 37) % 101)})
			at = at.Add(time.Duration(7+(i*13)%41)*time.Minute + time.Duration((i*29)%60)*time.Second)
		}
		table := dbTestSetup(ctx, t, pool, readings)
		for _, groupTime := range []string{"7s", "90s", "45m", "2h", "1d"} {
			windows := map[string][2]time.Time{
				"around the change":       {change.at.Add(-3 * time.Hour), change.at.Add(3 * time.Hour)},
				"ending just after it":    {change.at.Add(-3 * time.Hour), change.at.Add(30 * time.Minute)},
				"starting just before it": {change.at.Add(-10 * time.Minute), change.at.Add(2 * time.Hour)},
			}
			for name, window := range windows {
				t.Run(change.zone+" "+change.at.Format("2006-01-02")+" "+groupTime+" "+name, func(t *testing.T) {
					deviceId, serviceId := dbTestDeviceId, dbTestServiceId
					elementTime := dbTestWindow(window[0], window[1])
					gt := groupTime
					locf, linear := "time-weighted-mean-locf", "time-weighted-mean-linear"
					queries, err := wrapper.GenerateQueries(ctx, []model.QueriesRequestElement{{
						DeviceId:  &deviceId,
						ServiceId: &serviceId,
						Time:      &elementTime,
						GroupTime: &gt,
						Columns: []model.QueriesRequestElementColumn{
							{Name: dbTestEnergy, GroupType: &locf},
							{Name: dbTestEnergy, GroupType: &linear},
						},
					}}, "", []string{""}, change.zone, []models.Device{})
					if err != nil {
						t.Fatal(err)
					}
					results, err := wrapper.ExecuteQueries(ctx, queries)
					if err != nil {
						t.Fatal(queries[0], err)
					}
					references := []map[time.Time]float64{
						twDbReference(ctx, t, pool, table, groupTime, change.zone, window[0], window[1], false),
						twDbReference(ctx, t, pool, table, groupTime, change.zone, window[0], window[1], true),
					}
					if len(results[0]) != len(references[0]) {
						t.Errorf("expected %v buckets, got %v", len(references[0]), len(results[0]))
					}
					for _, row := range results[0] {
						bucket := row[0].(time.Time)
						for column, reference := range references {
							want, ok := reference[bucket]
							if !ok {
								t.Errorf("bucket %v is not in the reference", bucket)
								break
							}
							if got, ok := row[column+1].(float64); !ok || math.Abs(got-want) > 1e-6 {
								t.Errorf("bucket %v column %v: reference %v, got %v", bucket, column, want, row[column+1])
							}
						}
					}
				})
			}
		}
	}
}

// TestTimeWeightedDuplicateTimes checks points sharing a time, as a device that sends a message
// twice leaves them: they count once, with their mean value. Skipped when TEST_POSTGRES_DSN is
// unset.
func TestTimeWeightedDuplicateTimes(t *testing.T) {
	ctx, pool, _, wrapper := twDbTest(t)
	at := func(hour, minute int) time.Time { return time.Date(2021, 5, 3, hour, minute, 0, 0, time.UTC) }
	readings := []dbTestReading{
		{time: at(0, 30), energy: 10}, {time: at(0, 30), energy: 10},
		{time: at(3, 0), energy: 20},
		{time: at(4, 10), energy: 4}, {time: at(4, 10), energy: 8}, {time: at(4, 10), energy: 12},
		{time: at(6, 0), energy: 0},
		{time: at(7, 0), energy: 30}, {time: at(7, 0), energy: 30},
	}
	table := dbTestSetup(ctx, t, pool, readings)
	start, end := at(0, 0), at(12, 0)
	deviceId, serviceId := dbTestDeviceId, dbTestServiceId
	elementTime := dbTestWindow(start, end)
	groupTime := "1h"
	locf, linear := "time-weighted-mean-locf", "time-weighted-mean-linear"
	rows := twDbRun(ctx, t, wrapper, model.QueriesRequestElement{
		DeviceId:  &deviceId,
		ServiceId: &serviceId,
		Time:      &elementTime,
		GroupTime: &groupTime,
		Columns: []model.QueriesRequestElementColumn{
			{Name: dbTestEnergy, GroupType: &locf},
			{Name: dbTestEnergy, GroupType: &linear},
		},
	})
	references := []map[time.Time]float64{
		twDbReference(ctx, t, pool, table, groupTime, dbTestTimezone, start, end, false),
		twDbReference(ctx, t, pool, table, groupTime, dbTestTimezone, start, end, true),
	}
	// 00:00 UTC holds the first point, the buckets up to 12:00 UTC follow
	if len(rows) != 12 || len(references[0]) != 12 {
		t.Fatalf("expected 12 buckets, got %v, reference %v", len(rows), len(references[0]))
	}
	for _, row := range rows {
		bucket := row[0].(time.Time)
		for column, reference := range references {
			want, ok := reference[bucket]
			if got, isNumber := row[column+1].(float64); !ok || !isNumber || math.Abs(got-want) > 1e-6 {
				t.Errorf("bucket %v column %v: reference %v, got %v", bucket, column, want, row[column+1])
			}
		}
	}
}
