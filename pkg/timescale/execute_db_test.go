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

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestExecuteQueriesStopsAtTheFirstError runs a failing query next to a slow one. The slow query
// used to run to its end after the other one had failed, holding a connection for nothing.
// Skipped when TEST_POSTGRES_DSN is unset.
func TestExecuteQueriesStopsAtTheFirstError(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wrapper := &Wrapper{config: &configuration.ConfigStruct{}, readPool: pool, writePool: pool}

	start := time.Now()
	_, err = wrapper.ExecuteQueries(t.Context(), []string{"SELECT pg_sleep(20)", "SELECT * FROM ts_wrapper_table_that_does_not_exist"})
	if err == nil {
		t.Fatal("expected the error of the failing query")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("returned after %v, the slow query was not cancelled", elapsed)
	}

	res, err := wrapper.ExecuteQueries(t.Context(), []string{"SELECT 1", "SELECT 2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0][0][0] != int32(1) || res[1][0][0] != int32(2) {
		t.Fatalf("rows not in the order of the queries: %v", res)
	}
}
