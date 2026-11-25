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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDataAvailabilityOnlyOfRequestedDevice: the short id of device A contains '_', which is a
// wildcard in LIKE. Device B has the same short id with that character replaced. Only A's
// tables and continuous aggregates may be returned.
// Skipped when TEST_POSTGRES_DSN is unset.
func TestDataAvailabilityOnlyOfRequestedDevice(t *testing.T) {
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

	// a device id whose short id has a '_' in the first 21 characters
	var deviceA, shortA string
	for {
		deviceA = "urn:infai:ses:device:" + uuid.NewString()
		shortA, err = models.ShortenId(deviceA)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(shortA[:21], "_") {
			break
		}
	}
	shortB := strings.Replace(shortA, "_", "A", 1)
	// 22 characters like a short service id
	newService := func() string {
		s, err := models.ShortenId("urn:infai:ses:service:" + uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	serviceA, serviceB, serviceCA := newService(), newService(), newService()

	tableA := "device:" + shortA + "_service:" + serviceA
	tableCAbase := "device:" + shortA + "_service:" + serviceCA // hypertable of a continuous aggregate of A
	tableB := "device:" + shortB + "_service:" + serviceB
	viewA := tableCAbase + "_1d"
	viewB := tableB + "_1d"
	// a table of A with a longer service part must not match either
	tooLong := tableA + "x"

	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	t.Cleanup(func() {
		for _, v := range []string{viewA, viewB} {
			_, _ = pool.Exec(t.Context(), fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s CASCADE", quoteIdentifier(v)))
		}
		for _, tb := range []string{tableA, tableCAbase, tableB, tooLong} {
			_, _ = pool.Exec(t.Context(), fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", quoteIdentifier(tb)))
		}
	})
	for _, tb := range []string{tableA, tableCAbase, tableB, tooLong} {
		exec(fmt.Sprintf(`CREATE TABLE %s ("time" timestamptz NOT NULL, v double precision)`, quoteIdentifier(tb)))
		exec(fmt.Sprintf(`SELECT create_hypertable('%s', 'time')`, strings.ReplaceAll(quoteIdentifier(tb), "'", "''")))
		exec(fmt.Sprintf(`INSERT INTO %s VALUES ('2021-06-01T10:00:00Z', 1), ('2021-06-03T10:00:00Z', 2)`, quoteIdentifier(tb)))
	}
	for _, p := range [][2]string{{viewA, tableCAbase}, {viewB, tableB}} {
		exec(fmt.Sprintf(`CREATE MATERIALIZED VIEW %s WITH (timescaledb.continuous) AS SELECT time_bucket('1 day'::interval, "time") AS "time", last(v, "time") AS v FROM %s GROUP BY 1 WITH NO DATA`, quoteIdentifier(p[0]), quoteIdentifier(p[1])))
	}

	wrapper := &Wrapper{readPool: pool}
	got, err := wrapper.GetDataAvailability(ctx, deviceA)
	if err != nil {
		t.Fatal(err)
	}
	// A: table tableA, table tableCAbase and the view of tableCAbase. B and tooLong are not A's.
	idA, idCA, idB := mustLong(t, serviceA), mustLong(t, serviceCA), mustLong(t, serviceB)
	counts := map[string]int{}
	for _, e := range got {
		counts[e.ServiceId]++
		if e.ServiceId == servicePrefix+idB {
			t.Errorf("service of the other device returned: %+v", e)
		}
	}
	if counts[servicePrefix+idA] != 1 {
		t.Errorf("want tableA once, got %d (%+v)", counts[servicePrefix+idA], got)
	}
	if counts[servicePrefix+idCA] != 2 {
		t.Errorf("want table and view of the aggregated service, got %d (%+v)", counts[servicePrefix+idCA], got)
	}
	if len(got) != 3 {
		t.Errorf("want 3 elements, got %d: %+v", len(got), got)
	}
}

func mustLong(t *testing.T, short string) string {
	t.Helper()
	long, err := models.LongId(short)
	if err != nil {
		t.Fatal(err)
	}
	return long
}
