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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SENERGY-Platform/device-repository/lib/api"
	drmodel "github.com/SENERGY-Platform/device-repository/lib/model"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	dsmodel "github.com/SENERGY-Platform/device-selection/pkg/model"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

type RemoteCache struct {
	mc              *memcache.Client
	config          configuration.Config
	deviceRepo      api.Controller
	deviceSelection deviceSelection.Client
	tracer          trace.Tracer
}

var NotCachableError = errors.New("not cachable")

type Entry struct {
	Time  time.Time              `json:"time"`
	Value map[string]interface{} `json:"value"`
}

func NewRemote(config configuration.Config, deviceRepo api.Controller, deviceSelection deviceSelection.Client) *RemoteCache {
	rc := &RemoteCache{config: config, deviceRepo: deviceRepo, deviceSelection: deviceSelection, tracer: otel.Tracer("cache")}
	rc.initMemcached(context.Background())
	return rc
}

func (lv *RemoteCache) initMemcached(ctx context.Context) {
	_, span := lv.tracer.Start(ctx, "RemoteCache.initMemcached")
	defer span.End()
	log.Logger.Info("(Re-)init memcached Client")
	lv.mc = memcache.New(lv.config.MemcachedUrls...)
}

func (lv *RemoteCache) GetLastValuesFromCache(ctx context.Context, request model.QueriesRequestElement, forceTZ *string) ([][]interface{}, error) {
	ctx, span := lv.tracer.Start(ctx, "RemoteCache.GetLastValuesFromCache")
	defer span.End()
	if request.DeviceId == nil || request.ServiceId == nil || request.Limit == nil || *request.Limit != 1 ||
		request.Time != nil || request.GroupTime != nil || request.Filters != nil || request.DeviceGroupId != nil || forceTZ != nil {
		return nil, NotCachableError
	}

	for _, col := range request.Columns {
		if col.Math != nil || col.GroupType != nil {
			return nil, NotCachableError
		}
	}

	key := "device_" + *request.DeviceId + "_service_" + *request.ServiceId
	item, err := lv.mcGet(ctx, key)
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

func (lv *RemoteCache) GetLastMessageFromCache(ctx context.Context, deviceId string, serviceId string) (entry Entry, err error) {
	ctx, span := lv.tracer.Start(ctx, "RemoteCache.GetLastMessageFromCache")
	defer span.End()
	key := "device_" + deviceId + "_service_" + serviceId
	item, err := lv.mcGet(ctx, key)
	if err != nil {
		return entry, err
	}
	err = json.Unmarshal(item.Value, &entry)
	if err != nil {
		return entry, err
	}
	return
}

func (this *RemoteCache) GetService(ctx context.Context, serviceId string) (service models.Service, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetService")
	defer span.End()
	cachedItem, err := this.mcGet(ctx, "service_"+serviceId)
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
		this.mcSet(ctx, &memcache.Item{
			Key:        "service_" + service.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
	}
	return service, err
}

func (this *RemoteCache) GetConcept(ctx context.Context, conceptId string) (concept models.Concept, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetConcept")
	defer span.End()
	cachedItem, err := this.mcGet(ctx, "concept_"+conceptId)
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
		this.mcSet(ctx, &memcache.Item{
			Key:        "concept_" + concept.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
	}
	return
}

func (this *RemoteCache) StoreSecretQuery(ctx context.Context, query model.PreparedQueriesRequestElement) (secret string, err error) {
	_, span := this.tracer.Start(ctx, "RemoteCache.StoreSecretQuery")
	defer span.End()
	bytes, err := json.Marshal(query)
	if err != nil {
		return "", err
	}
	uid := uuid.NewString()
	err = this.mc.Set(&memcache.Item{ //not using mcSet to ensure error propagates
		Key:        "secretquery_" + uid,
		Value:      bytes,
		Expiration: 30,
	})
	return uid, err
}

func (this *RemoteCache) GetSecretQuery(ctx context.Context, secret string) (query model.PreparedQueriesRequestElement, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetSecretQuery")
	defer span.End()
	query = model.PreparedQueriesRequestElement{}
	item, err := this.mcGet(ctx, "secretquery_"+secret)
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

func (this *RemoteCache) GetDeviceGroup(ctx context.Context, deviceGroupId string, token string) (deviceGroup models.DeviceGroup, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetDeviceGroup")
	defer span.End()
	cachedItem, err := this.mcGet(ctx, "device_group_"+deviceGroupId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &deviceGroup)
		if err != nil {
			return
		}
	} else {
		deviceGroup, err, _ = this.deviceRepo.ReadDeviceGroup(deviceGroupId, token, false)
		if err != nil {
			return
		}
		bytes, err := json.Marshal(deviceGroup)
		if err != nil {
			return deviceGroup, err
		}
		this.mcSet(ctx, &memcache.Item{
			Key:        "device_group_" + deviceGroup.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
	}
	return
}

func (this *RemoteCache) GetDevice(ctx context.Context, deviceId string, token string) (device models.Device, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetDevice")
	defer span.End()
	cachedItem, err := this.mcGet(ctx, "device_"+deviceId)
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
		this.mcSet(ctx, &memcache.Item{
			Key:        "device_" + device.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
	}
	return device, err
}

func (this *RemoteCache) GetFunction(ctx context.Context, functionId string) (function models.Function, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetFunction")
	defer span.End()
	cachedItem, err := this.mcGet(ctx, "function_"+functionId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &function)
		if err != nil {
			return
		}
	} else {
		function, err, _ = this.deviceRepo.GetFunction(functionId)
		if err != nil {
			return
		}
		bytes, err := json.Marshal(function)
		if err != nil {
			return function, err
		}
		this.mcSet(ctx, &memcache.Item{
			Key:        "function_" + function.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
	}
	return
}

func (this *RemoteCache) GetLocation(ctx context.Context, locationId string, token string) (location models.Location, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetLocation")
	defer span.End()
	cachedItem, err := this.mcGet(ctx, "location_"+locationId)
	if err == nil {
		err = json.Unmarshal(cachedItem.Value, &location)
		if err != nil {
			return
		}
	} else {
		location, err, _ = this.deviceRepo.GetLocation(locationId, token)
		if err != nil {
			return
		}
		bytes, err := json.Marshal(location)
		if err != nil {
			return location, err
		}
		this.mcSet(ctx, &memcache.Item{
			Key:        "location_" + location.Id,
			Value:      bytes,
			Expiration: 5 * 60,
		})
	}
	return
}

func (this *RemoteCache) GetSelectables(ctx context.Context, userid string, token string, criteria []models.DeviceGroupFilterCriteria, options *deviceSelection.GetSelectablesOptions) (res []dsmodel.Selectable, code int, err error) {
	ctx, span := this.tracer.Start(ctx, "RemoteCache.GetSelectables")
	defer span.End()
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

	optionsBytes, err := json.Marshal(options)
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
	cachedItem, err := this.mcGet(ctx, key)
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
		this.mcSet(ctx, &memcache.Item{
			Key:        key,
			Value:      bytes,
			Expiration: 5 * 60,
		})
	}
	return
}

func (rc *RemoteCache) mcSet(ctx context.Context, item *memcache.Item) {
	ctx, span := rc.tracer.Start(ctx, "RemoteCache.mcSet")
	defer span.End()
	err := rc.mc.Set(item)
	if err != nil {
		rc.initMemcached(ctx)
		err := rc.mc.Set(item)
		if err != nil {
			log.Logger.Warn("mc set failed", attributes.ErrorKey, err)
		}
	}
}

func (rc *RemoteCache) mcGet(ctx context.Context, key string) (item *memcache.Item, err error) {
	ctx, span := rc.tracer.Start(ctx, "RemoteCache.mcGet")
	defer span.End()
	item, err = rc.mc.Get(key)
	if err != nil && err != memcache.ErrCacheMiss && err != memcache.ErrCASConflict && err != memcache.ErrNotStored && err != memcache.ErrServerError && err != memcache.ErrNoStats && err != memcache.ErrMalformedKey {
		rc.initMemcached(ctx)
		item, err = rc.mc.Get(key)
		if err != nil {
			log.Logger.Warn("mc get failed", attributes.ErrorKey, err)
		}
	}
	return
}

func getDeepEntry(m map[string]interface{}, path string) interface{} {
	pathElems := strings.Split(path, ".")
	var sub interface{}
	sub = m
	ok := false
	for _, elem := range pathElems {
		switch child := sub.(type) {
		case map[string]interface{}:
			sub, ok = child[elem]
			if !ok {
				return nil
			}
		case []interface{}:
			n, err := strconv.Atoi(elem)
			if err != nil {
				log.Logger.Warn(fmt.Sprintf("Could not extract index of list with index %#v", elem))
				return nil
			}
			sub = child[n]
		}
	}
	return sub
}
