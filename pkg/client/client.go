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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/SENERGY-Platform/gin-middleware/otelx"
)

// Client is the timescale wrapper's HTTP API as a Go interface.
//
// Every call exists twice. The Context variants put the caller's context on
// the request and inject the trace context and the baggage into it, so the
// call appears as a child span in the caller's own trace and stops when the
// caller stops. The variants without a context pass context.TODO() and are
// kept so that existing callers keep building; new code should use the
// Context variants.
type Client interface {
	GetDeviceUsage(token string, deviceIds []string) (result []Usage, code int, err error)
	GetDeviceUsageContext(ctx context.Context, token string, deviceIds []string) (result []Usage, code int, err error)
	GetExportUsage(token string, exportIds []string) (result []Usage, code int, err error)
	GetExportUsageContext(ctx context.Context, token string, exportIds []string) (result []Usage, code int, err error)
	GetQueriesV2(token string, requestElements []QueriesRequestElement, options *QueriesV2Options) (result []QueriesV2ResponseElement, code int, err error)
	GetQueriesV2Context(ctx context.Context, token string, requestElements []QueriesRequestElement, options *QueriesV2Options) (result []QueriesV2ResponseElement, code int, err error)
}

type impl struct {
	baseUrl string
}

func NewClient(baseUrl string) Client {
	return &impl{baseUrl: baseUrl}
}

// newRequest builds a request carrying the caller's context, its trace and
// its bearer token.
//
// http.NewRequestWithContext and not http.NewRequest, which is the difference
// between a call that can be cancelled and one that cannot:
// otelx.InjectContextToRequest writes the traceparent and baggage headers but
// calls req.WithContext on a copy of its own, so the context has to be put on
// the request here. Without this, a caller's timeout bounds nothing and
// whatever http.DefaultClient does is the only limit - and it has no timeout.
func newRequest(ctx context.Context, method string, url string, token string, body []byte) (*http.Request, error) {
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Authorization", token)
	if err = otelx.InjectContextToRequest(ctx, req); err != nil {
		return nil, err
	}
	return req, nil
}

func do[T any](req *http.Request) (result T, code int, err error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return result, http.StatusInternalServerError, err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 299 {
		temp, _ := io.ReadAll(resp.Body) //read error response end ensure that resp.Body is read to EOF
		return result, resp.StatusCode, GetErrFromCode(resp.StatusCode, string(temp))
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		_, _ = io.ReadAll(resp.Body) //ensure resp.Body is read to EOF
		return result, http.StatusInternalServerError, err
	}
	return result, resp.StatusCode, nil
}

var ErrNotFound = errors.New("not found")
var ErrAccessDenied = errors.New("access denied")
var ErrBadRequest = errors.New("bad request")
var ErrInvalidAuth = errors.New("invalid auth token")

func GetErrCode(err error) (code int) {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, ErrBadRequest) {
		return http.StatusBadRequest
	}
	if errors.Is(err, ErrAccessDenied) {
		return http.StatusForbidden
	}
	if errors.Is(err, ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, ErrInvalidAuth) {
		return http.StatusUnauthorized
	}
	return http.StatusInternalServerError
}

func GetErrFromCode(code int, msg string) error {
	if code < 300 {
		return nil
	}

	if msg == "" {
		msg = "received status code " + strconv.Itoa(code)
	}

	//clean message
	cleanedMessage := msg
	cleanedMessage = strings.TrimPrefix(msg, ErrBadRequest.Error())
	cleanedMessage = strings.TrimPrefix(msg, ErrAccessDenied.Error())
	cleanedMessage = strings.TrimPrefix(msg, ErrBadRequest.Error())
	cleanedMessage = strings.TrimPrefix(msg, ErrInvalidAuth.Error())
	cleanedMessage = strings.TrimSpace(msg)
	cleanedMessage = strings.TrimPrefix(msg, ":")
	cleanedMessage = strings.TrimSpace(msg)
	if cleanedMessage == "" {
		cleanedMessage = msg
	}

	switch code {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %v", ErrBadRequest, cleanedMessage)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %v", ErrAccessDenied, cleanedMessage)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %v", ErrNotFound, cleanedMessage)
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %v", ErrInvalidAuth, cleanedMessage)
	default:
		return errors.New(msg)
	}
}
