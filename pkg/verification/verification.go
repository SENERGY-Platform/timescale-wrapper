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
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"sync"
)

func VerifyAccess(elements []model.QueriesRequestElement, token string, userId string, config configuration.Config) (bool, error) {
	var err error
	ok := true
	wg := &sync.WaitGroup{}
	for i := range elements {
		i := i // thread safe
		wg.Add(1)
		go func() {
			okS, errS := VerifyAccessOnce(elements[i], token, userId, config)
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

func VerifyAccessOnce(element model.QueriesRequestElement, token string, userId string, config configuration.Config) (bool, error) {
	if element.ExportId != nil {
		return VerifyExport(*element.ExportId, token, userId, config)
	} else if element.DeviceId != nil {
		return VerifyDevice(*element.DeviceId, token, userId, config)
	}
	return true, nil
}
