/*
 * Copyright 2022 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cache

import (
	"encoding/json"
	"errors"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/bradfitz/gomemcache/memcache"
	"strings"
	"time"
)

type LastValueCache struct {
	mc *memcache.Client
}

var NotCachableError = errors.New("not cachable")

type Entry struct {
	Time  time.Time              `json:"time"`
	Value map[string]interface{} `json:"value"`
}

func NewLastValueCache(config configuration.Config) *LastValueCache {
	return &LastValueCache{memcache.New(config.MemcachedUrls...)}
}

func (lv *LastValueCache) GetLastValuesFromCache(request model.QueriesRequestElement) ([][]interface{}, error) {
	if request.DeviceId == nil || request.ServiceId == nil || request.Limit == nil || *request.Limit != 1 ||
		request.Time != nil || request.GroupTime != nil || request.Filters != nil {
		return nil, NotCachableError
	}

	for _, col := range request.Columns {
		if col.Math != nil || col.GroupType != nil {
			return nil, NotCachableError
		}
	}

	key := "device_" + *request.DeviceId + "_service_" + *request.ServiceId
	item, err := lv.mc.Get(key)
	if err != nil {
		return nil, err
	}
	var entry Entry
	err = json.Unmarshal(item.Value, &entry)
	if err != nil {
		return nil, err
	}
	res := make([]interface{}, len(request.Columns)+1)
	res[0] = entry.Time
	for i := range request.Columns {
		res[i+1] = getDeepEntry(entry.Value, request.Columns[i].Name)
	}

	return [][]interface{}{res}, nil
}

func getDeepEntry(m map[string]interface{}, path string) interface{} {
	pathElems := strings.Split(path, ".")
	sub := m
	ok := false
	var res interface{}
	for i, elem := range pathElems {
		res, ok = sub[elem]
		if !ok {
			return nil
		}
		if i < len(pathElems)-1 {
			sub, ok = res.(map[string]interface{})
			if !ok {
				return nil
			}
		}
	}
	return res
}
