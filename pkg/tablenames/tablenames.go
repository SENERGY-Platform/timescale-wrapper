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

// Package tablenames builds the hypertable names timescale-wrapper addresses devices and exports
// by, and looks up a continuous aggregate view matching a bucketed query. pkg/timescale's
// tableName and getCAQuery delegate here instead of keeping a second copy of this logic.
//
// This is a leaf package on purpose: it imports nothing beyond the standard library and
// github.com/SENERGY-Platform/models/go/models, so a caller outside this module can compute the
// same table names and run the same continuous-aggregate lookup without also pulling in the
// wrapper's database driver, tracing and other service dependencies.
package tablenames

import (
	"errors"
	"strings"

	"github.com/SENERGY-Platform/models/go/models"
)

// DeviceTableName returns the hypertable name for a device/service pair: both ids shortened with
// models.ShortenId and joined as "device:<short>_service:<short>".
func DeviceTableName(deviceId, serviceId string) (string, error) {
	shortDeviceId, err := models.ShortenId(deviceId)
	if err != nil {
		return "", err
	}
	shortServiceId, err := models.ShortenId(serviceId)
	if err != nil {
		return "", err
	}
	return "device:" + shortDeviceId + "_" + "service:" + shortServiceId, nil
}

// ExportTableName returns the hypertable name for a user/export pair, joined as
// "userid:<short>_export:<short>".
func ExportTableName(userId, exportId string) (string, error) {
	shortUserId, err := models.ShortenId(userId)
	if err != nil {
		return "", err
	}
	shortExportId, err := models.ShortenId(exportId)
	if err != nil {
		return "", err
	}
	return "userid:" + shortUserId + "_" + "export:" + shortExportId, nil
}

// ContinuousAggregateColumn describes one column of a continuous-aggregate lookup: its name and
// the aggregation function applied to it, using the same GroupType vocabulary
// model.QueriesRequestElementColumn.GroupType accepts in timescale-wrapper (e.g. "last",
// "difference-last", "time-weighted-mean-linear").
type ContinuousAggregateColumn struct {
	Name      string
	GroupType string
}

// ContinuousAggregateQuery returns the SQL that looks up, in
// timescaledb_information.continuous_aggregates, a continuous aggregate view on table whose
// bucket size is at most bucketInterval and whose view definition aggregates every one of
// columns with its given function.
func ContinuousAggregateQuery(table string, bucketInterval string, timezone string, columns []ContinuousAggregateColumn) (string, error) {
	// timezone, table and column names are embedded in string literals, so single quotes have
	// to be doubled
	query := "SELECT view_name FROM (SELECT view_name, substring(view_definition, 'time_bucket\\((.*?)::interval, \"time\", ''" + EscapeSQLString(timezone) + "''')::interval as bucket FROM timescaledb_information.continuous_aggregates WHERE hypertable_name = " + QuoteSQLString(table) + " "

	for _, column := range columns {
		if column.GroupType == "" {
			return "", errors.New("expected all columns to contain GroupType")
		}
		if column.GroupType == "mean" {
			// not implemented
			return "", errors.New("")
		}
		escapedName := EscapeSQLString(column.Name)
		query += "AND view_definition LIKE '%" + strings.ReplaceAll(TranslateFunctionName(column.GroupType), "'", "''")
		containsDot := strings.Contains(column.Name, ".")
		if containsDot {
			query += "\"" + escapedName + "\""
		} else {
			query += escapedName
		}
		query += ", \"time\") AS "
		if containsDot {
			query += "\"" + escapedName + "\""
		} else {
			query += escapedName
		}
		query += "%'"
	}
	query += ") sub WHERE bucket <= " + QuoteSQLString(bucketInterval) + "::interval ORDER BY bucket DESC"
	query += " LIMIT 1;"

	return query, nil
}

// EscapeSQLString escapes s for use inside a single quoted SQL string literal, without adding the
// surrounding quotes.
func EscapeSQLString(s string) string {
	s = strings.ReplaceAll(s, string([]byte{0}), "")
	return strings.ReplaceAll(s, "'", "''")
}

// QuoteSQLString renders s as a single quoted SQL string literal.
func QuoteSQLString(s string) string {
	return "'" + EscapeSQLString(s) + "'"
}

// TranslateFunctionName maps a GroupType aggregation name to the start of the SQL function call
// that implements it, e.g. "mean" to "avg(" and "difference-last" to "last(".
func TranslateFunctionName(name string) string {
	switch name {
	case "mean":
		return "avg("
	case "median":
		return "percentile_disc(0.5) WITHIN GROUP (ORDER BY "
	case "time-weighted-mean-linear":
		return "average(time_weight('Linear', \"time\", "
	case "time-weighted-mean-locf":
		return "average(time_weight('LOCF', \"time\", "
	default:
		if strings.HasPrefix(name, "difference") {
			parts := strings.Split(name, "-")
			return parts[1] + "("
		}
		return name + "("
	}
}
