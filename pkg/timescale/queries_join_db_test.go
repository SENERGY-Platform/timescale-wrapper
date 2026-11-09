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
	"os"
	"testing"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestBucketsOfLaterColumnsAgainstDatabase covers an aggregate whose columns read different
// buckets: with a limit, a "last" column reads the latest buckets and a "mean" column the earliest.
// The rows of buckets only the later column had came back with time NULL, and a third column,
// joined on sub0.time, did not join to them. Skipped when TEST_POSTGRES_DSN is unset.
func TestBucketsOfLaterColumnsAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	log.InitForTest()
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
	// four hourly buckets of readings
	start := time.Date(2021, 6, 1, 0, 0, 0, 0, loc)
	dbTestSetup(ctx, t, pool, generateReadings(start, start.Add(4*time.Hour)))

	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, readPool: pool}
	deviceId, serviceId := dbTestDeviceId, dbTestServiceId
	startS, endS := start.Add(-time.Minute).Format(time.RFC3339), start.Add(4*time.Hour).Format(time.RFC3339)
	groupTime, limit := "1h", 2
	mean, last := "mean", "last"
	desc := model.Desc
	zero := 0
	element := model.QueriesRequestElement{
		DeviceId:         &deviceId,
		ServiceId:        &serviceId,
		Time:             &model.QueriesRequestElementTime{Start: &startS, End: &endS},
		GroupTime:        &groupTime,
		Limit:            &limit,
		OrderColumnIndex: &zero,
		OrderDirection:   &desc,
		Columns: []model.QueriesRequestElementColumn{
			{Name: dbTestEnergy, GroupType: &mean},
			{Name: dbTestWater, GroupType: &last},
			{Name: dbTestEnergy, GroupType: &last},
		},
	}
	queries, err := wrapper.GenerateQueries(ctx, []model.QueriesRequestElement{element}, "", []string{""}, "", []models.Device{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := executeQuery(ctx, pool, queries[0])
	if err != nil {
		t.Fatal(queries[0], err)
	}
	// the two latest buckets, both read by the "last" columns only
	if len(rows) != 2 {
		t.Fatalf("expected two rows, got %v", rows)
	}
	for i, expected := range []time.Time{start.Add(3 * time.Hour), start.Add(2 * time.Hour)} {
		ts, ok := rows[i][0].(time.Time)
		if !ok || !ts.Equal(expected) {
			t.Errorf("row %d: expected time %v, got %v", i, expected, rows[i])
			continue
		}
		if rows[i][1] != nil || rows[i][2] == nil || rows[i][3] == nil {
			t.Errorf("row %d: expected no mean and both last values in one row, got %v", i, rows[i])
		}
	}
}
