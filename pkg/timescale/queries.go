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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	util "github.com/SENERGY-Platform/timescale-tableworker/pkg/lib/handler"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/v5"
)

// quoteIdentifier renders name as a quoted SQL identifier, escaping any embedded quotes.
func quoteIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// hashedColumnName mirrors util.HashFieldNameIfNeeded, which quotes without escaping.
func quoteColumnName(name string) string {
	hashed := util.HashFieldNameIfNeeded(name)
	return quoteIdentifier(strings.TrimSuffix(strings.TrimPrefix(hashed, "\""), "\""))
}

// escapeSQLString escapes s for use inside a single quoted SQL string literal, without adding
// the surrounding quotes.
func escapeSQLString(s string) string {
	s = strings.ReplaceAll(s, string([]byte{0}), "")
	return strings.ReplaceAll(s, "'", "''")
}

// quoteSQLString renders s as a single quoted SQL string literal.
func quoteSQLString(s string) string {
	return "'" + escapeSQLString(s) + "'"
}

// filterValueLiteral renders a filter value as a SQL literal. Values may originate from a
// request body or from a previous query (see CreateFiltersForImport), so unknown types are
// rejected rather than printed with %v.
func filterValueLiteral(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return quoteSQLString(v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(v), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case json.Number:
		if _, err := v.Float64(); err != nil {
			return "", fmt.Errorf("invalid numeric filter value %v", v)
		}
		return v.String(), nil
	default:
		return "", fmt.Errorf("unsupported filter value type %T", value)
	}
}

