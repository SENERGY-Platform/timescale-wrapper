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
		"SELECT delete_job(job_id) FROM timescaledb_information.jobs WHERE proc_name = '" + wrapperMaterializedViewProcedureName +
			"' AND config::text IN ('\"" + readTestStaleJob + "\"', '\"" + f.table + "\"')",
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

func (f *readTestFixture) requireRefreshJobs(t *testing.T, want int) {
	t.Helper()
	var jobs int
	err := f.admin.QueryRow(t.Context(), "SELECT count(*) FROM timescaledb_information.jobs WHERE proc_name = $1 AND config::text = $2",
		wrapperMaterializedViewProcedureName, `"`+f.table+`"`).Scan(&jobs)
	if err != nil {
		t.Fatal(err)
	}
	if jobs != want {
		t.Errorf("got %v refresh jobs for the view, want %v", jobs, want)
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

	// Nothing exists yet: created on the primary, read back from the primary.
	f.locate(t, wrapper, "creating the view")
	f.requireRefreshJobs(t, 1)

	// The view exists on the primary only. Creating it again would fail with duplicate_table.
	f.locate(t, wrapper, "with the view on the primary only")
	f.requireRefreshJobs(t, 1)
}
