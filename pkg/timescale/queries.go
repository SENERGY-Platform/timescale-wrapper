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
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"log"
	"regexp"
	"strconv"
	"strings"
)

func translateFunctionName(name string) string {
	switch name {
	case "mean":
		return "avg("
	case "median":
		return "percentile_disc(0.5) WITHIN GROUP (ORDER BY "
	default:
		if strings.HasPrefix(name, "difference") {
			parts := strings.Split(name, "-")
			return parts[1] + "("
		}
		return name + "("
	}
}

func (wrapper *Wrapper) GenerateQueries(elements []model.QueriesRequestElement, userId string) (queries []string, err error) {
	queries = make([]string, len(elements))
	for i, element := range elements {
		table, err := wrapper.tableName(element, userId)
		if err != nil {
			return queries, err
		}

		query := ""
		query += "SELECT "
		if element.GroupTime != nil {
			zero := 0
			asc := model.Asc
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
					query += " - lag(sub" + strconv.Itoa(idx) + ".value) OVER (ORDER BY 1)"
				}
				query += ") "
				if column.Math != nil {
					query += *column.Math + " "
				}
				query += "AS \"" + column.Name + "\""
			}
			query += " FROM "
			elementTimeLastAheadModified := false
			for idx, column := range element.Columns {
				query += "(SELECT time_bucket('" + *element.GroupTime + "', \"time\") AS \"time\", "
				if strings.HasPrefix(*column.GroupType, "difference") {
					groupParts := strings.Split(*column.GroupType, "-")
					if groupParts[1] == "first" || groupParts[1] == "last" {
						query += groupParts[1] + "(\"" + column.Name + "\", \"time\")"
					} else {
						query += translateFunctionName(groupParts[1]) + "\"" + column.Name + "\")"
					}
					if (element.Time.Last != nil || element.Time.Ahead != nil) && !elementTimeLastAheadModified {
						// manually increase the last offset by 1 to ensure unified results
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
						num++
						if element.Time.Last != nil {
							modified := strconv.Itoa(num) + strings.TrimPrefix(*element.Time.Last, prefix)
							element.Time.Last = &modified
						} else {
							modified := strconv.Itoa(num) + strings.TrimPrefix(*element.Time.Ahead, prefix)
							element.Time.Ahead = &modified
						}
						elementTimeLastAheadModified = true
					}
				} else if *column.GroupType == "first" || *column.GroupType == "last" {
					query += *column.GroupType + "(\"" + column.Name + "\", \"time\")"
				} else {
					query += translateFunctionName(*column.GroupType) + "\"" + column.Name + "\")"
				}
				query += " AS value"
				query += " FROM \"" + table + "\""
				filterString := ""
				if element.Limit != nil {
					l := *element.Limit + 1
					filterString, err = getFilterString(element, true, &zero, &asc, &l)
				} else {
					filterString, err = getFilterString(element, true, &zero, &asc, nil)
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

			query += getOrderLimitString(element, false, nil, nil, nil)

		} else {
			query += "\"time\", "
			for idx, column := range element.Columns {
				if column.GroupType != nil {
					return nil, errors.New("mixing aggregate and non-aggregate queries is not supported\n")
				}
				if idx > 0 {
					query += ", "
				}
				query += "\"" + column.Name + "\""
				if column.Math != nil {
					query += *column.Math
				}
				query += " AS \"" + column.Name + "\""
			}
			query += " FROM \"" + table + "\""
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

func getFilterString(element model.QueriesRequestElement, group bool, overrideSortIndex *int, overrideOrderDirection *model.Direction, overrideLimit *int) (query string, err error) {
	if element.Filters != nil || element.Time != nil {
		query += " WHERE "
	}
	if element.Filters != nil && len(*element.Filters) > 0 {
		for idx, filter := range *element.Filters {
			if idx != 0 {
				query += " AND "
			}
			_, valueIsString := filter.Value.(string)
			query += "\"" + filter.Column + "\" "
			if filter.Math != nil {
				query += *filter.Math + " "
			}
			query += filter.Type
			if valueIsString {
				query += " '" + filter.Value.(string) + "'"
			} else {
				value := fmt.Sprintf("%v", filter.Value)
				query += " " + value
			}
		}
	}
	if element.Time != nil {
		if element.Filters != nil && len(*element.Filters) > 0 {
			query += " AND "
		}
		if element.Time.Last != nil {
			query += "\"time\" > now() - interval '" + *element.Time.Last + "'"
		} else if element.Time.Ahead != nil {
			query += "\"time\" > now() AND \"time\" < now() + interval '" + *element.Time.Ahead + "'"
		} else {
			query += "\"time\" > '" + *element.Time.Start + "' AND \"time\" < '" + *element.Time.End + "'"
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
		orderIndex = 0
	}
	var orderDirection model.Direction
	if overrideOrderDirection != nil {
		orderDirection = *overrideOrderDirection
	} else if element.OrderDirection != nil {
		orderDirection = *element.OrderDirection
	} else {
		orderDirection = model.Desc
	}

	query += " ORDER BY " + strconv.Itoa(orderIndex+1) + " " + strings.ToUpper(string(orderDirection))
	if overrideLimit != nil {
		query += " LIMIT " + strconv.Itoa(*overrideLimit)
	} else if element.Limit != nil {
		query += " LIMIT " + strconv.Itoa(*element.Limit)
	}
	return
}

func (wrapper *Wrapper) tableName(element model.QueriesRequestElement, userId string) (table string, err error) {
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
		query, err := getCAQuery(element, table)
		if err != nil {
			log.Println("WARN: getCAQuery: " + err.Error())
			return table, nil
		}

		var caTable string
		if wrapper.config.Debug {
			log.Println("DEBUG: Checking for CA View with: " + query)
		}
		err = wrapper.pool.QueryRow(query).Scan(&caTable)
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

func getCAQuery(element model.QueriesRequestElement, table string) (string, error) {
	query := "SELECT view_name FROM timescaledb_information.continuous_aggregates WHERE hypertable_name = '" + table +
		"' AND view_definition LIKE '%SELECT time_bucket(''' || (SELECT '" + *element.GroupTime + "'::interval) || '''::interval, \"" +
		table + "\".\"time\")%'\n"

	for _, column := range element.Columns {
		if column.GroupType == nil {
			return table, errors.New("expected all columns to contain GroupType")
		}
		if *column.GroupType == "mean" {
			// not implemented
			return table, errors.New("")
		}
		query += "AND view_definition LIKE '%" + translateFunctionName(*column.GroupType) + "\"" + table + "\"."
		containsDot := strings.Contains(column.Name, ".")
		if containsDot {
			query += "\"" + column.Name + "\""
		} else {
			query += column.Name
		}
		query += ", \"" + table + "\".\"time\") AS "
		if containsDot {
			query += "\"" + column.Name + "\""
		} else {
			query += column.Name
		}
		query += "%'\n"
	}
	query += "LIMIT 1;"

	return query, nil
}
