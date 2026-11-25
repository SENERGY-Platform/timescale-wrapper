/*
 *    Copyright 2021 InfAI (CC SES)
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
	"errors"
	"fmt"
	util "github.com/SENERGY-Platform/timescale-tableworker/pkg/lib/handler"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/sqlliteral"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tablenames"
)

// quoteColumnName renders a column name as the table worker names the column: hashed if it is too
// long for postgres, then quoted. util.HashFieldNameIfNeeded quotes without escaping, so the quotes
// it adds are replaced by an escaping quote. It stays in this package rather than in
// pkg/sqlliteral, whose callers outside this module would otherwise import the table worker and
// its Kafka and database dependencies with it.
func quoteColumnName(name string) string {
	hashed := util.HashFieldNameIfNeeded(name)
	return sqlliteral.QuoteIdentifier(strings.TrimSuffix(strings.TrimPrefix(hashed, "\""), "\""))
}

// Aliases of the lateral join that unnests a list, see columnValue. They are quoted and start with
// underscores so that they cannot clash with a column of the table.
const (
	jsonElementAlias   = `"__e"`
	jsonValueColumn    = `"__v"`
	jsonOrdinalColumn  = `"__n"`
	jsonElementValue   = jsonElementAlias + "." + jsonValueColumn
	jsonElementOrdinal = jsonElementAlias + "." + jsonOrdinalColumn
)

// jsonSQLTypes are the casts a model.JSONPath may carry. The type is part of the query text.
var jsonSQLTypes = map[string]bool{"": true, "text": true, "boolean": true, "double precision": true, "bigint": true}

// castJSONText converts a jsonb value to the SQL type of the path. A path without a type stays jsonb.
func castJSONText(value string, sqlType string) (string, error) {
	if !jsonSQLTypes[sqlType] {
		return "", errors.New("unsupported json path type")
	}
	if sqlType == "" {
		return value, nil
	}
	return "(" + value + " #>> '{}')::" + sqlType, nil
}

// columnValue returns the SQL expression for a requested column or filter name, and the join it
// needs in the FROM clause, which is empty for most columns.
//
// A regular column is its quoted, possibly hashed name. A value inside a JSONB column is read with
// jsonb_path_query_first and cast to the type of the variable; a missing element or field is NULL.
// A path with "*" yields all matches as a jsonb array, or, if aggregated is set, one row per match
// of a lateral join, which the aggregate then reduces. The join is a left join: a message without
// elements, or with a NULL list, keeps its row with a NULL value. Dropping it would remove buckets
// from this column only, and the FULL OUTER JOIN of the columns would then emit rows without a
// time. The ordinality of the join is what orders the elements of one message in first and last.
func columnValue(element model.QueriesRequestElement, name string, aggregated bool) (value string, join string, err error) {
	path, ok := element.JSONPaths[name]
	if !ok {
		return quoteColumnName(name), "", nil
	}
	literal, err := sqlliteral.JSONPath(path.Segments)
	if err != nil {
		return "", "", err
	}
	column := quoteColumnName(path.Column)
	if !path.Wildcard() {
		value, err = castJSONText("jsonb_path_query_first("+column+", "+literal+")", path.SQLType)
		return value, "", err
	}
	if !aggregated {
		return "jsonb_path_query_array(" + column + ", " + literal + ")", "", nil
	}
	value, err = castJSONText(jsonElementValue, path.SQLType)
	if err != nil {
		return "", "", err
	}
	join = " LEFT JOIN LATERAL jsonb_path_query(" + column + ", " + literal + ") WITH ORDINALITY AS " +
		jsonElementAlias + "(" + jsonValueColumn + ", " + jsonOrdinalColumn + ") ON TRUE"
	return value, join, nil
}

// firstLast renders first(value, time) or last(value, time). If the value comes from a lateral
// join over the elements of a list, several rows share one time, and the position in the list
// decides. Rows without an element are skipped, so that a later message with an empty list does
// not hide the last element of an earlier one.
func firstLast(function string, value string, unnested bool) string {
	if !unnested {
		return function + "(" + value + ", \"time\")"
	}
	direction := "ASC"
	if function == "last" {
		direction = "DESC"
	}
	return "(array_agg(" + value + " ORDER BY \"time\" " + direction + ", " + jsonElementOrdinal + " " + direction + ") FILTER (WHERE " + value + " IS NOT NULL))[1]"
}

// aggregateExpression renders the aggregate over value that a time bucket is reduced with.
// Difference aggregates with the function it is based on; the subtraction of the previous bucket
// is done by the outer query. TimeWeighted has its own subquery (timeWeightedQuery) and is not
// handled here.
func aggregateExpression(agg model.Aggregation, value string, unnested bool) string {
	if agg.Function == "first" || agg.Function == "last" {
		return firstLast(agg.Function, value, unnested)
	}
	return sqlliteral.FunctionName(agg.Function) + value + ")"
}

// GenerateQueries renders one SQL query per element: a raw query if the element has no group
// time, otherwise the buckets of its columns joined on their time.
func (wrapper *Wrapper) GenerateQueries(ctx context.Context, elements []model.QueriesRequestElement, userId string, ownerUserIds []string, forceTz string, devices []models.Device) (queries []string, err error) {
	queries = make([]string, len(elements))
	for i, element := range elements {
		timezone, err := wrapper.resolveTimezone(element, forceTz, devices)
		if err != nil {
			return nil, err
		}
		table, err := wrapper.tableName(ctx, element, ownerUserIds[i], timezone)
		if err != nil {
			return queries, err
		}
		query, err := newElementQuery(element, table, timezone)
		if err != nil {
			return nil, err
		}
		if element.GroupTime != nil {
			queries[i], err = query.grouped()
		} else {
			queries[i], err = query.raw()
		}
		if err != nil {
			return nil, err
		}
	}
	return queries, nil
}

// resolveTimezone returns the time zone the buckets of an element are aligned to: forceTz if
// given, else the zone of the element's device, else the default.
func (wrapper *Wrapper) resolveTimezone(element model.QueriesRequestElement, forceTz string, devices []models.Device) (string, error) {
	if len(forceTz) > 0 {
		if !model.TimezoneValid(forceTz) {
			return "", errors.New("invalid timezone")
		}
		return forceTz, nil
	}
	if element.DeviceId != nil {
		return getTZ(*element.DeviceId, devices, wrapper.config.DefaultTimezone), nil
	}
	return wrapper.config.DefaultTimezone, nil // no special tz support for exports
}

// columnSQL is a requested column with the SQL its value is read with.
type columnSQL struct {
	name string
	math *string
	// value is the SQL expression of the column, join the lateral join it needs, see columnValue.
	value string
	join  string
	// agg is the aggregation of the column, the zero value for a column of a raw query.
	agg model.Aggregation
}

// elementQuery holds what the query of one element is built from.
type elementQuery struct {
	// element is the element as the query reads it: its time window is already widened for difference
	// columns, see widenTimeWindowForDifference.
	element  model.QueriesRequestElement
	table    string
	timezone string
	columns  []columnSQL
	// differenceBuckets is the number of buckets requested from a widened window, nil if it was not
	// widened.
	differenceBuckets *int
}

// newElementQuery resolves the columns of an element. A raw element has no aggregation on any
// column, a grouped element one on every column.
func newElementQuery(element model.QueriesRequestElement, table string, timezone string) (elementQuery, error) {
	query := elementQuery{element: element, table: table, timezone: timezone, columns: make([]columnSQL, len(element.Columns))}
	grouped := element.GroupTime != nil
	for i, column := range element.Columns {
		query.columns[i] = columnSQL{name: column.Name, math: column.Math}
		if !grouped {
			if column.GroupType != nil {
				return query, errors.New("mixing aggregate and non-aggregate queries is not supported\n")
			}
			continue
		}
		if column.GroupType == nil {
			return query, errors.New("mixing aggregate and non-aggregate queries is not supported\n")
		}
		agg, ok := column.Aggregation()
		if !ok {
			return query, errors.Join(errors.New("unknown group type "+*column.GroupType), model.ErrBadRequest)
		}
		query.columns[i].agg = agg
	}
	if grouped {
		var err error
		if query.element, query.differenceBuckets, err = widenTimeWindowForDifference(element); err != nil {
			return query, err
		}
	}
	for i, column := range query.columns {
		var err error
		if column.value, column.join, err = columnValue(query.element, column.name, grouped); err != nil {
			return query, err
		}
		query.columns[i] = column
	}
	return query, nil
}

// raw renders the query of an element without group time: the rows of the table.
func (q elementQuery) raw() (string, error) {
	query := "SELECT \"time\", "
	for i, column := range q.columns {
		if i > 0 {
			query += ", "
		}
		query += column.value
		if column.math != nil {
			query += *column.math
		}
		query += " AS " + sqlliteral.QuoteIdentifier(column.name)
	}
	query += " FROM " + sqlliteral.QuoteIdentifier(q.table)
	where, err := getWhereString(q.element)
	if err != nil {
		return "", err
	}
	return query + where + defaultOrderLimit(q.element).String(), nil
}

// grouped renders the query of an element with group time: one subquery per column that reads the
// buckets of the column, full outer joined on the bucket time.
func (q elementQuery) grouped() (string, error) {
	query := "SELECT " + bucketTime(len(q.columns)) + " AS \"time\", "
	for i, column := range q.columns {
		if i > 0 {
			query += ", "
		}
		query += "(" + subAlias(i) + ".value"
		if column.agg.Kind == model.Difference {
			// ORDER BY 1 would be the constant 1, not a positional reference: all rows
			// would be peers and lag() would pair whatever rows the join happens to emit
			// next to each other. Order by the column's own bucket instead.
			query += " - lag(" + subAlias(i) + ".value) OVER (ORDER BY " + subAlias(i) + ".time)"
		}
		query += ") "
		if column.math != nil {
			query += *column.math + " "
		}
		query += "AS " + sqlliteral.QuoteIdentifier(column.name)
	}
	query += " FROM "
	for i := range q.columns {
		subquery, err := q.bucketSubquery(i)
		if err != nil {
			return "", err
		}
		query += subquery
		if i > 0 {
			query += " on " + bucketTime(i) + " = " + subAlias(i) + ".time"
		}
		if i < len(q.columns)-1 {
			query += " FULL OUTER JOIN "
		}
	}

	outer := defaultOrderLimit(q.element)
	if q.differenceBuckets != nil {
		// the widened window holds one bucket more than was requested: the newest ones are kept
		outer = orderLimit{index: 0, direction: model.Desc, limit: q.differenceBuckets}
	}
	return query + outer.String(), nil
}

// bucketSubquery renders the subquery with the buckets of column i, named subAlias(i).
func (q elementQuery) bucketSubquery(i int) (string, error) {
	column := q.columns[i]
	// All columns of a widened element read the same rows in the same order, see
	// widenTimeWindowForDifference. Otherwise "last" reads from the newest bucket on and the
	// others from the oldest.
	rows := orderLimit{groupBy: true, index: 0, direction: model.Asc, limit: q.element.Limit}
	if q.differenceBuckets != nil {
		n := *q.differenceBuckets + 2
		rows.limit = &n
	} else if column.agg.Kind == model.Plain && column.agg.Function == "last" {
		rows.direction = model.Desc
	}

	if column.agg.Kind == model.TimeWeighted {
		if column.join != "" {
			return "", errors.Join(errors.New("time-weighted group types can not be combined with all elements of a list"), model.ErrBadRequest)
		}
		subquery, err := timeWeightedQuery(q.element, column.agg.Function, column.value, q.table, q.timezone, rows.String())
		if err != nil {
			return "", err
		}
		return "(" + subquery + ") " + subAlias(i), nil
	}
	where, err := getWhereString(q.element)
	if err != nil {
		return "", err
	}
	subquery := "(SELECT time_bucket(" + sqlliteral.QuoteString(*q.element.GroupTime) + ", \"time\", " + sqlliteral.QuoteString(q.timezone) + ") AS \"time\", "
	subquery += aggregateExpression(column.agg, column.value, column.join != "")
	subquery += " AS value FROM " + sqlliteral.QuoteIdentifier(q.table) + column.join
	return subquery + where + rows.String() + ") " + subAlias(i), nil
}

// widenTimeWindowForDifference returns a copy of the element whose time window is extended by one
// unit if it has difference-* columns, so that the first requested bucket has a predecessor to be
// differenced against, and reports how many buckets the caller originally asked for. The element
// passed in is not changed: its time is copied before it is. Elements without a difference column,
// and elements without a time window, are returned as they are and yield a nil bucket count.
//
// It runs once per element, before the first column subquery is generated. Every column of such an
// element then reads the same window with the same row limit: widening per column would give the
// subqueries different bucket sets, and the FULL OUTER JOIN combining them would then emit rows
// that match nothing on the other side.
//
// Two limits of the widening, both older than this function and both unchanged by it: it moves the
// window by one unit rather than by one bucket, which is the same thing only while the group time
// has no multiplier ("1h", not "6h"); and for Time.Ahead it moves the far end into the future,
// which does not produce a predecessor for the first bucket at all.
func widenTimeWindowForDifference(element model.QueriesRequestElement) (widened model.QueriesRequestElement, buckets *int, err error) {
	hasDifference := false
	for _, column := range element.Columns {
		if agg, ok := column.Aggregation(); ok && agg.Kind == model.Difference {
			hasDifference = true
			break
		}
	}
	if !hasDifference || element.Time == nil {
		return element, nil, nil
	}

	if element.Time.Last != nil || element.Time.Ahead != nil {
		// manually increase the last offset by 1 to ensure unified results
		copy := element.Time.Copy() // ensure no other elements are affected that share this pointer
		element.Time = &copy
		re := regexp.MustCompile(`\d+`)
		var prefix string
		if element.Time.Last != nil {
			prefix = string(re.Find([]byte(*element.Time.Last)))
		} else {
			prefix = string(re.Find([]byte(*element.Time.Ahead)))
		}
		num, err := strconv.Atoi(prefix)
		if err != nil {
			return element, nil, err
		}
		n := num
		num++
		if element.Time.Last != nil {
			modified := strconv.Itoa(num) + strings.TrimPrefix(*element.Time.Last, prefix)
			element.Time.Last = &modified
		} else {
			modified := strconv.Itoa(num) + strings.TrimPrefix(*element.Time.Ahead, prefix)
			element.Time.Ahead = &modified
		}
		return element, &n, nil
	}

	if element.Time.Start == nil || element.Time.End == nil || element.GroupTime == nil {
		return element, nil, nil
	}
	copy := element.Time.Copy() // ensure no other elements are affected that share this pointer
	element.Time = &copy
	re := regexp.MustCompile(`\D+`)
	suffix := re.Find([]byte(*element.GroupTime))
	if suffix == nil {
		return element, nil, fmt.Errorf("could not parse GroupTime %v", *element.GroupTime)
	}
	endT, err := time.Parse(time.RFC3339, *element.Time.End)
	if err != nil {
		return element, nil, err
	}
	startT, err := time.Parse(time.RFC3339, *element.Time.Start)
	if err != nil {
		return element, nil, err
	}
	diff := endT.Sub(startT)
	diffT := 0
	const before = -1
	const after = 0
	var start time.Time
	var end time.Time
	switch string(suffix) {
	case "ms":
		diffT = int(diff.Milliseconds())
		start = startT.Add(before * time.Millisecond)
		end = endT.Add(after * time.Millisecond)
	case "s":
		diffT = int(diff.Seconds())
		start = startT.Add(before * time.Second)
		end = endT.Add(after * time.Second)
	case "months":
		fallthrough
	case "mon":
		start = startT.AddDate(0, before, 0)
		end = endT.AddDate(0, after, 0)
		diffT = (endT.Year()-startT.Year())*12 + (int(endT.Month()) - int(startT.Month()))
	case "m":
		start = startT.Add(before * time.Minute)
		end = endT.Add(after * time.Minute)
		diffT = int(diff.Minutes())
	case "h":
		start = startT.Add(before * time.Hour)
		end = endT.Add(after * time.Hour)
		diffT = int(diff.Hours())
	case "day":
		fallthrough
	case "d":
		start = startT.Add(before * 24 * time.Hour)
		end = endT.Add(after * 24 * time.Hour)
		diffT = int(diff.Hours() / 24)
	case "w":
		start = startT.Add(before * 24 * 7 * time.Hour)
		end = endT.Add(after * 24 * 7 * time.Hour)
		diffT = int(diff.Hours() / 24 / 7)
	case "y":
		start = startT.AddDate(before, 0, 0)
		end = endT.AddDate(after, 0, 0)
		diffT = endT.Year() - startT.Year()
	}
	startS := start.Format(time.RFC3339)
	element.Time.Start = &startS
	endS := end.Format(time.RFC3339)
	element.Time.End = &endS
	return element, &diffT, nil
}

// getWhereString renders the filters and the time window of an element as a WHERE clause, or the
// empty string if the element has neither.
func getWhereString(element model.QueriesRequestElement) (string, error) {
	conditions, err := getFilterConditions(element)
	if err != nil {
		return "", err
	}
	if element.Time != nil {
		condition, err := timeCondition(element.Time)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, condition)
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), nil
}

// getFilterConditions renders the filters of an element as SQL conditions, one per filter.
func getFilterConditions(element model.QueriesRequestElement) (conditions []string, err error) {
	if element.Filters == nil {
		return nil, nil
	}
	for _, filter := range *element.Filters {
		// filters are evaluated per row, so "*" is not valid here (see QueriesRequestElementFilter.Valid)
		filterColumn, _, err := columnValue(element, filter.Column, false)
		if err != nil {
			return nil, err
		}
		if path, ok := element.JSONPaths[filter.Column]; ok && path.Wildcard() {
			return nil, errors.Join(errors.New("filtering on all elements of a list is not supported"), model.ErrBadRequest)
		}
		condition := ""
		if filter.Value == nil {
			condition = filterColumn + " IS "
			if filter.Type == "!=" {
				condition += "NOT "
			}
			condition += "NULL"
		} else {
			value, err := sqlliteral.ValueLiteral(filter.Value)
			if err != nil {
				return nil, err
			}
			condition = filterColumn
			if filter.Math != nil {
				condition += *filter.Math + " "
			}
			condition += filter.Type
			condition += " " + value
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
}

// tableName builds the hypertable name and, if a matching continuous aggregate view exists,
// substitutes it. DeviceTableName, ExportTableName and the continuous-aggregate lookup itself
// live in pkg/tablenames so a caller outside this module can reuse them without depending on the
// wrapper's request types or its other, heavier dependencies; this method and getCAQuery are the
// callers within this module.
func (wrapper *Wrapper) tableName(ctx context.Context, element model.QueriesRequestElement, userId string, timezone string) (table string, err error) {
	if element.ExportId != nil {
		table, err = tablenames.ExportTableName(userId, *element.ExportId)
		if err != nil {
			return "", err
		}
	} else {
		table, err = tablenames.DeviceTableName(*element.DeviceId, *element.ServiceId)
		if err != nil {
			return "", err
		}
	}
	if element.GroupTime != nil && wrapper.readPool != nil && len(element.JSONPaths) == 0 {
		// check if CA View available. Continuous aggregates are built over plain columns and do not
		// cover values read from inside a JSONB column.
		query, err := getCAQuery(element, table, timezone)
		if err != nil {
			log.Logger.WarnContext(ctx, "getCAQuery failed", "error", err)
			return table, nil
		}

		var caTable string
		if wrapper.config.Debug {
			log.Logger.DebugContext(ctx, "Checking for CA View with: "+query)
		}
		err = wrapper.readPool.QueryRow(ctx, query).Scan(&caTable)
		if err == nil {
			return caTable, nil
		} else {
			return table, nil
		}
	}
	return table, nil

}

// getCAQuery adapts an element's columns to tablenames.ContinuousAggregateColumn and delegates to
// tablenames.ContinuousAggregateQuery. A column with a nil GroupType maps to the empty string,
// which ContinuousAggregateQuery rejects the same way this function used to reject it directly.
func getCAQuery(element model.QueriesRequestElement, table string, timezone string) (string, error) {
	columns := make([]tablenames.ContinuousAggregateColumn, len(element.Columns))
	for i, column := range element.Columns {
		groupType := ""
		if column.GroupType != nil {
			groupType = *column.GroupType
		}
		columns[i] = tablenames.ContinuousAggregateColumn{Name: column.Name, GroupType: groupType}
	}
	query, err := tablenames.ContinuousAggregateQuery(table, *element.GroupTime, timezone, columns)
	if err != nil {
		return table, err
	}
	return query, nil
}

func getTZ(deviceId string, devices []models.Device, defaultTZ string) string {
	for _, d := range devices {
		if d.Id == deviceId {
			for _, a := range d.Attributes {
				if strings.ToLower(a.Key) == "timezone" {
					if !model.TimezoneValid(a.Value) {
						return defaultTZ // device attributes are not trusted input
					}
					return a.Value
				}
			}
			return defaultTZ
		}
	}
	return defaultTZ
}
