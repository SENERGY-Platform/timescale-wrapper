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
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/SENERGY-Platform/device-repository/lib/api"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/bradfitz/gomemcache/memcache"
)

// newRemoteCacheForTest connects to the memcached named by TEST_MEMCACHED_URL. The returned prefix is
// unique to this test run, so keys left in memcached by earlier runs do not interfere.
func newRemoteCacheForTest(t *testing.T, repo api.Controller) (*RemoteCache, string) {
	t.Helper()
	url := os.Getenv("TEST_MEMCACHED_URL")
	if url == "" {
		t.Skip("TEST_MEMCACHED_URL not set")
	}
	log.InitForTest()
	rc := NewRemote(&configuration.ConfigStruct{MemcachedUrls: []string{url}}, repo, nil)
	return rc, fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
}

// fakeServiceRepo implements the upstream GetService call. Other api.Controller methods are not set
// and panic when called.
type fakeServiceRepo struct {
	api.Controller
	service models.Service
	err     error
	calls   int
}

func (f *fakeServiceRepo) GetService(id string) (models.Service, error, int) {
	f.calls++
	return f.service, f.err, 0
}

func TestGetOrFetchStoresAndHits(t *testing.T) {
	rc, prefix := newRemoteCacheForTest(t, nil)
	key := prefix + "_key"
	calls := 0

	got, err := getOrFetch(context.Background(), rc, key, func() (string, error) {
		calls++
		return "value", nil
	})
	if err != nil || got != "value" || calls != 1 {
		t.Fatalf("first call: got %q, err %v, fetches %d", got, err, calls)
	}
	item, err := rc.mc.Load().Get(key)
	if err != nil {
		t.Fatalf("value not stored under key: %v", err)
	}
	if string(item.Value) != `"value"` {
		t.Errorf("stored %s, expected %s", item.Value, `"value"`)
	}

	got, err = getOrFetch(context.Background(), rc, key, func() (string, error) {
		calls++
		return "", errors.New("upstream down")
	})
	if err != nil || got != "value" || calls != 1 {
		t.Errorf("second call must hit the cache: got %q, err %v, fetches %d", got, err, calls)
	}
}

func TestGetOrFetchErrorIsNotStored(t *testing.T) {
	rc, prefix := newRemoteCacheForTest(t, nil)
	key := prefix + "_key"
	fetchErr := errors.New("upstream down")

	_, err := getOrFetch(context.Background(), rc, key, func() (string, error) {
		return "", fetchErr
	})
	if !errors.Is(err, fetchErr) {
		t.Fatalf("expected fetch error, got %v", err)
	}
	if _, err := rc.mc.Load().Get(key); err != memcache.ErrCacheMiss {
		t.Errorf("failed fetch must not store anything, memcached returned %v", err)
	}
}

// The object returned upstream carries its own id. The cache entry must still be stored under the
// requested id, otherwise the next call misses.
func TestGetServiceHitsWhenReturnedIdDiffers(t *testing.T) {
	returnedId := fmt.Sprintf("%s-returned-%d", t.Name(), time.Now().UnixNano())
	repo := &fakeServiceRepo{service: models.Service{Id: returnedId}}
	rc, prefix := newRemoteCacheForTest(t, repo)
	requestedId := prefix + "_requested"

	service, err := rc.GetService(context.Background(), requestedId)
	if err != nil || service.Id != returnedId || repo.calls != 1 {
		t.Fatalf("first call: id %q, err %v, fetches %d", service.Id, err, repo.calls)
	}
	if _, err := rc.mc.Load().Get("service_" + requestedId); err != nil {
		t.Fatalf("entry not stored under requested id: %v", err)
	}
	if _, err := rc.mc.Load().Get("service_" + returnedId); err != memcache.ErrCacheMiss {
		t.Errorf("entry must not be stored under the returned id, memcached returned %v", err)
	}

	repo.err = errors.New("upstream down")
	repo.calls = 0
	service, err = rc.GetService(context.Background(), requestedId)
	if err != nil || service.Id != returnedId || repo.calls != 0 {
		t.Errorf("second call must hit the cache: id %q, err %v, fetches %d", service.Id, err, repo.calls)
	}
}
