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
	"fmt"
	"strings"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/pgtype"
)

func (wrapper *Wrapper) GetDeviceUsage(deviceIds []string) (res []model.Usage, err error) {
	res = []model.Usage{}
	if len(deviceIds) == 0 {
		return res, err
	}
	shortDeviceIds := []string{}
	for _, deviceId := range deviceIds {
		shortId, err := models.ShortenId(deviceId)
		if err != nil {
			return nil, err
		}
		shortDeviceIds = append(shortDeviceIds, "'"+shortId+"'")
	}

	rows, err := wrapper.pool.Query(fmt.Sprintf("SELECT substring(\"table\", 8, 22) as short_device_id, sum(bytes), min(updated_at), sum(bytes_per_day) FROM %v.usage WHERE substring(\"table\", 8, 22) IN (%v) GROUP BY short_device_id", wrapper.config.PostgresUsageSchema, strings.Join(shortDeviceIds, ", ")))
	if err != nil {
		return nil, err
	}

	r := model.Usage{}
	var bytesPerDay pgtype.Float8
	for rows.Next() {
		err = rows.Scan(&r.DeviceId, &r.Bytes, &r.UpdatedAt, &bytesPerDay)
		if err != nil {
			return nil, err
		}
		r.BytesPerDay = bytesPerDay.Float
		r.DeviceId, err = models.LongId(r.DeviceId)
		if err != nil {
			return nil, err
		}
		r.DeviceId = "urn:infai:ses:device:" + r.DeviceId
		res = append(res, r)
	}

	return res, nil
}

func (wrapper *Wrapper) GetExportUsage(exportIds []string) (res []model.Usage, err error) {
	res = []model.Usage{}
	if len(exportIds) == 0 {
		return res, err
	}
	shortExportIds := []string{}
	for _, exportId := range exportIds {
		shortId, err := models.ShortenId(exportId)
		if err != nil {
			return nil, err
		}
		shortExportIds = append(shortExportIds, "'"+shortId+"'")
	}

	rows, err := wrapper.pool.Query(fmt.Sprintf("SELECT substring(\"table\", 38, 60) as short_export_id, bytes, updated_at, bytes_per_day FROM %v.usage WHERE substring(\"table\", 38, 60) IN (%v)", wrapper.config.PostgresUsageSchema, strings.Join(shortExportIds, ", ")))
	if err != nil {
		return nil, err
	}

	r := model.Usage{}
	var bytesPerDay pgtype.Float8
	for rows.Next() {
		err = rows.Scan(&r.ExportId, &r.Bytes, &r.UpdatedAt, &bytesPerDay)
		if err != nil {
			return nil, err
		}
		r.BytesPerDay = bytesPerDay.Float
		r.ExportId, err = models.LongId(r.ExportId)
		if err != nil {
			return nil, err
		}
		res = append(res, r)
	}

	return res, nil
}
