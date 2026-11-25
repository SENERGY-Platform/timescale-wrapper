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
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTimeWeightedQueriesAgainstDatabase executes generated time-weighted-mean-* queries over a
// device that sends less often than once per bucket (SNRGY-4319) and compares every bucket against
// a mean sampled once per second from the same points. Skipped when TEST_POSTGRES_DSN is unset.
func TestTimeWeightedQueriesAgainstDatabase(t *testing.T) {
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
	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, readPool: pool}
	locf, linear, last, last3h := "time-weighted-mean-locf", "time-weighted-mean-linear", "last", "3h"

	at := func(day, hour, minute int) time.Time { return time.Date(2021, 6, day, hour, minute, 0, 0, loc) }
	now := time.Now().Truncate(time.Second)
	if now.Equal(now.Truncate(time.Hour)) {
		// the current bucket of the relative window would start at now and have no length
		time.Sleep(time.Second)
		now = time.Now().Truncate(time.Second)
	}

	cases := []struct {
		name      string
		readings  []dbTestReading
		time      model.QueriesRequestElementTime
		groupTime string
		buckets   []time.Time // the expected buckets plus the end of the last one
	}{
		{
			// the first point lies before the window, buckets 04:00 and 06:00 to 08:00 have none
			// of their own, the point after the window is needed for the linear mean of the last
			// buckets
			name: "sparse points, fixed window",
			readings: []dbTestReading{
				{time: at(1, 22, 30), energy: 10}, {time: at(2, 1, 30), energy: 20},
				{time: at(2, 2, 15), energy: 40}, {time: at(2, 5, 0), energy: 0}, {time: at(2, 9, 40), energy: 7},
			},
			time:      dbTestWindow(at(2, 0, 0), at(2, 8, 0)),
			groupTime: "1h",
			buckets:   dbTestSteps(at(2, 0, 0), 9, func(t time.Time) time.Time { return t.Add(time.Hour) }),
		},
		{
			// 2021-10-31 has 25 hours in Europe/Berlin
			name: "daily buckets across the end of daylight saving time",
			readings: []dbTestReading{
				{time: time.Date(2021, 10, 29, 12, 0, 0, 0, loc), energy: 1},
				{time: time.Date(2021, 10, 31, 12, 0, 0, 0, loc), energy: 3},
				{time: time.Date(2021, 11, 3, 12, 0, 0, 0, loc), energy: 5},
			},
			time:      dbTestWindow(time.Date(2021, 10, 30, 0, 0, 0, 0, loc), time.Date(2021, 11, 2, 0, 0, 0, 0, loc)),
			groupTime: "1d",
			buckets: dbTestSteps(time.Date(2021, 10, 30, 0, 0, 0, 0, loc), 4,
				func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }),
		},
		{
			// 2021-10-31 02:00 to 03:00 happens twice in Europe/Berlin, as two buckets
			name: "hourly buckets across the end of daylight saving time",
			readings: []dbTestReading{
				{time: time.Date(2021, 10, 30, 21, 0, 0, 0, loc), energy: 1},
				{time: time.Date(2021, 10, 31, 0, 50, 0, 0, time.UTC), energy: 4},
				{time: time.Date(2021, 10, 31, 1, 20, 0, 0, time.UTC), energy: 9},
				{time: time.Date(2021, 10, 31, 7, 0, 0, 0, loc), energy: 2},
			},
			time:      dbTestWindow(time.Date(2021, 10, 30, 22, 0, 0, 0, loc), time.Date(2021, 10, 31, 6, 0, 0, 0, loc)),
			groupTime: "1h",
			buckets: dbTestSteps(time.Date(2021, 10, 30, 22, 0, 0, 0, loc), 10,
				func(t time.Time) time.Time { return t.Add(time.Hour) }),
		},
		{
			// the newest point is held until now, the current bucket ends there
			name: "window relative to now",
			readings: []dbTestReading{
				{time: now.Add(-5 * time.Hour), energy: 2}, {time: now.Add(-80 * time.Minute), energy: 6},
			},
			time:      model.QueriesRequestElementTime{Last: &last3h},
			groupTime: "1h",
			buckets: append(dbTestSteps(now.Add(-3*time.Hour).Truncate(time.Hour), 4,
				func(t time.Time) time.Time { return t.Add(time.Hour) }), now),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dbTestSetup(ctx, t, pool, c.readings)
			deviceId, serviceId := dbTestDeviceId, dbTestServiceId
			elementTime := c.time
			zero := 0
			asc := model.Asc
			elements := []model.QueriesRequestElement{{
				DeviceId:  &deviceId,
				ServiceId: &serviceId,
				Time:      &elementTime,
				GroupTime: &c.groupTime,
				Columns: []model.QueriesRequestElementColumn{
					{Name: dbTestEnergy, GroupType: &locf},
					{Name: dbTestEnergy, GroupType: &linear},
					{Name: dbTestEnergy, GroupType: &last},
				},
				OrderColumnIndex: &zero,
				OrderDirection:   &asc,
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
			if len(rows) != len(c.buckets)-1 {
				t.Fatalf("expected %v rows, got %v: %v\n%v", len(c.buckets)-1, len(rows), rows, queries[0])
			}
			for i, row := range rows {
				start, end := c.buckets[i], c.buckets[i+1]
				ts, ok := row[0].(time.Time)
				if !ok || !ts.Equal(start) {
					t.Fatalf("row %v: expected bucket %v, got %v\n%v", i, start, row[0], queries[0])
				}
				for column, interpolate := range []bool{false, true} {
					expected := dbTestSampledMean(c.readings, interpolate, start, end, now)
					actual, ok := row[column+1].(float64)
					if !ok {
						t.Fatalf("bucket %v column %v is not a number: %v\n%v", start, column, row, queries[0])
					}
					if math.Abs(actual-expected) > 1e-6 {
						t.Errorf("bucket %v column %v: expected %v, got %v", start, column, expected, actual)
					}
				}
			}
		})
	}
}

