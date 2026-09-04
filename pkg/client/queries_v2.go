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
	"context"
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
	ForceTz          *string
}

// GetQueriesV2 reads through /queries/v2 without a caller context. See Client:
// new code should use GetQueriesV2Context, which joins the caller's trace and
// honours its cancellation.
func (c impl) GetQueriesV2(token string, requestElements []QueriesRequestElement, options *QueriesV2Options) (result []QueriesV2ResponseElement, code int, err error) {
	return c.GetQueriesV2Context(context.TODO(), token, requestElements, options)
}

func (c impl) GetQueriesV2Context(ctx context.Context, token string, requestElements []QueriesRequestElement, options *QueriesV2Options) (result []QueriesV2ResponseElement, code int, err error) {
	body, err := json.Marshal(requestElements)
	if err != nil {
		return result, 0, err
	}

	req, err := newRequest(ctx, http.MethodPost, c.baseUrl+"/queries/v2", token, body)
	if err != nil {
		return result, 0, err
	}

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
		if options.ForceTz != nil {
			q.Add("force_tz", *options.ForceTz)
		}
	}
	req.URL.RawQuery = q.Encode()

	return do[[]QueriesV2ResponseElement](req)
}
