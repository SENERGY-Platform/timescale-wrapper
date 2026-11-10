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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	servingLib "github.com/SENERGY-Platform/analytics-serving/lib"
	importRepo "github.com/SENERGY-Platform/import-repository/lib/client"
	importModel "github.com/SENERGY-Platform/import-repository/lib/model"
	"github.com/SENERGY-Platform/service-commons/pkg/jwt"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/locate"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tablenames"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// readTestRole may only SELECT. A hot standby refuses writes with the same error (25006); it
	// also refuses switching the transaction back to read-write, which the wrapper never does.
	readTestRole = "ts_wrapper_test_reader"
	// readTestLaggingRole is a replica that has not replayed the materialized view yet: same
	// restrictions, but its search_path leads to an empty schema, so the view is missing on the
	// read connection while it exists on the write connection.
	readTestLaggingRole = "ts_wrapper_test_lagging_reader"
	readTestLagSchema   = "ts_wrapper_test_lag"
	readTestRolePw      = "reader-pw"
	readTestUserId      = "4f8b8a5e-0d6c-4b1e-9a51-3c2d1e0f9a8b"
	readTestExportId    = "7a1c2b3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	readTestStaleJob    = "ts_wrapper_test_gone"
	readTestLatChar     = "urn:infai:ses:characteristic:63bb46ea-64f1-4a60-aabb-67b9febdb588"
	readTestLonChar     = "urn:infai:ses:characteristic:d8a73a4d-8745-40b9-87e5-50c5d31f745a"
)

type readTestImportRepo struct {
	importRepo.Interface
	importType importModel.ImportType
}

func (repo readTestImportRepo) ReadImportType(string, jwt.Token) (importModel.ImportType, error, int) {
	return repo.importType, nil, http.StatusOK
}

// readTestFixture is an export of an import with two stations, the two read-only roles and an
// analytics-serving that knows the export.
type readTestFixture struct {
	admin      *pgxpool.Pool
	connConfig *pgx.ConnConfig
	table      string
	servingUrl string
}

// newReadTestFixture skips the test when TEST_POSTGRES_DSN is unset.
func newReadTestFixture(t *testing.T) *readTestFixture {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := t.Context()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	connConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	table, err := tablenames.ExportTableName(readTestUserId, readTestExportId)
	if err != nil {
		t.Fatal(err)
	}
	f := &readTestFixture{admin: admin, connConfig: connConfig, table: table}

	f.dropAll(t, ctx) // left over from an aborted run
	t.Cleanup(func() { f.dropAll(t, context.Background()) })
	f.exec(t,
		"CREATE SCHEMA "+readTestLagSchema,
		"CREATE ROLE "+readTestRole+" LOGIN PASSWORD '"+readTestRolePw+"'",
		"ALTER ROLE "+readTestRole+" SET default_transaction_read_only = on",
		"CREATE ROLE "+readTestLaggingRole+" LOGIN PASSWORD '"+readTestRolePw+"'",
		"ALTER ROLE "+readTestLaggingRole+" SET default_transaction_read_only = on",
		"ALTER ROLE "+readTestLaggingRole+" SET search_path = "+readTestLagSchema,
		"GRANT USAGE ON SCHEMA "+readTestLagSchema+" TO "+readTestLaggingRole,
		"GRANT SELECT ON ALL TABLES IN SCHEMA public TO "+readTestRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO "+readTestRole,
		"CREATE TABLE "+quoteIdentifier(table)+" (time TIMESTAMPTZ NOT NULL, station TEXT, lat DOUBLE PRECISION, lon DOUBLE PRECISION)",
		"INSERT INTO "+quoteIdentifier(table)+" VALUES (now(), 'near', 51.0, 9.0), (now(), 'near', 51.0, 9.0), (now(), 'far', 48.0, 11.0)",
	)

	serving := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance/"+readTestExportId {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(servingLib.Instance{
			ServiceName: "urn:infai:ses:import-type:readtest",
			UserId:      readTestUserId,
			Values: []servingLib.Value{
				{Name: "station", Path: "value.station"},
				{Name: "lat", Path: "value.lat"},
				{Name: "lon", Path: "value.lon"},
			},
		})
	}))
	t.Cleanup(serving.Close)
	f.servingUrl = serving.URL
	return f
}

