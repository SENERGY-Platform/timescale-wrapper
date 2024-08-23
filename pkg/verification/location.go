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
	"github.com/SENERGY-Platform/permission-search/lib/client"
)

func (verifier *Verifier) verifyLocation(id string, token string) (bool, error) {
	err := verifier.permSearchClient.CheckUserOrGroup(token, "locations", id, "rx")
	if errors.Is(err, client.ErrAccessDenied) {
		return false, nil
	}
	if errors.Is(err, client.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
