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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/SENERGY-Platform/device-repository/lib/api"
	drmodel "github.com/SENERGY-Platform/device-repository/lib/model"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	dsmodel "github.com/SENERGY-Platform/device-selection/pkg/model"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/google/uuid"
)

type RemoteCache struct {
	mc              *memcache.Client
	config          configuration.Config
	deviceRepo      api.Controller
	deviceSelection deviceSelection.Client
}

var NotCachableError = errors.New("not cachable")

type Entry struct {
	Time  time.Time              `json:"time"`
	Value map[string]interface{} `json:"value"`
}

func NewRemote(config configuration.Config, deviceRepo api.Controller, deviceSelection deviceSelection.Client) *RemoteCache {
	return &RemoteCache{mc: memcache.New(config.MemcachedUrls...), config: config, deviceRepo: deviceRepo, deviceSelection: deviceSelection}
}

func (lv *RemoteCache) GetLastValuesFromCache(request model.QueriesRequestElement) ([][]interface{}, error) {
	if request.DeviceId == nil || request.ServiceId == nil || request.Limit == nil || *request.Limit != 1 ||
		request.Time != nil || request.GroupTime != nil || request.Filters != nil || request.DeviceGroupId != nil {
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

func (this *RemoteCache) GetService(serviceId string) (service models.Service, err error) {
	cachedItem, err := this.mc.Get("service_" + serviceId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &service)
		if err != nil {
			return service, err
		}
	} else {
		service, err, _ = this.deviceRepo.GetService(serviceId)
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

func (this *RemoteCache) GetConcept(conceptId string) (concept models.Concept, err error) {
	cachedItem, err := this.mc.Get("concept_" + conceptId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &concept)
		if err != nil {
			return
		}
	} else {
		concept, err, _ = this.deviceRepo.GetConceptWithoutCharacteristics(conceptId)
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

func (this *RemoteCache) StoreSecretQuery(query model.PreparedQueriesRequestElement) (secret string, err error) {
	bytes, err := json.Marshal(query)
	if err != nil {
		return "", err
	}
	uid := uuid.NewString()
	err = this.mc.Set(&memcache.Item{
		Key:        "secretquery_" + uid,
		Value:      bytes,
		Expiration: 30,
	})
	return uid, err
}

func (this *RemoteCache) GetSecretQuery(secret string) (query model.PreparedQueriesRequestElement, err error) {
	query = model.PreparedQueriesRequestElement{}
	item, err := this.mc.Get("secretquery_" + secret)
	if err != nil {
		return query, err
	}
	err = this.mc.Delete("secretquery_" + secret)
	if err != nil {
		return query, err
	}
	err = json.Unmarshal(item.Value, &query)
	return query, err
}

func (this *RemoteCache) GetDeviceGroup(deviceGroupId string, token string) (deviceGroup models.DeviceGroup, err error) {
	cachedItem, err := this.mc.Get("device_group_" + deviceGroupId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &deviceGroup)
		if err != nil {
			return
		}
	} else {
		deviceGroup, err, _ = this.deviceRepo.ReadDeviceGroup(deviceGroupId, token)
		if err != nil {
			return
		}
		bytes, err := json.Marshal(deviceGroup)
		if err != nil {
			return deviceGroup, err
		}
		err = this.mc.Set(&memcache.Item{
			Key:        "device_group_" + deviceGroup.Id,
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

func (this *RemoteCache) GetDevice(deviceId string, token string) (device models.Device, err error) {
	cachedItem, err := this.mc.Get("device_" + deviceId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &device)
		if err != nil {
			return device, err
		}
	} else {
		device, err, _ = this.deviceRepo.ReadDevice(deviceId, token, drmodel.READ)
		if err != nil {
			return device, err
		}
		bytes, err := json.Marshal(device)
		if err != nil {
			return device, err
		}
		err = this.mc.Set(&memcache.Item{
			Key:        "device_" + device.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
		if err != nil {
			log.Println("WARNING: " + err.Error())
			err = nil
		}
	}
	return device, err
}

func (this *RemoteCache) GetFunction(functionId string) (concept models.Function, err error) {
	cachedItem, err := this.mc.Get("function_" + functionId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &concept)
		if err != nil {
			return
		}
	} else {
		concept, err, _ = this.deviceRepo.GetFunction(functionId)
		if err != nil {
			return
		}
		bytes, err := json.Marshal(concept)
		if err != nil {
			return concept, err
		}
		err = this.mc.Set(&memcache.Item{
			Key:        "function_" + concept.Id,
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

func (this *RemoteCache) GetSelectables(userid string, token string, criteria []models.DeviceGroupFilterCriteria, options *deviceSelection.GetSelectablesOptions) (res []dsmodel.Selectable, code int, err error) {
	hasher := sha256.New()
	criteriaBytes, err := json.Marshal(criteria)
	if err != nil {
		code = http.StatusInternalServerError
		return
	}
	_, err = hasher.Write(criteriaBytes)
	if err != nil {
		code = http.StatusInternalServerError
		return
	}

	optionsBytes, err := json.Marshal(criteria)
	if err != nil {
		code = http.StatusInternalServerError
		return
	}
	_, err = hasher.Write(optionsBytes)
	if err != nil {
		code = http.StatusInternalServerError
		return
	}

	key := "selectables_" + hex.EncodeToString(hasher.Sum(nil))
	cachedItem, err := this.mc.Get(key)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &res)
		if err != nil {
			return
		}
	} else {
		res, code, err = this.deviceSelection.GetSelectables(token, criteria, options)
		if err != nil {
			return
		}
		bytes, err := json.Marshal(res)
		if err != nil {
			return res, http.StatusInternalServerError, err
		}
		err = this.mc.Set(&memcache.Item{
			Key:        key,
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
