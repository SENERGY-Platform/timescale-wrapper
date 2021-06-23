/*
 *    Copyright 2020 InfAI (CC SES)
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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/jackc/pgx"
	"sync"
)

func NewWrapper(ctx context.Context, wg *sync.WaitGroup, config configuration.Config) (wrapper *Wrapper, err error) {
	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     config.PostgresHost,
			Port:     config.PostgresPort,
			Database: config.PostgresDb,
			User:     config.PostgresUser,
			Password: config.PostgresPw,
		},
		MaxConnections: 50,
		AcquireTimeout: 0,
	})
	if err != nil {
		return nil, err
	}
	wg.Add(1)
	go func() {
		<-ctx.Done()
		pool.Close()
		wg.Done()
	}()
	return &Wrapper{config: config, pool: pool}, nil
}

/* TODO
func (this *Wrapper) GetLatestValue(db string, pair RequestElement) (timeValuePair TimeValuePair, err error) {
	timeValuePairs, err := this.GetLatestValues(db, []RequestElement{pair})
	if err != nil {
		return timeValuePair, err
	}
	return timeValuePairs[0], err
}

func (this *Wrapper) GetLatestValues(db string, pairs []RequestElement) (timeValuePairs []TimeValuePair, err error) {
	set := transformMeasurementColumnPairs(pairs)

	query := generateQuery(set) + " ORDER BY \"time\" DESC LIMIT 1"
	responseP, err := this.ExecuteQuery(db, query)
	if err != nil {
		return timeValuePairs, err
	}

	if len(responseP.Results) != 1 {
		return timeValuePairs, ErrNULL
	}

	numExpectedColumns := 0
	for key := range set.Columns {
		numExpectedColumns += len(set.Columns[key])
	}

	for i := range responseP.Results[0].Series {
		if len(responseP.Results[0].Series[i].Values) != 1 || len(responseP.Results[0].Series[i].Values[0]) != numExpectedColumns {
			return timeValuePairs, ErrNULL
		}
	}

	for _, pair := range pairs {
		seriesIndex, err := findSeriesIndex(pair.Measurement, responseP.Results[0].Series)
		if err != nil {
			if err == ErrNotFound {
				timeValuePairs = append(timeValuePairs, TimeValuePair{
					Time:  nil,
					Value: nil,
				})
				continue
			}
			return timeValuePairs, err
		}
		columnIndex, err := findColumnIndex(getColumnName(pair), responseP.Results[0].Series[seriesIndex])
		if err != nil {
			if err == ErrNotFound {
				time := responseP.Results[0].Series[seriesIndex].Values[0][0].(string)
				timeValuePairs = append(timeValuePairs, TimeValuePair{
					Time:  &time,
					Value: nil,
				})
				continue
			}
			return timeValuePairs, err
		}

		time := responseP.Results[0].Series[seriesIndex].Values[0][0].(string)
		timeValuePairs = append(timeValuePairs, TimeValuePair{
			Time:  &time,
			Value: responseP.Results[0].Series[seriesIndex].Values[0][columnIndex],
		})
	}

	return
}

*/
