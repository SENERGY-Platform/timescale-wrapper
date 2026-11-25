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

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/gin-gonic/gin"
)

// accessVerifier is the part of *verification.Verifier that verifyRequest needs.
type accessVerifier interface {
	VerifyAccess(ctx context.Context, elements []model.QueriesRequestElement, token string, userId string) (ok bool, userIds []string, err error)
}

var _ accessVerifier = (*verification.Verifier)(nil)

// statusClasses are the errors model.GetStatusCode maps to a status code.
var statusClasses = []error{model.ErrBadRequest, model.ErrInternalServerError, model.ErrNotFound, model.ErrForbidden, model.ErrNoContent}

// classify makes sure err carries a status class. An error that already has one is returned unchanged, so that the
// class its origin chose is not overridden by the order model.GetStatusCode checks in. Any other error is joined
// with fallback; a nil fallback means model.ErrInternalServerError. classify(nil, ...) is nil.
func classify(err error, fallback error) error {
	if err == nil {
		return nil
	}
	for _, class := range statusClasses {
		if errors.Is(err, class) {
			return err
		}
	}
	if fallback == nil {
		fallback = model.ErrInternalServerError
	}
	return errors.Join(err, fallback)
}

// classifyDB classifies an error of the database. An error without a class gets the one timescale.GetHTTPErrorCode
// assigns to it; codes without a class of their own (e.g. 502) are answered as 500.
func classifyDB(err error) error {
	if err == nil {
		return nil
	}
	return classify(err, model.GetError(timescale.GetHTTPErrorCode(err)))
}

// verifyRequest reads the user from the token of the request and checks that the user may access all elements.
// The returned error carries exactly one status class: model.ErrBadRequest for a missing or invalid token,
// model.ErrNotFound if access is denied and model.ErrInternalServerError if a check fails.
// ownerUserIds holds the owner of each element, in the order of elements.
func verifyRequest(ctx context.Context, request *http.Request, verifier accessVerifier, elements []model.QueriesRequestElement) (userId string, ownerUserIds []string, err error) {
	userId, err = getUserId(request)
	if err != nil {
		return "", nil, classify(err, model.ErrBadRequest)
	}
	ok, ownerUserIds, err := verifier.VerifyAccess(ctx, elements, getToken(request), userId)
	if err != nil {
		return "", nil, classify(err, model.ErrInternalServerError)
	}
	if !ok {
		return "", nil, errors.Join(errors.New("not found"), model.ErrNotFound)
	}
	return userId, ownerUserIds, nil
}

// writeJSON answers with v as JSON. A failure to write the response can not be reported to the client any more
// and is logged.
func writeJSON(c *gin.Context, v any) {
	c.Writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(c.Writer).Encode(v); err != nil {
		logResponseError(c, err)
	}
}

// logResponseError logs the failure to write a response whose status has already been sent.
func logResponseError(c *gin.Context, err error) {
	log.Logger.ErrorContext(c.Request.Context(), "could not write response", attributes.ErrorKey, err)
}
