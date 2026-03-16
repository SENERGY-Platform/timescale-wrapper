/*
 *    Copyright 2026 InfAI (CC SES)
 *
 *    Licensed under the Apache License, Version 2.0 (the "License");
 *    you may not use this file except in compliance with the License.
 *    You may obtain a copy of the License at
 *
 *        http://www.apache.org/licenses/LICENSE-2.0
 *
 *    Unless required by applicable law or agreed to in writing, software
 *    distributed under the License is distributed on an "AS IS" BASIS,
 *    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *    See the License for the specific language governing permissions and
 *    limitations under the License.
 */

package model

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrBadRequest = errors.New("bad request")
var ErrInternalServerError = errors.New("internal server error")
var ErrForbidden = fmt.Errorf("forbidden")
var ErrNotFound = fmt.Errorf("not found")

func GetStatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, ErrBadRequest) {
		return http.StatusBadRequest
	}
	if errors.Is(err, ErrInternalServerError) {
		return http.StatusInternalServerError
	}
	if errors.Is(err, ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, ErrForbidden) {
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func GetError(code int) error {
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusInternalServerError:
		return ErrInternalServerError
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusForbidden:
		return ErrForbidden
	default:
		return ErrInternalServerError
	}
}
