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
	"net/http"
)

func (verifier *Verifier) verifyExport(id string, token string, userId string) (bool, error) {
	req, err := http.NewRequest("GET", verifier.config.ServingUrl+"/instance/"+id, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("X-UserId", userId)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	return resp.StatusCode == http.StatusOK, nil
}
