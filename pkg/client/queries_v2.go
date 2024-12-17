/*
 * Copyright 2024 InfAI (CC SES)
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

package client

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
)

type QueriesV2Options struct {
	Format           *string
	OrderColumnIndex *int
	OrderDirection   *string
	TimeFormat       *string
	LocateLat        *float64
	LocateLon        *float64
}

func (c impl) GetQueriesV2(token string, requestElements []QueriesRequestElement, options *QueriesV2Options) (result []QueriesV2ResponseElement, code int, err error) {
	body, err := json.Marshal(requestElements)
	if err != nil {
		return result, 0, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseUrl+"/queries/v2", bytes.NewReader(body))
	if err != nil {
		return result, 0, err
	}

	req.Header.Add("Authorization", token)

	q := req.URL.Query()
	if options != nil {
		if options.Format != nil {
			q.Add("format", *options.Format)
		}
		if options.OrderColumnIndex != nil {
			q.Add("order_column_index", strconv.Itoa(*options.OrderColumnIndex))
		}
		if options.OrderDirection != nil {
			q.Add("order_direction", *options.OrderDirection)
		}
		if options.TimeFormat != nil {
			q.Add("time_format", *options.TimeFormat)
		}
		if options.LocateLat != nil {
			q.Add("locate_lat", strconv.FormatFloat(*options.LocateLat, 'f', -1, 64))
		}
		if options.LocateLon != nil {
			q.Add("locate_lon", strconv.FormatFloat(*options.LocateLon, 'f', -1, 64))
		}
	}
	req.URL.RawQuery = q.Encode()

	return do[[]QueriesV2ResponseElement](req)
}
