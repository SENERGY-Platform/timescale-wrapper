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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/meta"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/bradfitz/gomemcache/memcache"
	"log"
	"strings"
	"time"
)

type RemoteCache struct {
	mc     *memcache.Client
	config configuration.Config
}

var NotCachableError = errors.New("not cachable")

type Entry struct {
	Time  time.Time              `json:"time"`
	Value map[string]interface{} `json:"value"`
}

func NewRemote(config configuration.Config) *RemoteCache {
	return &RemoteCache{mc: memcache.New(config.MemcachedUrls...), config: config}
}

func (lv *RemoteCache) GetLastValuesFromCache(request model.QueriesRequestElement) ([][]interface{}, error) {
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

func (this *RemoteCache) GetService(serviceId string) (service meta.Service, err error) {
	cachedItem, err := this.mc.Get("service_" + serviceId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &service)
		if err != nil {
			return service, err
		}
	} else {
		service, err = meta.GetService(serviceId, this.config.DeviceRepoUrl)
		if err != nil {
			return service, err
		}
		bytes, err := json.Marshal(service)
		if err != nil {
			return service, err
		}
		err = this.mc.Set(&memcache.Item{
			Key:        "service_" + service.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
		if err != nil {
			log.Println("WARNING: " + err.Error())
			err = nil
		}
	}
	return service, err
}

func (this *RemoteCache) GetConcept(conceptId string) (concept meta.Concept, err error) {
	cachedItem, err := this.mc.Get("concept_" + conceptId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &concept)
		if err != nil {
			return
		}
	} else {
		concept, err = meta.GetConcept(conceptId, this.config.DeviceRepoUrl)
		if err != nil {
			return
		}
		bytes, err := json.Marshal(concept)
		if err != nil {
			return concept, err
		}
		err = this.mc.Set(&memcache.Item{
			Key:        "concept_" + concept.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
		if err != nil {
			log.Println("WARNING: " + err.Error())
			err = nil
		}
	}
	return
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
