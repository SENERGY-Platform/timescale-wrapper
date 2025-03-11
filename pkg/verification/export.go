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
	"github.com/SENERGY-Platform/permissions-v2/pkg/client"
)

const ServingExportInstanceTopic string = "export-instances"

func (verifier *Verifier) VerifyExport(id string, token string, userId string) (result VerifierCacheEntry, err error) {
	access, err, _ := verifier.permClient.CheckPermission(token, ServingExportInstanceTopic, id, client.Execute)
	if !access || err != nil {
		return result, err
	}
	instance, err := verifier.servingClient.GetInstance(token, id)
	if err != nil {
		return result, err
	}
	result.Ok = true
	result.OwnerUserId = instance.UserId
	return result, nil
}
