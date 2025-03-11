/*
 *    Copyright 2025 InfAI (CC SES)
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
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
)

func (wrapper *Wrapper) GetLastMessage(deviceId string, serviceId string, service models.Service) (entry cache.Entry, err error) {
	shortDeviceId, err := shortenId(deviceId)
	if err != nil {
		return entry, err
	}
	shortServiceId, err := shortenId(serviceId)
	if err != nil {
		return entry, err
	}
	var rawValues string
	err = wrapper.pool.QueryRow("select to_json(r) from (select * from \"device:" + shortDeviceId + "_service:" + shortServiceId + "\" order by time desc limit 1) r;").Scan(&rawValues)
	if err != nil {
		return entry, err
	}
	var jsonValues map[string]interface{}
	err = json.Unmarshal([]byte(rawValues), &jsonValues)
	if err != nil {
		return entry, err
	}
	ti, ok := jsonValues["time"]
	if !ok {
		return entry, fmt.Errorf("values are missing time %s", rawValues)
	}
	t, ok := ti.(string)
	if !ok {
		return entry, fmt.Errorf("field time is not string %v", ti)
	}
	entry.Time, err = time.Parse(time.RFC3339, t)
	if err != nil {
		return entry, err
	}
	entry.Value = map[string]interface{}{}
	for _, output := range service.Outputs {
		m, err := buildOutput(output.ContentVariable, jsonValues, "")
		if err != nil {
			return entry, err
		}
		entry.Value[output.ContentVariable.Name] = m[output.ContentVariable.Name]
	}
	return
}

func buildOutput(cv models.ContentVariable, jsonValues map[string]interface{}, path string) (result map[string]interface{}, err error) {
	result = map[string]interface{}{}
	if len(path) > 0 {
		path += "."
	}
	path += cv.Name
	switch cv.Type {
	case models.String, models.Boolean, models.Float, models.Integer:
		result[cv.Name] = jsonValues[path]
		return
	case models.Structure:
		m := map[string]interface{}{}
		for _, sub := range cv.SubContentVariables {
			subMap, err := buildOutput(sub, jsonValues, path)
			if err != nil {
				return nil, err
			}
			m[sub.Name] = subMap[sub.Name]
		}
		result[cv.Name] = m
		return
	case models.List:
		if len(cv.SubContentVariables) > 0 && cv.SubContentVariables[0].Name == "*" {
			result[cv.Name] = jsonValues[path]
			return
		}
		l := make([]interface{}, len(cv.SubContentVariables))
		for _, sub := range cv.SubContentVariables {
			i, err := strconv.Atoi(sub.Name)
			if err != nil {
				return nil, err
			}
			if i > len(l)-1 {
				return nil, fmt.Errorf("index too large %d", i)
			}
			subMap, err := buildOutput(sub, jsonValues, path)
			if err != nil {
				return nil, err
			}
			l[i] = subMap[sub.Name]
		}
		result[cv.Name] = l
		return
	default:
		return nil, fmt.Errorf("unsupported content variable type %s", cv.Type)
	}
}
