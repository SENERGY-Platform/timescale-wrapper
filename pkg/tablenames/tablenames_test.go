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

package tablenames

import "testing"

// The expected strings below are literals that predate the move to this package: they are the
// table names pkg/timescale's tableName produced before DeviceTableName/ExportTableName existed
// (see pkg/timescale's "Test GenerateQueries Simple" and "Test GenerateQueries Export"), so a
// mismatch here means the table-naming rules moved, not just the code.
func TestDeviceTableName(t *testing.T) {
	actual, err := DeviceTableName("urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4", "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050")
	if err != nil {
		t.Fatal(err)
	}
	expected := "device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA"
	if actual != expected {
		t.Error("Expected/Actual\n", expected, "\n", actual)
	}
}

func TestExportTableName(t *testing.T) {
	actual, err := ExportTableName("ade1fba6-fa5f-4704-9997-81dc168f62f4", "97805820-ca0a-46c5-9dcf-16c2e386b050")
	if err != nil {
		t.Fatal(err)
	}
	expected := "userid:reH7pvpfRwSZl4HcFo9i9A_export:l4BYIMoKRsWdzxbC44awUA"
	if actual != expected {
		t.Error("Expected/Actual\n", expected, "\n", actual)
	}
}

// TestContinuousAggregateQuery pins the same SQL pkg/timescale's "Test CA Query" pinned for
// getCAQuery before the move; pkg/timescale additionally proves at runtime that getCAQuery still
// produces byte-identical output by calling both and comparing.
func TestContinuousAggregateQuery(t *testing.T) {
	columns := []ContinuousAggregateColumn{
		{Name: "test1", GroupType: "first"},
		{Name: "test.2", GroupType: "last"},
	}
	actual, err := ContinuousAggregateQuery("table", "1d", "Europe/Berlin", columns)
	if err != nil {
		t.Fatal(err)
	}
	expected := "SELECT view_name FROM (SELECT view_name, substring(view_definition, 'time_bucket\\((.*?)::interval, \"time\", ''Europe/Berlin''')::interval as bucket FROM timescaledb_information.continuous_aggregates WHERE hypertable_name = 'table' " +
		"AND view_definition LIKE '%first(test1, \"time\") AS test1%'" +
		"AND view_definition LIKE '%last(\"test.2\", \"time\") AS \"test.2\"%'" +
		") sub WHERE bucket <= '1d'::interval ORDER BY bucket DESC LIMIT 1;"
	if actual != expected {
		t.Error("Expected/Actual\n", expected, "\n", actual)
	}
}

func TestContinuousAggregateQueryRejectsMissingGroupType(t *testing.T) {
	_, err := ContinuousAggregateQuery("table", "1d", "Europe/Berlin", []ContinuousAggregateColumn{{Name: "test1"}})
	if err == nil {
		t.Error("expected an error for a column without a GroupType")
	}
}

func TestContinuousAggregateQueryRejectsMean(t *testing.T) {
	_, err := ContinuousAggregateQuery("table", "1d", "Europe/Berlin", []ContinuousAggregateColumn{{Name: "test1", GroupType: "mean"}})
	if err == nil {
		t.Error("expected an error for GroupType mean")
	}
}
