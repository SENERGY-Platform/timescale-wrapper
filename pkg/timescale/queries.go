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

func (wrapper *Wrapper) GenerateQueries(ctx context.Context, elements []model.QueriesRequestElement, userId string, ownerUserIds []string, forceTz string, devices []models.Device) (queries []string, err error) {
	queries = make([]string, len(elements))
	for i, element := range elements {
		var timezone string
		if len(forceTz) > 0 {
			if !model.TimezoneValid(forceTz) {
				return nil, errors.New("invalid timezone")
			}
			timezone = forceTz
		} else {
			if element.DeviceId != nil {
				timezone = getTZ(*element.DeviceId, devices, wrapper.config.DefaultTimezone)
			} else {
				timezone = wrapper.config.DefaultTimezone // no special tz support for exports
			}
		}
		table, err := wrapper.tableName(ctx, element, ownerUserIds[i], timezone)
		if err != nil {
			return queries, err
		}

		query := ""
		query += "SELECT "
		if element.GroupTime != nil {
			zero := 0
			asc := model.Asc
			desc := model.Desc
			query += bucketTime(len(element.Columns)) + " AS \"time\", "
			for idx, column := range element.Columns {
				if column.GroupType == nil {
					return nil, errors.New("mixing aggregate and non-aggregate queries is not supported\n")
				}
				if idx > 0 {
					query += ", "
				}
				query += "(sub" + strconv.Itoa(idx) + ".value"
				if strings.HasPrefix(*column.GroupType, "difference") {
					// ORDER BY 1 would be the constant 1, not a positional reference: all rows
					// would be peers and lag() would pair whatever rows the join happens to emit
					// next to each other. Order by the column's own bucket instead.
					query += " - lag(sub" + strconv.Itoa(idx) + ".value) OVER (ORDER BY sub" + strconv.Itoa(idx) + ".time)"
				}
				query += ") "
				if column.Math != nil {
					query += *column.Math + " "
				}
				query += "AS " + sqlliteral.QuoteIdentifier(column.Name)
			}
			query += " FROM "
			l, err := widenTimeWindowForDifference(&element)
			if err != nil {
				return nil, err
			}
			elements[i] = element
			for idx, column := range element.Columns {
				columnValueSQL, join, err := columnValue(element, column.Name, true)
				if err != nil {
					return nil, err
				}
				var groupOrderLimit string
				if l != nil {
					n := *l
					n += 2
					groupOrderLimit = getOrderLimitString(element, true, &zero, &asc, &n)
				} else {
					dir := asc
					if column.GroupType != nil && *column.GroupType == "last" {
						dir = desc
					}
					groupOrderLimit = getOrderLimitString(element, true, &zero, &dir, nil)
				}
				if strings.HasPrefix(*column.GroupType, "time-weighted-") {
					if join != "" {
						return nil, errors.Join(errors.New("time-weighted group types can not be combined with all elements of a list"), model.ErrBadRequest)
					}
					subquery, err := timeWeightedQuery(element, *column.GroupType, columnValueSQL, table, timezone, groupOrderLimit)
					if err != nil {
						return nil, err
					}
					query += "(" + subquery + ") sub" + strconv.Itoa(idx)
				} else {
					unnested := join != ""
					query += "(SELECT time_bucket(" + sqlliteral.QuoteString(*element.GroupTime) + ", \"time\", " + sqlliteral.QuoteString(timezone) + ") AS \"time\", "
					if strings.HasPrefix(*column.GroupType, "difference") {
						groupParts := strings.Split(*column.GroupType, "-")
						if groupParts[1] == "first" || groupParts[1] == "last" {
							query += firstLast(groupParts[1], columnValueSQL, unnested)
						} else {
							query += sqlliteral.FunctionName(groupParts[1]) + columnValueSQL + ")"
						}
					} else if *column.GroupType == "first" || *column.GroupType == "last" {
						query += firstLast(*column.GroupType, columnValueSQL, unnested)
					} else {
						query += sqlliteral.FunctionName(*column.GroupType) + columnValueSQL + ")"
					}
					query += " AS value"
					query += " FROM " + sqlliteral.QuoteIdentifier(table) + join
					where, err := getWhereString(element)
					if err != nil {
						return nil, err
					}
					query += where + groupOrderLimit + ") sub" + strconv.Itoa(idx)
				}
				if idx > 0 {
					query += " on " + bucketTime(idx) + " = sub" + strconv.Itoa(idx) + ".time"
				}
				if idx < len(element.Columns)-1 {
					query += " FULL OUTER JOIN "
				}
			}

			var limit *int
			var order *model.Direction
			var orderIndex *int
			if l != nil {
				limit = l
				order = &desc
				orderIndex = &zero
			}
			query += getOrderLimitString(element, false, orderIndex, order, limit)

		} else {
			query += "\"time\", "
			for idx, column := range element.Columns {
				if column.GroupType != nil {
					return nil, errors.New("mixing aggregate and non-aggregate queries is not supported\n")
				}
				if idx > 0 {
					query += ", "
				}
				columnValueSQL, _, err := columnValue(element, column.Name, false)
				if err != nil {
					return nil, err
				}
				query += columnValueSQL
				if column.Math != nil {
					query += *column.Math
				}
				query += " AS " + sqlliteral.QuoteIdentifier(column.Name)
			}
			query += " FROM " + sqlliteral.QuoteIdentifier(table)
			filterString, err := getFilterString(element, false, nil, nil, nil)
			if err != nil {
				return nil, err
			}
			query += filterString
		}
		queries[i] = query
	}
	return
}

