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
	"os"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/sqlliteral"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tablenames"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	caLookupTestDeviceId  = "urn:infai:ses:device:7f3e2c1a-9b8d-4e5f-a6b7-c8d9e0f1a2b3"
	caLookupTestServiceId = "urn:infai:ses:service:8a4f3d2b-1c9e-4d6f-b7a8-d9e0f1a2b3c4"
	caLookupTestNoViewDev = "urn:infai:ses:device:9b5a4e3c-2d0f-4e7a-b8c9-e0f1a2b3c4d5"
	caLookupTestView      = "ts_wrapper_ca_lookup_cache_test_view"
)

// caLookupTestTable creates the hypertable tableName looks up for deviceId and serviceId, with a
// value column. It is dropped on cleanup, and so is the view registered by the caller, which has
// to be registered after this call so that it is dropped first.
func caLookupTestTable(ctx context.Context, t *testing.T, pool *pgxpool.Pool, deviceId string, serviceId string) string {
	t.Helper()
	table, err := tablenames.DeviceTableName(deviceId, serviceId)
	if err != nil {
		t.Fatal(err)
	}
	quoted := sqlliteral.QuoteIdentifier(table)
	exec := func(query string) {
		t.Helper()
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(query, err)
		}
	}
	exec("CREATE EXTENSION IF NOT EXISTS timescaledb")
	exec("DROP TABLE IF EXISTS " + quoted)
	exec("CREATE TABLE " + quoted + " (\"time\" timestamptz NOT NULL, value double precision)")
	exec("SELECT create_hypertable(" + sqlliteral.QuoteString(quoted) + ", 'time')")
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP TABLE IF EXISTS "+quoted); err != nil {
			t.Log("could not drop test table:", err)
		}
	})
	return table
}

// TestTableNameCachesTheContinuousAggregateLookup checks that tableName finds a continuous aggregate
// view, caches the lookup, and returns the base table for a hypertable without a view. Skipped when
// TEST_POSTGRES_DSN is unset.
func TestTableNameCachesTheContinuousAggregateLookup(t *testing.T) {
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

	// a view left behind by an earlier run would keep its table from being dropped
	if _, err := pool.Exec(ctx, "DROP MATERIALIZED VIEW IF EXISTS "+sqlliteral.QuoteIdentifier(caLookupTestView)); err != nil {
		t.Fatal(err)
	}

	table := caLookupTestTable(ctx, t, pool, caLookupTestDeviceId, caLookupTestServiceId)
	view := sqlliteral.QuoteIdentifier(caLookupTestView)
	createView := "CREATE MATERIALIZED VIEW " + view + " WITH (timescaledb.continuous) AS " +
		"SELECT time_bucket('1 hour'::interval, \"time\", '" + dbTestTimezone + "') AS \"time\", last(value, \"time\") AS value " +
		"FROM " + sqlliteral.QuoteIdentifier(table) + " GROUP BY 1 WITH NO DATA"
	if _, err := pool.Exec(ctx, createView); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DROP MATERIALIZED VIEW IF EXISTS "+view); err != nil {
			t.Log("could not drop test view:", err)
		}
	})

	deviceId, serviceId := caLookupTestDeviceId, caLookupTestServiceId
	groupTime, groupType := "1h", "last"
	element := model.QueriesRequestElement{
		DeviceId:  &deviceId,
		ServiceId: &serviceId,
		GroupTime: &groupTime,
		Columns:   []model.QueriesRequestElementColumn{{Name: "value", GroupType: &groupType}},
	}
	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, readPool: pool, caViews: cache.NewLocal()}

	for i := range 2 {
		got, err := wrapper.tableName(ctx, element, "", dbTestTimezone)
		if err != nil {
			t.Fatal(err)
		}
		if got != caLookupTestView {
			t.Fatalf("call %d: tableName = %q, want the continuous aggregate %q", i+1, got, caLookupTestView)
		}
	}

	// The view is gone, but the cached name is still returned: the second lookup did not reach the database.
	if _, err := pool.Exec(ctx, "DROP MATERIALIZED VIEW "+view); err != nil {
		t.Fatal(err)
	}
	got, err := wrapper.tableName(ctx, element, "", dbTestTimezone)
	if err != nil {
		t.Fatal(err)
	}
	if got != caLookupTestView {
		t.Fatalf("after the view was dropped: tableName = %q, want the cached %q", got, caLookupTestView)
	}
	// Without a cache the lookup runs again and finds no view.
	uncached := &Wrapper{config: wrapper.config, readPool: pool}
	if got, err := uncached.tableName(ctx, element, "", dbTestTimezone); err != nil || got != table {
		t.Fatalf("uncached after the view was dropped: tableName = %q, %v, want the base table %q", got, err, table)
	}

	// A hypertable without a view returns its base table, and the miss is cached too.
	noViewDevice := caLookupTestNoViewDev
	noViewTable := caLookupTestTable(ctx, t, pool, noViewDevice, caLookupTestServiceId)
	noViewElement := element
	noViewElement.DeviceId = &noViewDevice
	for i := range 2 {
		got, err := wrapper.tableName(ctx, noViewElement, "", dbTestTimezone)
		if err != nil {
			t.Fatal(err)
		}
		if got != noViewTable {
			t.Fatalf("call %d without a view: tableName = %q, want the base table %q", i+1, got, noViewTable)
		}
	}
}
