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
	"fmt"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/jackc/pgx/v5/pgtype"
)

// GetDeviceUsage sums the usage of all service tables of each device.
func (wrapper *Wrapper) GetDeviceUsage(ctx context.Context, deviceIds []string) (res []model.Usage, err error) {
	query := fmt.Sprintf("SELECT substring(\"table\", 8, 22) as short_device_id, sum(bytes), min(updated_at), sum(bytes_per_day) FROM %v.usage WHERE substring(\"table\", 8, 22) = ANY($1) GROUP BY short_device_id", wrapper.config.PostgresUsageSchema)
	return wrapper.queryUsage(ctx, query, deviceIds, func(usage *model.Usage, longId string) {
		usage.DeviceId = "urn:infai:ses:device:" + longId
	})
}

// GetExportUsage returns the usage of the table of each export.
func (wrapper *Wrapper) GetExportUsage(ctx context.Context, exportIds []string) (res []model.Usage, err error) {
	query := fmt.Sprintf("SELECT substring(\"table\", 38, 60) as short_export_id, bytes, updated_at, bytes_per_day FROM %v.usage WHERE substring(\"table\", 38, 60) = ANY($1)", wrapper.config.PostgresUsageSchema)
	return wrapper.queryUsage(ctx, query, exportIds, func(usage *model.Usage, longId string) {
		usage.ExportId = longId
	})
}

// queryUsage runs query with the short forms of ids as its only parameter. Each row is a short id
// followed by bytes, updated at and bytes per day; setId stores the long form of the id.
func (wrapper *Wrapper) queryUsage(ctx context.Context, query string, ids []string, setId func(usage *model.Usage, longId string)) (res []model.Usage, err error) {
	res = []model.Usage{}
	if len(ids) == 0 {
		return res, nil
	}
	shortIds := make([]string, 0, len(ids))
	for _, id := range ids {
		shortId, err := models.ShortenId(id)
		if err != nil {
			return nil, err
		}
		shortIds = append(shortIds, shortId)
	}
	rows, err := wrapper.readPool.Query(ctx, query, shortIds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var usage model.Usage
		var shortId string
		var bytesPerDay pgtype.Float8
		if err = rows.Scan(&shortId, &usage.Bytes, &usage.UpdatedAt, &bytesPerDay); err != nil {
			return nil, err
		}
		usage.BytesPerDay = bytesPerDay.Float64
		longId, err := models.LongId(shortId)
		if err != nil {
			return nil, err
		}
		setId(&usage, longId)
		res = append(res, usage)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}
