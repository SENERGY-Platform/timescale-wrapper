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

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestUsage reads the usage of devices and exports from a usage table. A device's usage is the sum
// over its service tables. Skipped when TEST_POSTGRES_DSN is unset.
func TestUsage(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const schema = "ts_wrapper_test_usage"
	short := func(id string) string {
		s, err := models.ShortenId(id)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	device := "urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4"
	otherDevice := "urn:infai:ses:device:0b1c2d3e-4f50-4617-8829-3a4b5c6d7e8f"
	service1 := "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050"
	service2 := "urn:infai:ses:service:1b6a3e1c-0d8f-4a51-9e43-2f6b5c7d8e90"
	export := "1b6a3e1c-0d8f-4a51-9e43-2f6b5c7d8e90"
	user := "97805820-ca0a-46c5-9dcf-16c2e386b050"
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	}
	cleanup()
	t.Cleanup(cleanup)
	for _, statement := range []string{
		"CREATE SCHEMA " + schema,
		"CREATE TABLE " + schema + `.usage ("table" text, bytes bigint, updated_at timestamptz, bytes_per_day double precision)`,
	} {
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(table string, bytes int64, updated string, perDay float64) {
		if _, err := pool.Exec(ctx, "INSERT INTO "+schema+`.usage VALUES ($1, $2, $3, $4)`, table, bytes, updated, perDay); err != nil {
			t.Fatal(err)
		}
	}
	insert("device:"+short(device)+"_service:"+short(service1), 100, "2026-01-02T00:00:00Z", 1.5)
	insert("device:"+short(device)+"_service:"+short(service2), 50, "2026-01-01T00:00:00Z", 0.5)
	insert("device:"+short(otherDevice)+"_service:"+short(service1), 7, "2026-01-01T00:00:00Z", 7)
	insert("userid:"+short(user)+"_export:"+short(export), 30, "2026-01-03T00:00:00Z", 3)

	wrapper := &Wrapper{config: &configuration.ConfigStruct{PostgresUsageSchema: schema}, readPool: pool, writePool: pool}

	devices, err := wrapper.GetDeviceUsage(ctx, []string{device})
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceId != device || devices[0].Bytes != 150 || devices[0].BytesPerDay != 2 || devices[0].UpdatedAt.Format("2006-01-02") != "2026-01-01" {
		t.Fatalf("device usage: %+v", devices)
	}
	exports, err := wrapper.GetExportUsage(ctx, []string{export})
	if err != nil {
		t.Fatal(err)
	}
	if len(exports) != 1 || exports[0].ExportId != export || exports[0].Bytes != 30 || exports[0].BytesPerDay != 3 {
		t.Fatalf("export usage: %+v", exports)
	}
	none, err := wrapper.GetDeviceUsage(ctx, nil)
	if err != nil || len(none) != 0 {
		t.Fatalf("no ids: %+v %v", none, err)
	}
}
