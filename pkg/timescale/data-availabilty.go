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
	"errors"
	"fmt"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"regexp"
	"sync"
)

const servicePrefix = "urn:infai:ses:service:"

var serviceRegex = regexp.MustCompile("device:.*_service:(.{22})")
var intervalRegex = regexp.MustCompile("time_bucket\\('(.*)'")
var typeRegex = regexp.MustCompile("(\\S*)\\(\"")

func (wrapper *Wrapper) GetDataAvailability(deviceId string) (res []model.DataAvailabilityResponseElement, err error) {
	shortDeviceId, err := shortenId(deviceId)
	if err != nil {
		return nil, err
	}
	tablePrefix := "device:" + shortDeviceId + "_"
	if err != nil {
		return nil, err
	}
	rows, err := wrapper.pool.Query("SELECT view_name, view_definition FROM timescaledb_information.continuous_aggregates WHERE hypertable_name LIKE '" + tablePrefix + "%';")
	if err != nil {
		return nil, err
	}
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

			elem, err := wrapper.parseDataAvailability(viewName, &viewDescription)
			if err != nil {
				anyErr = err
				return
			}
			mtx.Lock()
			res = append(res, *elem)
			mtx.Unlock()
		}()
	}

	rows, err = wrapper.pool.Query("SELECT table_name FROM information_schema.tables WHERE table_name ~ '" + tablePrefix + "service:.{22}$';")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tableName string
		err = rows.Scan(&tableName)
		if err != nil {
			return nil, err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			elem, err := wrapper.parseDataAvailability(tableName, nil)
			if err != nil {
				anyErr = err
				return
			}
			mtx.Lock()
			res = append(res, *elem)
			mtx.Unlock()
		}()
	}
	wg.Wait()
	if anyErr != nil {
		return nil, anyErr
	}
	return
}

func (wrapper *Wrapper) parseDataAvailability(viewTableName string, viewDescription *string) (*model.DataAvailabilityResponseElement, error) {
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
		intervalMatches := intervalRegex.FindStringSubmatch(*viewDescription)
		if len(intervalMatches) < 2 {
			return nil, errors.New("unexpected interval matches from view description")
		}
		typeMatches := typeRegex.FindStringSubmatch(*viewDescription)
		if len(typeMatches) < 2 {
			return nil, errors.New("unexpected type matches from view description")
		}
		groupType = &typeMatches[1]
		groupTime = &intervalMatches[1]
	}
	elem := model.DataAvailabilityResponseElement{
		ServiceId: servicePrefix + longServiceId,
		GroupType: groupType,
		GroupTime: groupTime,
	}

	subRows, err := wrapper.pool.Query(fmt.Sprintf("(SELECT time from \"%s\" ORDER BY time ASC LIMIT 1) UNION ALL (SELECT time from \"%s\" ORDER BY time DESC LIMIT 1);", viewTableName, viewTableName))
	if err != nil {
		return nil, err
	}

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
	return &elem, nil
}
