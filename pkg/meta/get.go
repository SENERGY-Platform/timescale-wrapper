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

package meta

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

func GetService(id string, url string) (service Service, err error) {
	url += "/services/" + id
	body, err := get(url)
	if err != nil {
		return
	}
	err = json.NewDecoder(body).Decode(&service)
	return
}

func GetConcept(id string, url string) (concept Concept, err error) {
	url += "/concepts/" + id
	body, err := get(url)
	if err != nil {
		return
	}
	err = json.NewDecoder(body).Decode(&concept)
	return
}

func get(url string) (body io.ReadCloser, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("unexpected status code while getting service: " + strconv.Itoa(resp.StatusCode) + ", URL was " + url)
	}
	return resp.Body, err
}
