/*
 * Copyright 2021 InfAI (CC SES)
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

package verification

import (
	"context"

	serving "github.com/SENERGY-Platform/analytics-serving/client"
	permClient "github.com/SENERGY-Platform/permissions-v2/pkg/client"
	"golang.org/x/sync/errgroup"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// verifyConcurrency limits the upstream permission checks of one VerifyAccess call.
const verifyConcurrency = 10

type Verifier struct {
	c             *cache.LocalCache
	permClient    permClient.Client
	servingClient *serving.Client
}

type VerifierCacheEntry struct {
	Ok          bool
	OwnerUserId string
}

func New(config configuration.Config) *Verifier {
	return &Verifier{
		c:             cache.NewLocal(),
		permClient:    permClient.New(config.PermissionsUrl),
		servingClient: serving.New(config.ServingUrl),
	}
}

// resourceKind describes one kind of verifiable resource: the namespace in the cache key,
// the id the request element carries for it and the upstream check.
type resourceKind struct {
	name  string
	id    func(model.QueriesRequestElement) *string
	check func(v *Verifier, ctx context.Context, id string, token string, userId string) (VerifierCacheEntry, error)
}

// resourceKinds is ordered by precedence: if an element carries several ids, the first kind wins.
var resourceKinds = []resourceKind{
	{
		name: "export",
		id:   func(e model.QueriesRequestElement) *string { return e.ExportId },
		check: func(v *Verifier, ctx context.Context, id string, token string, userId string) (VerifierCacheEntry, error) {
			return v.VerifyExport(ctx, id, token, userId)
		},
	},
	{
		name: "device",
		id:   func(e model.QueriesRequestElement) *string { return e.DeviceId },
		check: func(v *Verifier, ctx context.Context, id string, token string, _ string) (VerifierCacheEntry, error) {
			return v.VerifyDevice(ctx, id, token)
		},
	},
	{
		name: "device-group",
		id:   func(e model.QueriesRequestElement) *string { return e.DeviceGroupId },
		check: func(v *Verifier, ctx context.Context, id string, token string, _ string) (VerifierCacheEntry, error) {
			return v.VerifyDeviceGroup(ctx, id, token)
		},
	},
	{
		name: "location",
		id:   func(e model.QueriesRequestElement) *string { return e.LocationId },
		check: func(v *Verifier, ctx context.Context, id string, token string, _ string) (VerifierCacheEntry, error) {
			return v.VerifyLocation(ctx, id, token)
		},
	},
}

// resourceOf returns the kind and id an element is verified by. found is false for an element without any id.
func resourceOf(element model.QueriesRequestElement) (kind *resourceKind, id string, found bool) {
	for i := range resourceKinds {
		if p := resourceKinds[i].id(element); p != nil {
			return &resourceKinds[i], *p, true
		}
	}
	return nil, "", false
}

// deniedExpirationInSec is how long a denied access stays cached. Shorter than the expiration of
// a granted one, so that a permission just granted takes effect within a minute.
const deniedExpirationInSec = 60

func (verifier *Verifier) verifyResource(ctx context.Context, kind *resourceKind, id string, token string, userId string) (VerifierCacheEntry, error) {
	return cache.UseLocalFor(verifier.c, kind.name+":"+userId+":"+id, func() (VerifierCacheEntry, error) {
		return kind.check(verifier, ctx, id, token, userId)
	}, verifierCacheExpiration)
}

// verifierCacheExpiration keeps a denied access for deniedExpirationInSec and a granted one for
// the expiration of the cache.
func verifierCacheExpiration(entry VerifierCacheEntry) int {
	if !entry.Ok {
		return deniedExpirationInSec
	}
	return cache.LocalCacheExpirationInSec
}

type resourceKey struct {
	kind string
	id   string
}

type resourceResult struct {
	entry VerifierCacheEntry
	err   error
}

// VerifyAccess checks every distinct resource of the elements once, concurrently and with a limit.
// ok is false if any element is denied, has no id or fails; userIds then holds the owner only
// for elements that were allowed. err is the error of a failed check.
func (verifier *Verifier) VerifyAccess(ctx context.Context, elements []model.QueriesRequestElement, token string, userId string) (ok bool, userIds []string, err error) {
	ok = true
	userIds = make([]string, len(elements))

	keys := make([]*resourceKey, len(elements)) // nil: element without id
	kinds := map[resourceKey]*resourceKind{}
	var distinct []resourceKey
	for i, element := range elements {
		kind, id, found := resourceOf(element)
		if !found {
			ok = false
			continue
		}
		key := resourceKey{kind: kind.name, id: id}
		keys[i] = &key
		if _, seen := kinds[key]; !seen {
			kinds[key] = kind
			distinct = append(distinct, key)
		}
	}

	results := make([]resourceResult, len(distinct))
	var g errgroup.Group
	g.SetLimit(verifyConcurrency)
	for i, key := range distinct {
		g.Go(func() error {
			results[i].entry, results[i].err = verifier.verifyResource(ctx, kinds[key], key.id, token, userId)
			return nil
		})
	}
	_ = g.Wait() // the tasks never return an error, failures are collected in results

	byKey := make(map[resourceKey]resourceResult, len(distinct))
	for i, key := range distinct {
		byKey[key] = results[i]
		if results[i].err != nil && err == nil {
			err = results[i].err
		}
	}
	for i, key := range keys {
		if key == nil {
			continue
		}
		result := byKey[*key]
		if result.err != nil || !result.entry.Ok {
			ok = false
			continue
		}
		userIds[i] = result.entry.OwnerUserId
	}
	return ok, userIds, err
}

// VerifyAccessOnce checks a single element. An element without any id yields a denied result and no error.
func (verifier *Verifier) VerifyAccessOnce(ctx context.Context, element model.QueriesRequestElement, token string, userId string) (result VerifierCacheEntry, err error) {
	kind, id, found := resourceOf(element)
	if !found {
		return result, nil
	}
	return verifier.verifyResource(ctx, kind, id, token, userId)
}
