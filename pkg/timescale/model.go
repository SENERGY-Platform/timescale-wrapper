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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/jackc/pgx"
)

type Wrapper struct {
	config configuration.Config
	pool   *pgx.ConnPool
}

type RequestElement struct {
	Measurement string  `json:"measurement"`
	ColumnName  string  `json:"columnName"`
	Math        *string `json:"math"`
}

type uniqueMeasurementsColumns struct {
	Measurements map[string]struct{}            //Keys of the map are all requested measurements
	Columns      map[string]map[string]struct{} //Keys of the outer map are all requested columns.
	// Keys of the inner maps are all requested math operations. Empty string is no math operation
}
