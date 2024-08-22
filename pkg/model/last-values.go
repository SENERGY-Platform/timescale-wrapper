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

package model

type LastValuesRequestElement struct {
	ExportId               *string `json:"exportId,omitempty"`
	DeviceId               *string `json:"deviceId,omitempty"`
	ServiceId              *string `json:"serviceId,omitempty"`
	Math                   *string `json:"math,omitempty"`
	SourceCharacteristicId *string `json:"sourceCharacteristicId,omitempty"`
	TargetCharacteristicId *string `json:"targetCharacteristicId,omitempty"`
	ConceptId              *string `json:"conceptId,omitempty"`
	ColumnName             string  `json:"columnName,omitempty"`
}

type LastValuesResponseElement struct {
	Time  *string     `json:"time"`
	Value interface{} `json:"value"`
}