func dbTestWindow(start time.Time, end time.Time) model.QueriesRequestElementTime {
	startS, endS := start.Format(time.RFC3339), end.Format(time.RFC3339)
	return model.QueriesRequestElementTime{Start: &startS, End: &endS}
}

func dbTestSteps(start time.Time, n int, next func(time.Time) time.Time) (steps []time.Time) {
	for t := start; len(steps) < n; t = next(t) {
		steps = append(steps, t)
	}
	return steps
}

// dbTestSampledMean samples the energy readings once per second, in the middle of each second,
// from start to end, leaving out seconds before the first reading and from now on. Between two
// readings the value is held, or with interpolate interpolated; after the last one it is held.
func dbTestSampledMean(readings []dbTestReading, interpolate bool, start time.Time, end time.Time, now time.Time) float64 {
	sum, n := 0.0, 0
	for t := start.Add(500 * time.Millisecond); t.Before(end) && t.Before(now); t = t.Add(time.Second) {
		i := -1
		for i+1 < len(readings) && !readings[i+1].time.After(t) {
			i++
		}
		if i < 0 {
			continue
		}
		value := readings[i].energy
		if interpolate && i+1 < len(readings) {
			next := readings[i+1]
			share := t.Sub(readings[i].time).Seconds() / next.time.Sub(readings[i].time).Seconds()
			value += (next.energy - value) * share
		}
		sum += value
		n++
	}
	return sum / float64(n)
}

// TestTimeWeightedBucketsMatchTimeBucket joins a time-weighted column with a last column, whose
// buckets come from time_bucket, over both changes of daylight saving time. A point every minute
// puts one into every bucket, also into the pieces of a few minutes next to a change of the clock. A bucket only one side
// has shows up as a row with a NULL on the other side. Skipped when TEST_POSTGRES_DSN is unset.
func TestTimeWeightedBucketsMatchTimeBucket(t *testing.T) {
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
	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, readPool: pool}
	locf, last := "time-weighted-mean-locf", "last"

	readings := []dbTestReading{}
	for t := time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC); t.Before(time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC)); t = t.Add(time.Minute) {
		readings = append(readings, dbTestReading{time: t.Add(17 * time.Second), energy: float64(len(readings) % 7)})
	}
	dbTestSetup(ctx, t, pool, readings)

	windows := map[string]model.QueriesRequestElementTime{
		"spring": dbTestWindow(time.Date(2021, 3, 27, 12, 0, 0, 0, loc), time.Date(2021, 3, 29, 12, 0, 0, 0, loc)),
		"autumn": dbTestWindow(time.Date(2021, 10, 30, 12, 0, 0, 0, loc), time.Date(2021, 11, 1, 12, 0, 0, 0, loc)),
		"year":   dbTestWindow(time.Date(2021, 3, 1, 0, 0, 0, 0, loc), time.Date(2022, 1, 1, 0, 0, 0, 0, loc)),
	}
	for _, groupTime := range []string{"7m", "10m", "25m", "45m", "50m", "55m", "1h", "90m", "2h", "5h", "1:30:00", "1d", "1w", "1mon"} {
		for name, window := range windows {
			t.Run(groupTime+" "+name, func(t *testing.T) {
				deviceId, serviceId := dbTestDeviceId, dbTestServiceId
				elementTime := window
				zero, asc := 0, model.Asc
				elements := []model.QueriesRequestElement{{
					OrderColumnIndex: &zero,
					OrderDirection:   &asc,
					DeviceId:         &deviceId,
					ServiceId:        &serviceId,
					Time:             &elementTime,
					GroupTime:        &groupTime,
					Columns: []model.QueriesRequestElementColumn{
						{Name: dbTestEnergy, GroupType: &locf},
						{Name: dbTestEnergy, GroupType: &last},
					},
				}}
				queries, err := wrapper.GenerateQueries(ctx, elements, "", []string{""}, dbTestTimezone, []models.Device{})
				if err != nil {
					if errors.Is(err, model.ErrBadRequest) {
						t.Skip(err)
					}
					t.Fatal(err)
				}
				results, err := wrapper.ExecuteQueries(ctx, queries)
				if err != nil {
					t.Fatal(queries[0], err)
				}
				rows := results[0]
				for i, row := range rows {
					if row[1] == nil {
						t.Errorf("bucket %v has no time-weighted mean", row[0])
					}
					// the first and the last bucket of a last column only see the points inside
					// the window, which may not reach into them
					if row[2] == nil && i != 0 && i != len(rows)-1 {
						t.Errorf("bucket %v has no last value", row[0])
					}
				}
			})
		}
	}
}