// bucketTime is the bucket of a row of the FULL OUTER JOIN over the first n column subqueries.
// The subqueries can hold different buckets: a "last" column reads its rows in descending order
// and the others in ascending order, so with a limit they cover different ends of the window. A
// bucket only a later column has would otherwise get no time, and the next column, joined on
// sub0.time, would not join to it and land on a row of its own.
func bucketTime(n int) string {
	if n <= 1 {
		return "sub0.time"
	}
	subs := make([]string, n)
	for i := range subs {
		subs[i] = "sub" + strconv.Itoa(i) + ".time"
	}
	return "COALESCE(" + strings.Join(subs, ", ") + ")"
}

// widenTimeWindowForDifference extends the time window of an element that has difference-* columns
// by one unit, so that the first requested bucket has a predecessor to be differenced against, and
// reports how many buckets the caller originally asked for. Elements without a difference column,
// and elements without a time window, are left untouched and yield a nil bucket count.
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
func widenTimeWindowForDifference(element *model.QueriesRequestElement) (buckets *int, err error) {
	hasDifference := false
	for _, column := range element.Columns {
		if column.GroupType != nil && strings.HasPrefix(*column.GroupType, "difference") {
			hasDifference = true
			break
		}
	}
	if !hasDifference || element.Time == nil {
		return nil, nil
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
			return nil, err
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
		return &n, nil
	}

	if element.Time.Start == nil || element.Time.End == nil || element.GroupTime == nil {
		return nil, nil
	}
	copy := element.Time.Copy() // ensure no other elements are affected that share this pointer
	element.Time = &copy
	re := regexp.MustCompile(`\D+`)
	suffix := re.Find([]byte(*element.GroupTime))
	if suffix == nil {
		return nil, fmt.Errorf("could not parse GroupTime %v", *element.GroupTime)
	}
	endT, err := time.Parse(time.RFC3339, *element.Time.End)
	if err != nil {
		return nil, err
	}
	startT, err := time.Parse(time.RFC3339, *element.Time.Start)
	if err != nil {
		return nil, err
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
	element.Time.EndOriginal = element.Time.End
	element.Time.End = &endS
	return &diffT, nil
}

func getFilterString(element model.QueriesRequestElement, group bool, overrideSortIndex *int, overrideOrderDirection *model.Direction, overrideLimit *int) (query string, err error) {
	query, err = getWhereString(element)
	if err != nil {
		return "", err
	}
	query += getOrderLimitString(element, group, overrideSortIndex, overrideOrderDirection, overrideLimit)
	return
}

// getWhereString renders the filters and the time window of an element as a WHERE clause, or the
// empty string if the element has neither.
func getWhereString(element model.QueriesRequestElement) (string, error) {
	conditions, err := getFilterConditions(element)
	if err != nil {
		return "", err
	}
	if element.Time != nil {
		if element.Time.Last != nil {
			conditions = append(conditions, "\"time\" > now() - interval "+sqlliteral.QuoteString(*element.Time.Last))
		} else if element.Time.Ahead != nil {
			conditions = append(conditions, "\"time\" > now() AND \"time\" < now() + interval "+sqlliteral.QuoteString(*element.Time.Ahead))
		} else {
			conditions = append(conditions, "\"time\" > "+sqlliteral.QuoteString(*element.Time.Start)+" AND \"time\" < "+sqlliteral.QuoteString(*element.Time.End))
		}
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

func getOrderLimitString(element model.QueriesRequestElement, group bool, overrideOrderIndex *int, overrideOrderDirection *model.Direction, overrideLimit *int) (query string) {
	if group {
		query += " GROUP BY 1"
	}
	var orderIndex int
	if overrideOrderIndex != nil {
		orderIndex = *overrideOrderIndex
	} else if element.OrderColumnIndex != nil {
		orderIndex = *element.OrderColumnIndex
	} else {
		orderIndex = -1
	}
	var orderDirection model.Direction
	if overrideOrderDirection != nil {
		orderDirection = *overrideOrderDirection
	} else if element.OrderDirection != nil {
		orderDirection = *element.OrderDirection
	} else {
		orderDirection = model.Desc
	}

	if orderIndex != -1 {
		query += " ORDER BY " + strconv.Itoa(orderIndex+1) + " " + strings.ToUpper(string(orderDirection))
	}
	if overrideLimit != nil {
		query += " LIMIT " + strconv.Itoa(*overrideLimit)
	} else if element.Limit != nil {
		query += " LIMIT " + strconv.Itoa(*element.Limit)
	}
	return
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
