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
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func VerifyDevice(id string, token string, userId string, config configuration.Config) (bool, error) {
	req, err := http.NewRequest("GET", config.PermissionSearchUrl+"/v3/resources/devices/"+id+"/access?rights=r", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("X-UserId", userId)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println("verifyDevice: could not close response body", err.Error())
		}
	}(resp.Body)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	ok, err := strconv.ParseBool(strings.ReplaceAll(string(body), "\n", ""))
	if err != nil {
		return false, err
	}
	return ok, nil
}
