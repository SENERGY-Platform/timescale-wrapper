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
// This is a leaf package on purpose: it imports nothing beyond the standard library,
// github.com/SENERGY-Platform/models/go/models and pkg/sqlliteral (literal quoting, which needs
// pgx for identifiers), so a caller outside this module can compute the same table names and run
// the same continuous-aggregate lookup without also pulling in the wrapper's tracing and other
// service dependencies.
package tablenames

import (
	"errors"
	"strings"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/sqlliteral"
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
// columns with its given function. Which aggregations a view can answer: see
// continuousAggregateCall.
func ContinuousAggregateQuery(table string, bucketInterval string, timezone string, columns []ContinuousAggregateColumn) (string, error) {
	// timezone, table and column names are embedded in string literals, so single quotes have
	// to be doubled
	query := "SELECT view_name FROM (SELECT view_name, substring(view_definition, 'time_bucket\\((.*?)::interval, \"time\", ''" + sqlliteral.EscapeString(timezone) + "''')::interval as bucket FROM timescaledb_information.continuous_aggregates WHERE hypertable_name = " + sqlliteral.QuoteString(table) + " "

	for _, column := range columns {
		if column.GroupType == "" {
			return "", errors.New("expected all columns to contain GroupType")
		}
		call, err := continuousAggregateCall(column)
		if err != nil {
			return "", err
		}
		// PostgreSQL prints one select item per line: the item ends with a comma, or with the line
		// break before FROM. Matching the end keeps "AS value" from matching "AS value_last".
		pattern := sqlliteral.EscapeString(likeEscape(call))
		query += "AND view_definition LIKE ANY (ARRAY['%" + pattern + ",%', '%" + pattern + "' || chr(10) || '%']) "
	}
	query += ") sub WHERE bucket <= " + sqlliteral.QuoteString(bucketInterval) + "::interval ORDER BY bucket DESC"
	query += " LIMIT 1;"

	return query, nil
}

// continuousAggregateCall renders the aggregate of column as PostgreSQL prints it in the
// definition of a continuous aggregate, e.g. last(value, "time") AS value or max("a.b") AS "a.b".
//
// The caller buckets the rows of the view again with the same function, so a view only answers
// aggregations that give the same result over pre-aggregated buckets as over the raw rows: first,
// last, min, max and sum, and their difference-* forms. mean, median and count do not (a mean of
// means, a count of buckets), and time-weighted means need the raw points.
func continuousAggregateCall(column ContinuousAggregateColumn) (string, error) {
	function := column.GroupType
	if base, found := strings.CutPrefix(function, "difference-"); found {
		function = base
	}
	name := viewDefinitionIdentifier(column.Name)
	switch function {
	case "first", "last":
		return function + "(" + name + ", \"time\") AS " + name, nil
	case "min", "max", "sum":
		return function + "(" + name + ") AS " + name, nil
	default:
		return "", errors.New("group type " + column.GroupType + " can not be read from a continuous aggregate")
	}
}

// likeEscape escapes the wildcards of LIKE, with the default escape character backslash, so that
// s matches only itself: "_" in a column name would otherwise match any character.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// viewDefinitionIdentifier renders name as PostgreSQL prints an identifier in a view definition:
// unquoted if it consists of lower case letters, digits and underscores and does not start with a
// digit, quoted otherwise. Reserved words, which PostgreSQL quotes as well, are not handled; no
// column of a device table is named like one.
func viewDefinitionIdentifier(name string) string {
	plain := name != ""
	for i, r := range name {
		if !(r >= 'a' && r <= 'z' || r == '_' || i > 0 && r >= '0' && r <= '9') {
			plain = false
			break
		}
	}
	if plain {
		return name
	}
	return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
}

// EscapeSQLString escapes s for use inside a single quoted SQL string literal, without adding the
// surrounding quotes. It forwards to pkg/sqlliteral and stays for callers outside this module.
func EscapeSQLString(s string) string {
	return sqlliteral.EscapeString(s)
}

// QuoteSQLString renders s as a single quoted SQL string literal. It forwards to pkg/sqlliteral
// and stays for callers outside this module.
func QuoteSQLString(s string) string {
	return sqlliteral.QuoteString(s)
}

// TranslateFunctionName maps a GroupType aggregation name to the start of the SQL function call
// that implements it, e.g. "mean" to "avg(" and "difference-last" to "last(". It forwards to
// pkg/sqlliteral and stays for callers outside this module.
func TranslateFunctionName(name string) string {
	return sqlliteral.FunctionName(name)
}
