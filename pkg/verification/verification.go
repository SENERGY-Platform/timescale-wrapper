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
	"errors"
	permSearchClient "github.com/SENERGY-Platform/permission-search/lib/client"
	permClient "github.com/SENERGY-Platform/permissions-v2/pkg/client"

	"sync"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

type Verifier struct {
	c                *cache.LocalCache
	config           configuration.Config
	permClient       permClient.Client
	permSearchClient permSearchClient.Client
}

func New(config configuration.Config) *Verifier {
	var client permClient.Client
	if config.PermissionsUrl != "" && config.PermissionsUrl != "-" {
		client = permClient.New(config.PermissionsUrl)
	}
	return &Verifier{
		c:                cache.NewLocal(),
		config:           config,
		permClient:       client,
		permSearchClient: permSearchClient.NewClient(config.PermissionSearchUrl),
	}
}

var errUnexpectedUpstreamStatuscode = errors.New("unexpected upstream statuscode")

func (verifier *Verifier) VerifyAccess(elements []model.QueriesRequestElement, token string, userId string) (bool, error) {
	var err error
	ok := true
	wg := &sync.WaitGroup{}
	for i := range elements {
		i := i // thread safe
		wg.Add(1)
		go func() {
			okS, errS := verifier.VerifyAccessOnce(elements[i], token, userId)
			if errS != nil {
				err = errS
				ok = false
			} else if !okS {
				ok = false
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return ok, err
}

func (verifier *Verifier) VerifyAccessOnce(element model.QueriesRequestElement, token string, userId string) (ok bool, err error) {
	if element.ExportId != nil {
		err = verifier.c.Use(userId+*element.ExportId, func() (interface{}, error) {
			return verifier.verifyExport(*element.ExportId, token, userId)
		}, &ok)
		return
	} else if element.DeviceId != nil {
		err = verifier.c.Use(userId+*element.DeviceId, func() (interface{}, error) {
			return verifier.verifyDevice(*element.DeviceId, token)
		}, &ok)
		return
	} else if element.DeviceGroupId != nil {
		err = verifier.c.Use(userId+*element.DeviceGroupId, func() (interface{}, error) {
			return verifier.verifyDeviceGroup(*element.DeviceGroupId, token)
		}, &ok)
		return
	}
	return true, nil
}
