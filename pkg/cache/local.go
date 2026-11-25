/*
 * Copyright 2020 InfAI (CC SES)
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

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/coocood/freecache"
)

var LocalCacheSize = 10 * 1024 * 1024 // 10 0MB
var LocalCacheExpirationInSec = 600   // 10 min

type LocalCache struct {
	l1         *freecache.Cache
	expiration int
}

var ErrNotFound = errors.New("key not found in cache")

func NewLocal() *LocalCache {
	return &LocalCache{l1: freecache.NewCache(LocalCacheSize), expiration: LocalCacheExpirationInSec}
}

func (this *LocalCache) Get(key string) (value []byte, err error) {
	value, err = this.l1.Get([]byte(key))
	if err == freecache.ErrNotFound {
		err = ErrNotFound
	}
	return
}

func (this *LocalCache) Set(key string, value []byte) {
	err := this.l1.Set([]byte(key), value, this.expiration)
	if err != nil {
		log.Logger.Warn("err in LocalCache::l1.Set()", attributes.ErrorKey, err)
	}
	return
}

// UseLocal returns the value cached under key. On a miss it calls getter, stores the
// JSON encoding of its result and returns the getter's value as is. Errors from getter
// are returned and not cached.
func UseLocal[T any](c *LocalCache, key string, getter func() (T, error)) (T, error) {
	var result T
	value, err := c.Get(key)
	if err == nil {
		err = json.Unmarshal(value, &result)
		return result, err
	} else if err != ErrNotFound {
		log.Logger.Warn("err in LocalCache::l1.Get()", attributes.ErrorKey, err)
	}
	result, err = getter()
	if err != nil {
		var zero T
		return zero, err
	}
	value, err = json.Marshal(result)
	if err != nil {
		var zero T
		return zero, err
	}
	c.Set(key, value)
	return result, nil
}
