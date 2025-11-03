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
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

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

func (wrapper *Wrapper) GenerateQueries(elements []model.QueriesRequestElement, userId string, ownerUserIds []string, forceTz string, devices []models.Device) (queries []string, err error) {
	queries = make([]string, len(elements))
	for i, element := range elements {
		var timezone string
		if len(forceTz) > 0 {
			timezone = forceTz
		} else {
			if element.DeviceId != nil {
				timezone = getTZ(*element.DeviceId, devices, wrapper.config.DefaultTimezone)
			} else {
				timezone = wrapper.config.DefaultTimezone // no special tz support for exports
			}
		}
		table, err := wrapper.tableName(element, ownerUserIds[i], timezone)
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
			var l *int
			for idx, column := range element.Columns {
				query += "(SELECT time_bucket('" + *element.GroupTime + "', \"time\", '" + timezone + "') AS \"time\", "
				if strings.HasPrefix(*column.GroupType, "difference") {
					groupParts := strings.Split(*column.GroupType, "-")
					if groupParts[1] == "first" || groupParts[1] == "last" {
						query += groupParts[1] + "(\"" + column.Name + "\", \"time\")"
					} else {
						query += translateFunctionName(groupParts[1]) + "\"" + column.Name + "\")"
					}
					if element.Time != nil && (element.Time.Last != nil || element.Time.Ahead != nil) && !elementTimeLastAheadModified {
						// manually increase the last offset by 1 to ensure unified results
						copy := element.Time.Copy() // ensure no other elements are affected that share this pointer
						element.Time = &copy
						elements[i] = element
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
						l = &n
						num++
						if element.Time.Last != nil {
							modified := strconv.Itoa(num) + strings.TrimPrefix(*element.Time.Last, prefix)
							element.Time.Last = &modified
						} else {
							modified := strconv.Itoa(num) + strings.TrimPrefix(*element.Time.Ahead, prefix)
							element.Time.Ahead = &modified
						}
						elementTimeLastAheadModified = true
					} else if element.Time != nil && element.Time.Start != nil && element.Time.End != nil && element.GroupTime != nil && !elementTimeLastAheadModified {
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
						l = &diffT
						startS := start.Format(time.RFC3339)
						element.Time.Start = &startS
						endS := end.Format(time.RFC3339)
						element.Time.EndOriginal = element.Time.End
						element.Time.End = &endS
						elements[i] = element
					}
				} else if *column.GroupType == "first" || *column.GroupType == "last" {
					query += *column.GroupType + "(\"" + column.Name + "\", \"time\")"
				} else if strings.HasPrefix(*column.GroupType, "time-weighted-") {
					query += translateFunctionName(*column.GroupType) + "\"" + column.Name + "\"))"
				} else {
					query += translateFunctionName(*column.GroupType) + "\"" + column.Name + "\")"
				}
				query += " AS value"
				query += " FROM \"" + table + "\""
				filterString := ""
				if l != nil {
					n := *l
					n++
					filterString, err = getFilterString(element, true, &zero, &asc, &n)
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

			var limit *int
			var order *model.Direction
			var orderIndex *int
			if l != nil {
				limit = l
				desc := model.Desc
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
	if (element.Filters != nil && len(*element.Filters) > 0) || element.Time != nil {
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

func (wrapper *Wrapper) tableName(element model.QueriesRequestElement, userId string, timezone string) (table string, err error) {
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

func getCAQuery(element model.QueriesRequestElement, table string, timezone string) (string, error) {
	query := "SELECT view_name FROM (SELECT view_name, substring(view_definition, 'time_bucket\\((.*?)::interval, \"time\", ''" + timezone + "''')::interval as bucket FROM timescaledb_information.continuous_aggregates WHERE hypertable_name = '" + table + "' "

	for _, column := range element.Columns {
		if column.GroupType == nil {
			return table, errors.New("expected all columns to contain GroupType")
		}
		if *column.GroupType == "mean" {
			// not implemented
			return table, errors.New("")
		}
		query += "AND view_definition LIKE '%" + strings.ReplaceAll(translateFunctionName(*column.GroupType), "'", "''")
		containsDot := strings.Contains(column.Name, ".")
		if containsDot {
			query += "\"" + column.Name + "\""
		} else {
			query += column.Name
		}
		query += ", \"time\") AS "
		if containsDot {
			query += "\"" + column.Name + "\""
		} else {
			query += column.Name
		}
		query += "%'"
	}
	query += ") sub WHERE bucket <= '" + *element.GroupTime + "'::interval ORDER BY bucket DESC"
	query += " LIMIT 1;"

	return query, nil
}

func getTZ(deviceId string, devices []models.Device, defaultTZ string) string {
	for _, d := range devices {
		if d.Id == deviceId {
			for _, a := range d.Attributes {
				if strings.ToLower(a.Key) == "timezone" {
					return a.Value
				}
			}
			return defaultTZ
		}
	}
	return defaultTZ
}
