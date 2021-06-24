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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/api/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/util"
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
		return name + "("
	}
}

func GenerateQueries(elements []model.QueriesRequestElement) (queries []string, err error) {
	queries = make([]string, len(elements))
	for i, element := range elements {
		table, err := tableName(element)
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
			for idx, column := range element.Columns {
				query += "(SELECT time_bucket('" + *element.GroupTime + "', \"time\") AS \"time\", "
				if strings.HasPrefix(*column.GroupType, "difference") {
					groupParts := strings.Split(*column.GroupType, "-")
					if groupParts[1] == "first" || groupParts[1] == "last" {
						query += groupParts[1] + "(\"" + column.Name + "\", \"time\")"
					} else {
						query += translateFunctionName(groupParts[1]) + "\"" + column.Name + "\")"
					}
				} else if *column.GroupType == "first" || *column.GroupType == "last" {
					query += *column.GroupType + "(\"" + column.Name + "\", \"time\")"
				} else {
					query += translateFunctionName(*column.GroupType) + "\"" + column.Name + "\")"
				}
				query += " AS value"
				query += " FROM \"" + table + "\""
				filterString, err := getFilterString(element, true, &zero, &asc)
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

			query += getOrderLimitString(element, false, nil, nil)

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
			filterString, err := getFilterString(element, false, nil, nil)
			if err != nil {
				return nil, err
			}
			query += filterString
		}
		queries[i] = query
	}
	return
}

func getFilterString(element model.QueriesRequestElement, group bool, overrideSortIndex *int, overrideOrderDirection *model.Direction) (query string, err error) {
	if element.Filters != nil || element.Time != nil {
		query += " WHERE "
	}
	if element.Filters != nil {
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
				value, err := util.String(filter.Value)
				if err != nil {
					return "", err
				}
				query += " " + value
			}
		}
	}
	if element.Time != nil {
		if element.Filters != nil {
			query += " AND "
		}
		if element.Time.Last != nil {
			query += "\"time\" > now() - interval '" + *element.Time.Last + "'"
		} else {
			query += "\"time\" > '" + *element.Time.Start + "' AND \"time\" < '" + *element.Time.End + "'"
		}
	}
	query += getOrderLimitString(element, group, overrideSortIndex, overrideOrderDirection)
	return
}

func getOrderLimitString(element model.QueriesRequestElement, group bool, overrideOrderIndex *int, overrideOrderDirection *model.Direction) (query string) {
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
	if element.Limit != nil {
		query += " LIMIT " + strconv.Itoa(*element.Limit)
	}
	return
}

func tableName(element model.QueriesRequestElement) (string, error) {
	if element.ExportId != nil {
		return "", errors.New("exports are not supported yet")
	}
	shortDeviceId, err := shortenId(*element.DeviceId)
	if err != nil {
		return "", err
	}
	shortServiceId, err := shortenId(*element.ServiceId)
	if err != nil {
		return "", err
	}
	return "device:" + shortDeviceId + "_" + "service:" + shortServiceId, nil

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
