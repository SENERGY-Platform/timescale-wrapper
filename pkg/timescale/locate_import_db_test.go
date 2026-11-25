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
	"sync"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/locate"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/sqlliteral"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSetupMaterializedRefreshJobConcurrent calls setupMaterializedRefreshJob for the same new view
// from several goroutines, like replicas that all hit undefined_table at once. All must succeed
// and exactly one refresh job must exist. A view whose job is missing gets it on the next call.
// Skipped when TEST_POSTGRES_DSN is unset.
func TestSetupMaterializedRefreshJobConcurrent(t *testing.T) {
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
	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: dbTestTimezone}, writePool: pool}
	// the refresh jobs call the procedure Migrate creates; on a fresh database no other test has run it yet
	if err := wrapper.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	table := "ts_wrapper_test_setup_mv"
	view := locate.ViewName(table)
	jobCount := func() (n int) {
		err := pool.QueryRow(ctx, "SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name = $1 AND config #>> '{}' = $2",
			wrapperMaterializedViewProcedureName, view).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	deleteJobs := "SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name = " +
		sqlliteral.QuoteString(wrapperMaterializedViewProcedureName) + " AND config #>> '{}' = " + sqlliteral.QuoteString(view)
	cleanup := func() {
		for _, statement := range []string{
			deleteJobs,
			"DROP MATERIALIZED VIEW IF EXISTS " + sqlliteral.QuoteIdentifier(view),
			"DROP TABLE IF EXISTS " + sqlliteral.QuoteIdentifier(table),
		} {
			if _, err := pool.Exec(context.Background(), statement); err != nil {
				t.Errorf("cleanup: %v: %v", statement, err)
			}
		}
	}
	cleanup() // left over from an aborted run
	t.Cleanup(cleanup)

	for _, statement := range []string{
		"CREATE TABLE " + sqlliteral.QuoteIdentifier(table) + " (time TIMESTAMPTZ NOT NULL, station TEXT, lat DOUBLE PRECISION, lon DOUBLE PRECISION)",
		"INSERT INTO " + sqlliteral.QuoteIdentifier(table) + " VALUES (now(), 'a', 51.0, 9.0), (now(), 'a', 51.0, 9.0)",
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	const callers = 8
	errs := make([]error, callers)
	wg := sync.WaitGroup{}
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = wrapper.setupMaterializedRefreshJob(ctx, "station", "lat", "lon", table)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %v: %v", i, err)
		}
	}
	if n := jobCount(); n != 1 {
		t.Errorf("expected 1 job for the view, got %v", n)
	}

	// a view left without its job, as by a crash between the two statements of an earlier version
	if _, err := pool.Exec(ctx, deleteJobs); err != nil {
		t.Fatal(err)
	}
	if err := wrapper.setupMaterializedRefreshJob(ctx, "station", "lat", "lon", table); err != nil {
		t.Fatal(err)
	}
	if n := jobCount(); n != 1 {
		t.Errorf("expected the missing job to be added, got %v jobs", n)
	}
}
