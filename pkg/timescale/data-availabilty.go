/*
 *    Copyright 2023 InfAI (CC SES)
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
	"regexp"
	"sync"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

const servicePrefix = "urn:infai:ses:service:"

var serviceRegex = regexp.MustCompile("device:.*_service:(.{22})")

// Postgres renders the view definition of a continuous aggregate like this, quoting column
// identifiers only where required:
//
//	SELECT time_bucket('1 day'::interval, "time", 'Europe/Berlin'::text) AS "time",
//	   last(lwt, "time") AS lwt,
//	   last("sensor.POWER", "time") AS "sensor.POWER"
//	  FROM "device:cFbkUpzDSoa8iYOCMVAYrw_service:8Poq-YNIREmO_5Gl8uebCA"
//	 GROUP BY (time_bucket('1 day'::interval, "time", 'Europe/Berlin'::text));
var intervalRegex = regexp.MustCompile(`time_bucket\('([^']*)'`)
var typeRegex = regexp.MustCompile(`(\w+)\((?:"(?:[^"]|"")*"|\w+), "time"\)`)

func (wrapper *Wrapper) GetDataAvailability(ctx context.Context, deviceId string) (res []model.DataAvailabilityResponseElement, err error) {
	shortDeviceId, err := shortenId(deviceId)
	if err != nil {
		return nil, err
	}
	tablePrefix := "device:" + shortDeviceId + "_"
	rows, err := wrapper.pool.Query(ctx, "SELECT view_name, view_definition FROM timescaledb_information.continuous_aggregates WHERE hypertable_name LIKE '"+tablePrefix+"%';")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mtx := sync.Mutex{}
	wg := sync.WaitGroup{}
	var anyErr error
	res = []model.DataAvailabilityResponseElement{}
	for rows.Next() {
		var viewName, viewDescription string
		err = rows.Scan(&viewName, &viewDescription)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			elem, err := wrapper.parseDataAvailability(ctx, viewName, &viewDescription)
			mtx.Lock()
			defer mtx.Unlock()
			if err != nil {
				if anyErr == nil {
					anyErr = err
				}
				return
			}
			res = append(res, *elem)
		}()
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	rows, err = wrapper.pool.Query(ctx, "SELECT table_name FROM information_schema.tables WHERE table_name ~ '"+tablePrefix+"service:.{22}$';")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var tableName string
		err = rows.Scan(&tableName)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			elem, err := wrapper.parseDataAvailability(ctx, tableName, nil)
			mtx.Lock()
			defer mtx.Unlock()
			if err != nil {
				if anyErr == nil {
					anyErr = err
				}
				return
			}
			res = append(res, *elem)
		}()
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	wg.Wait()
	if anyErr != nil {
		return nil, anyErr
	}
	return
}

func (wrapper *Wrapper) parseDataAvailability(ctx context.Context, viewTableName string, viewDescription *string) (*model.DataAvailabilityResponseElement, error) {
	serviceMatches := serviceRegex.FindStringSubmatch(viewTableName)
	if len(serviceMatches) < 2 {
		return nil, errors.New("unexpected service matches from view name")
	}
	longServiceId, err := models.LongId(serviceMatches[1])
	if err != nil {
		return nil, err
	}
	var groupType, groupTime *string
	if viewDescription != nil {
		parsedType, parsedTime, err := parseViewDescription(*viewDescription)
		if err != nil {
			return nil, err
		}
		groupType = &parsedType
		groupTime = &parsedTime
	}
	elem := model.DataAvailabilityResponseElement{
		ServiceId: servicePrefix + longServiceId,
		GroupType: groupType,
		GroupTime: groupTime,
	}

	quotedViewTableName := quoteIdentifier(viewTableName) // read from the database
	subRows, err := wrapper.pool.Query(ctx, fmt.Sprintf("(SELECT time from %s ORDER BY time ASC LIMIT 1) UNION ALL (SELECT time from %s ORDER BY time DESC LIMIT 1);", quotedViewTableName, quotedViewTableName))
	if err != nil {
		return nil, err
	}
	defer subRows.Close()

	i := 0
	for subRows.Next() {
		if i == 0 {
			i++
			err = subRows.Scan(&elem.From)
		} else {
			err = subRows.Scan(&elem.To)
		}
		if err != nil {
			return nil, err
		}
	}
	if err = subRows.Err(); err != nil {
		return nil, err
	}
	return &elem, nil
}

// parseViewDescription reads the aggregation function and the bucket interval of a continuous
// aggregate from its view definition.
func parseViewDescription(viewDescription string) (groupType string, groupTime string, err error) {
	intervalMatches := intervalRegex.FindStringSubmatch(viewDescription)
	if len(intervalMatches) < 2 {
		return "", "", errors.New("unexpected interval matches from view description")
	}
	typeMatches := typeRegex.FindStringSubmatch(viewDescription)
	if len(typeMatches) < 2 {
		return "", "", errors.New("unexpected type matches from view description")
	}
	return typeMatches[1], intervalMatches[1], nil
}
