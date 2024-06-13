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
	"github.com/SENERGY-Platform/permissions-v2/pkg/client"
	"log"
	"net/http"
)

const ServingExportInstanceTopic string = "export-instances"

func (verifier *Verifier) verifyExport(id string, token string, userId string) (bool, error) {
	if verifier.permClient != nil {
		access, err, _ := verifier.permClient.CheckPermission(token, ServingExportInstanceTopic, id, client.Execute)
		return access, err
	} else if verifier.config.ServingUrl != "" && verifier.config.ServingUrl != "-" {
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
		if resp.StatusCode > 299 {
			log.Printf("WARN: Could not verify device group access at analytics-serving. upstream status code %v\n", resp.StatusCode)
			return false, errUnexpectedUpstreamStatuscode
		}
		return resp.StatusCode == http.StatusOK, nil
	}
	return false, errors.New("missing verify config")
}