func (f *readTestFixture) exec(t *testing.T, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := f.admin.Exec(t.Context(), statement); err != nil {
			t.Fatalf("%v: %v", statement, err)
		}
	}
}

func (f *readTestFixture) dropAll(t *testing.T, ctx context.Context) {
	for _, statement := range []string{
		// The table name is the config of a job created before SNRGY-5103.
		"SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name = '" + wrapperMaterializedViewProcedureName +
			"' AND config #>> '{}' IN (" + quoteSQLString(readTestStaleJob) + ", " + quoteSQLString(f.table) + ", " +
			quoteSQLString(locate.ViewName(f.table)) + ")",
		"DROP MATERIALIZED VIEW IF EXISTS " + quoteIdentifier(locate.ViewName(f.table)),
		"DROP TABLE IF EXISTS " + quoteIdentifier(f.table),
		"DROP SCHEMA IF EXISTS " + readTestLagSchema,
		"DO $$ BEGIN IF EXISTS (SELECT FROM pg_roles WHERE rolname = '" + readTestRole + "') THEN " +
			"DROP OWNED BY " + readTestRole + "; DROP ROLE " + readTestRole + "; END IF; END $$",
		"DO $$ BEGIN IF EXISTS (SELECT FROM pg_roles WHERE rolname = '" + readTestLaggingRole + "') THEN " +
			"DROP OWNED BY " + readTestLaggingRole + "; DROP ROLE " + readTestLaggingRole + "; END IF; END $$",
	} {
		if _, err := f.admin.Exec(ctx, statement); err != nil {
			t.Errorf("cleanup: %v: %v", statement, err)
		}
	}
}

// wrapper builds a wrapper through NewWrapper that writes as the admin and reads as readRole, and
// checks that it really does: a test whose reads ran on the write connection would pass with
// every statement misrouted.
func (f *readTestFixture) wrapper(t *testing.T, readRole string) *Wrapper {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	wrapper, err := NewWrapper(ctx, wg, &configuration.ConfigStruct{
		PostgresHost:     f.connConfig.Host,
		PostgresPort:     f.connConfig.Port,
		PostgresUser:     f.connConfig.User,
		PostgresPw:       f.connConfig.Password,
		PostgresDb:       f.connConfig.Database,
		PostgresReadHost: f.connConfig.Host,
		PostgresReadUser: readRole,
		PostgresReadPw:   readTestRolePw,
		ServingUrl:       f.servingUrl,
		DefaultTimezone:  dbTestTimezone,
	})
	if err != nil {
		t.Fatal(err)
	}
	wrapper.importRepoClient = readTestImportRepo{importType: importModel.ImportType{Output: importModel.ContentVariable{
		SubContentVariables: []importModel.ContentVariable{{Name: "value", SubContentVariables: []importModel.ContentVariable{
			{Name: "station", UseAsTag: true},
			{Name: "lat", CharacteristicId: readTestLatChar},
			{Name: "lon", CharacteristicId: readTestLonChar},
		}}},
	}}}

	current, err := wrapper.ExecuteQuery(t.Context(), "SELECT current_user::text")
	if err != nil {
		t.Fatal(err)
	}
	if current[0][0] != readRole {
		t.Fatalf("reads run as %v, want %v", current[0][0], readRole)
	}
	if _, err := wrapper.readPool.Exec(t.Context(), "CREATE TABLE public.ts_wrapper_test_forbidden (x INT)"); err == nil {
		_, _ = f.admin.Exec(context.Background(), "DROP TABLE public.ts_wrapper_test_forbidden")
		t.Fatalf("%v may create tables, so it cannot tell a misrouted write", readRole)
	}
	return wrapper
}

