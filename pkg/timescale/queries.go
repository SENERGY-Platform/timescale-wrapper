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

func GenerateQueries(elements []model.QueriesRequestElement, timeDirection model.Direction) (queries []string, err error) {
	queries = make([]string, len(elements))
	for i, element := range elements {
		query := ""
		query += "SELECT "
		if element.GroupTime != nil {
			query += "time_bucket('" + *element.GroupTime + "', \"time\") AS \"time\", "
		} else {
			query += "\"time\", "
		}
		for idx, column := range element.Columns {
			if idx > 0 {
				query += ", "
			}
			if column.GroupType != nil {
				if strings.HasPrefix(*column.GroupType, "difference") {
					groupParts := strings.Split(*column.GroupType, "-")
					if groupParts[1] == "first" || groupParts[1] == "last" {
						query += "(lead(" + groupParts[1] + "(\"" + column.Name + "\", \"time\"), 1) OVER (PARTITION BY 1 ORDER BY 1 ASC) - " +
							groupParts[1] + "(\"" + column.Name + "\", \"time\")) * -1"
					} else {
						query += "(lead(" + translateFunctionName(groupParts[1]) + "\"" + column.Name + "\"), 1) OVER (PARTITION BY 1 ORDER BY 1 ASC) - " +
							translateFunctionName(groupParts[1]) + "\"" + column.Name + "\")) * -1"
					}
				} else if *column.GroupType == "first" || *column.GroupType == "last" {
					query += *column.GroupType + "(\"" + column.Name + "\", \"time\")"
				} else {
					query += translateFunctionName(*column.GroupType) + "\"" + column.Name + "\")"
				}
			} else {
				query += "\"" + column.Name + "\""
			}
			if column.Math != nil {
				query += *column.Math
			}
			query += " AS \"" + column.Name + "\""
		}
		table, err := tableName(element)
		if err != nil {
			return queries, err
		}
		query += " FROM \"" + table + "\""
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
						return queries, err
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
		if element.GroupTime != nil {
			query += " GROUP BY 1"
		}
		query += " ORDER BY 1 " + strings.ToUpper(string(timeDirection))
		if element.Limit != nil {
			query += " LIMIT " + strconv.Itoa(*element.Limit)
		}
		queries[i] = query
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