func translateFunctionName(name string) string {
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
			query += "sub0.time AS \"time\", "
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
				query += "AS " + quoteIdentifier(column.Name)
			}
			query += " FROM "
			l, err := widenTimeWindowForDifference(&element)
			if err != nil {
				return nil, err
			}
			elements[i] = element
			for idx, column := range element.Columns {
				hashedColumnName := quoteColumnName(column.Name)
				query += "(SELECT time_bucket(" + quoteSQLString(*element.GroupTime) + ", \"time\", " + quoteSQLString(timezone) + ") AS \"time\", "
				if strings.HasPrefix(*column.GroupType, "difference") {
					groupParts := strings.Split(*column.GroupType, "-")
					if groupParts[1] == "first" || groupParts[1] == "last" {
						query += groupParts[1] + "(" + hashedColumnName + ", \"time\")"
					} else {
						query += translateFunctionName(groupParts[1]) + hashedColumnName + ")"
					}
				} else if *column.GroupType == "first" || *column.GroupType == "last" {
					query += *column.GroupType + "(" + hashedColumnName + ", \"time\")"
				} else if strings.HasPrefix(*column.GroupType, "time-weighted-") {
					query += translateFunctionName(*column.GroupType) + hashedColumnName + "))"
				} else {
					query += translateFunctionName(*column.GroupType) + hashedColumnName + ")"
				}
				query += " AS value"
				query += " FROM " + quoteIdentifier(table)
				filterString := ""
				if l != nil {
					n := *l
					n += 2
					filterString, err = getFilterString(element, true, &zero, &asc, &n)
				} else {
					dir := asc
					if column.GroupType != nil && *column.GroupType == "last" {
						dir = desc
					}
					filterString, err = getFilterString(element, true, &zero, &dir, nil)
				}
				if err != nil {
					return nil, err
				}
				query += filterString + ") sub" + strconv.Itoa(idx)
				if idx > 0 {
					query += " on sub0.time = sub" + strconv.Itoa(idx) + ".time"
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
				query += quoteColumnName(column.Name)
				if column.Math != nil {
					query += *column.Math
				}
				query += " AS " + quoteIdentifier(column.Name)
			}
			query += " FROM " + quoteIdentifier(table)
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
	if (element.Filters != nil && len(*element.Filters) > 0) || element.Time != nil {
		query += " WHERE "
	}
	if element.Filters != nil && len(*element.Filters) > 0 {
		for idx, filter := range *element.Filters {
			if idx != 0 {
				query += " AND "
			}
			if filter.Value == nil {
				query += quoteColumnName(filter.Column) + " IS "
				if filter.Type == "!=" {
					query += "NOT "
				}
				query += "NULL"
			} else {
				value, err := filterValueLiteral(filter.Value)
				if err != nil {
					return "", err
				}
				query += quoteColumnName(filter.Column)
				if filter.Math != nil {
					query += *filter.Math + " "
				}
				query += filter.Type
				query += " " + value
			}
		}
	}
	if element.Time != nil {
		if element.Filters != nil && len(*element.Filters) > 0 {
			query += " AND "
		}
		if element.Time.Last != nil {
			query += "\"time\" > now() - interval " + quoteSQLString(*element.Time.Last)
		} else if element.Time.Ahead != nil {
			query += "\"time\" > now() AND \"time\" < now() + interval " + quoteSQLString(*element.Time.Ahead)
		} else {
			query += "\"time\" > " + quoteSQLString(*element.Time.Start) + " AND \"time\" < " + quoteSQLString(*element.Time.End)
		}
	}
	query += getOrderLimitString(element, group, overrideSortIndex, overrideOrderDirection, overrideLimit)
	return
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

func (wrapper *Wrapper) tableName(ctx context.Context, element model.QueriesRequestElement, userId string, timezone string) (table string, err error) {
	if element.ExportId != nil {
		shortUserId, err := shortenId(userId)
		if err != nil {
			return "", err
		}
		shortExportId, err := shortenId(*element.ExportId)
		if err != nil {
			return "", err
		}
		table = "userid:" + shortUserId + "_" + "export:" + shortExportId
	} else {
		shortDeviceId, err := shortenId(*element.DeviceId)
		if err != nil {
			return "", err
		}
		shortServiceId, err := shortenId(*element.ServiceId)
		if err != nil {
			return "", err
		}
		table = "device:" + shortDeviceId + "_" + "service:" + shortServiceId
	}
	if element.GroupTime != nil && wrapper.pool != nil {
		// check if CA View available
		query, err := getCAQuery(element, table, timezone)
		if err != nil {
			log.Logger.WarnContext(ctx, "getCAQuery failed", "error", err)
			return table, nil
		}

		var caTable string
		if wrapper.config.Debug {
			log.Logger.DebugContext(ctx, "Checking for CA View with: "+query)
		}
		err = wrapper.pool.QueryRow(ctx, query).Scan(&caTable)
		if err == nil {
			return caTable, nil
		} else {
			return table, nil
		}
	}
	return table, nil

}

func shortenId(uuid string) (string, error) {
	parts := strings.Split(uuid, ":")
	noPrefix := parts[len(parts)-1]
	noPrefix = strings.ReplaceAll(noPrefix, "-", "")
	bytes, err := hex.DecodeString(noPrefix)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func getCAQuery(element model.QueriesRequestElement, table string, timezone string) (string, error) {
	// timezone, table and column names are embedded in string literals, so single quotes have
	// to be doubled
	query := "SELECT view_name FROM (SELECT view_name, substring(view_definition, 'time_bucket\\((.*?)::interval, \"time\", ''" + escapeSQLString(timezone) + "''')::interval as bucket FROM timescaledb_information.continuous_aggregates WHERE hypertable_name = " + quoteSQLString(table) + " "

	for _, column := range element.Columns {
		if column.GroupType == nil {
			return table, errors.New("expected all columns to contain GroupType")
		}
		if *column.GroupType == "mean" {
			// not implemented
			return table, errors.New("")
		}
		escapedName := escapeSQLString(column.Name)
		query += "AND view_definition LIKE '%" + strings.ReplaceAll(translateFunctionName(*column.GroupType), "'", "''")
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
	query += ") sub WHERE bucket <= " + quoteSQLString(*element.GroupTime) + "::interval ORDER BY bucket DESC"
	query += " LIMIT 1;"

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