// locate runs the locate import of POST /queries/v2 and expects the station next to the
// requested position.
func (f *readTestFixture) locate(t *testing.T, wrapper *Wrapper, call string) {
	t.Helper()
	filters, err := wrapper.CreateFiltersForImport(t.Context(), readTestExportId, readTestUserId, "token", 51.1, 9.1)
	if err != nil {
		t.Fatalf("CreateFiltersForImport %v: %v", call, err)
	}
	if len(filters) != 1 || filters[0].Column != "station" || filters[0].Type != "=" || filters[0].Value != "near" {
		t.Fatalf("CreateFiltersForImport %v: got %+v, want station = near", call, filters)
	}
}

// requireRefreshJobs expects want refresh jobs whose config names the view.
func (f *readTestFixture) requireRefreshJobs(t *testing.T, want int) {
	t.Helper()
	var jobs int
	err := f.admin.QueryRow(t.Context(), "SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name = $1 AND config #>> '{}' = $2",
		wrapperMaterializedViewProcedureName, locate.ViewName(f.table)).Scan(&jobs)
	if err != nil {
		t.Fatal(err)
	}
	if jobs != want {
		t.Errorf("got %v refresh jobs for the view, want %v", jobs, want)
	}
}

// runRefreshJob runs the export's refresh job once in this session, whichever of the table or the
// view its config names, so a job that cannot refresh fails here with the database's error.
func (f *readTestFixture) runRefreshJob(t *testing.T) {
	t.Helper()
	var job int64
	err := f.admin.QueryRow(t.Context(), "SELECT job_id FROM timescaledb_information.jobs WHERE proc_name = $1 AND config #>> '{}' IN ($2, $3)",
		wrapperMaterializedViewProcedureName, f.table, locate.ViewName(f.table)).Scan(&job)
	if err != nil {
		t.Fatalf("refresh job of the export: %v", err)
	}
	if _, err := f.admin.Exec(t.Context(), "CALL run_job($1)", job); err != nil {
		t.Fatalf("refresh job %v: %v", job, err)
	}
}

// requireStationInView expects station among the locations of the view.
func (f *readTestFixture) requireStationInView(t *testing.T, station string) {
	t.Helper()
	var rows int
	err := f.admin.QueryRow(t.Context(), "SELECT count(*) FROM "+quoteIdentifier(locate.ViewName(f.table))+" WHERE station = $1", station).Scan(&rows)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("view has %v rows of station %v, want 1", rows, station)
	}
}

// TestReadConnectionOnlyReads runs startup and the locate import of POST /queries/v2 with a read
// connection whose role may do nothing but SELECT. Both change the database - the startup deletes
// refresh jobs of views that are gone, the import creates a materialized view and its refresh job
// - so either fails here if it reaches the read connection. Skipped when TEST_POSTGRES_DSN is
// unset.
func TestReadConnectionOnlyReads(t *testing.T) {
	f := newReadTestFixture(t)
	f.exec(t, "CREATE OR REPLACE PROCEDURE "+wrapperMaterializedViewProcedureName+"(job_id INT, view JSONB) LANGUAGE PLPGSQL AS $$ BEGIN END $$")
	var staleJob int64
	err := f.admin.QueryRow(t.Context(), "SELECT add_job('"+wrapperMaterializedViewProcedureName+"', '1day', config => '\""+readTestStaleJob+"\"', scheduled => false)").Scan(&staleJob)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := f.wrapper(t, readTestRole)

	if err := wrapper.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var stale int
	if err := f.admin.QueryRow(t.Context(), "SELECT count(*) FROM timescaledb_information.jobs WHERE job_id = $1", staleJob).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Error("Migrate left the refresh job of a missing view in place")
	}

	f.locate(t, wrapper, "creating the view")
	f.locate(t, wrapper, "with the view in place")
	f.requireRefreshJobs(t, 1)
}

