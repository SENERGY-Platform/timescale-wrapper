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

func (verifier *Verifier) VerifyDevice(id string, token string) (result VerifierCacheEntry, err error) {
	access, err, _ := verifier.permClient.CheckPermission(token, "devices", id, client.Execute)
	result.Ok = access
	return result, err
}