// TestLocateImportBehindALaggingReplica is the read connection pointing at a replica that has not
// replayed the materialized view: the import has to find it on the write connection instead of
// creating it a second time, and has to read a view it just created from there as well.
func TestLocateImportBehindALaggingReplica(t *testing.T) {
	f := newReadTestFixture(t)
	wrapper := f.wrapper(t, readTestLaggingRole)
	if err := wrapper.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Nothing exists yet: created on the primary, read back from the primary.
	f.locate(t, wrapper, "creating the view")
	f.requireRefreshJobs(t, 1)

	// The view exists on the primary only. Creating it again would fail with duplicate_table.
	f.locate(t, wrapper, "with the view on the primary only")
	f.requireRefreshJobs(t, 1)
}

// TestRefreshJobRefreshesTheView runs the refresh job the locate import creates and expects a
// station added to the export afterwards in the view. Until SNRGY-5103 the job's config named the
// export table, so every run failed with "is not a materialized view" and the view kept the
// stations it was created with. Skipped when TEST_POSTGRES_DSN is unset.
func TestRefreshJobRefreshesTheView(t *testing.T) {
	f := newReadTestFixture(t)
	wrapper := f.wrapper(t, readTestRole)
	if err := wrapper.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	f.locate(t, wrapper, "creating the view")
	f.requireRefreshJobs(t, 1)

	f.exec(t, "INSERT INTO "+quoteIdentifier(f.table)+" VALUES (now(), 'new', 52.0, 10.0)")
	f.runRefreshJob(t)
	f.requireStationInView(t, "new")
}

// TestMigrateRepairsRefreshJobs starts from a view and refresh job as created before SNRGY-5103:
// the job's config names the export table and the procedure renders it with %s. Migrate has to
// point the job at the view, and must not take it for orphaned on the next start although the
// view's name exceeds 63 bytes and is stored truncated: the fixture's table name is 59 bytes long,
// the view adds 5. Skipped when TEST_POSTGRES_DSN is unset.
func TestMigrateRepairsRefreshJobs(t *testing.T) {
	f := newReadTestFixture(t)
	f.exec(t,
		"CREATE OR REPLACE PROCEDURE "+wrapperMaterializedViewProcedureName+"(job_id INT, view JSONB) LANGUAGE PLPGSQL "+
			"AS $$ BEGIN EXECUTE format('REFRESH MATERIALIZED VIEW %s', view); END $$",
		"CREATE MATERIALIZED VIEW "+quoteIdentifier(locate.ViewName(f.table))+" AS SELECT DISTINCT station, lat, lon FROM "+quoteIdentifier(f.table),
		"SELECT add_job('"+wrapperMaterializedViewProcedureName+"', '1day', config => "+quoteSQLString(`"`+f.table+`"`)+", scheduled => false)",
	)
	wrapper := f.wrapper(t, readTestRole)

	for _, start := range []string{"first", "second"} {
		if err := wrapper.Migrate(t.Context()); err != nil {
			t.Fatalf("Migrate on the %v start: %v", start, err)
		}
		f.requireRefreshJobs(t, 1)
	}

	f.exec(t, "INSERT INTO "+quoteIdentifier(f.table)+" VALUES (now(), 'new', 52.0, 10.0)")
	f.runRefreshJob(t)
	f.requireStationInView(t, "new")
}

// TestMigrateConcurrently starts several replicas at once on a database without the procedure.
// Without the advisory lock in Migrate, concurrent CREATE OR REPLACE of the procedure fails with
// "tuple concurrently updated" or a duplicate key in pg_proc. Skipped when TEST_POSTGRES_DSN is
// unset.
func TestMigrateConcurrently(t *testing.T) {
	f := newReadTestFixture(t)
	f.exec(t, "DROP PROCEDURE IF EXISTS "+wrapperMaterializedViewProcedureName)
	wrapper := f.wrapper(t, readTestRole)

	errs := make(chan error, 8)
	wg := sync.WaitGroup{}
	for range cap(errs) {
		wg.Go(func() { errs <- wrapper.Migrate(t.Context()) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Migrate: %v", err)
		}
	}
}
