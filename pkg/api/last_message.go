/*
 *    Copyright 2025 InfAI (CC SES)
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
	"errors"
	"net/http"
	"strings"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/gin-gonic/gin"
)

// Query godoc
// @Summary      Last Message
// @Produce      json
// @Security Bearer
// @Param		 device_id query string true "device_id"
// @Param		 service_id query string true "service_id"
// @Success      200 {object} cache.Entry "The last message"
// @Success      204 "No message available"
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /last-message [GET]
func LastMessageEndpoint(router gin.IRouter, d deps) {
	router.GET("/last-message", func(c *gin.Context) {
		request := c.Request
		deviceId := request.URL.Query().Get("device_id")
		serviceId := request.URL.Query().Get("service_id")
		if len(deviceId) == 0 {
			c.Error(errors.Join(errors.New("missing query param device_id"), model.ErrBadRequest))
			return
		}
		if len(serviceId) == 0 {
			c.Error(errors.Join(errors.New("missing query param service_id"), model.ErrBadRequest))
			return
		}

		ok, err := d.verifier.VerifyDevice(c.Request.Context(), deviceId, getToken(request))
		if err != nil {
			c.Error(classify(err, model.ErrInternalServerError))
			return
		}
		if !ok.Ok {
			c.Error(errors.Join(errors.New("not found"), model.ErrNotFound))
			return
		}

		deviceId = strings.Split(deviceId, "$")[0]

		entry, err := d.remoteCache.GetLastMessageFromCache(c.Request.Context(), deviceId, serviceId)
		if err != nil {
			service, err := d.remoteCache.GetService(c.Request.Context(), serviceId)
			if err != nil {
				c.Error(classify(err, model.ErrInternalServerError))
				return
			}
			entry, err = d.wrapper.GetLastMessage(c.Request.Context(), deviceId, serviceId, service)
			if err != nil {
				if errors.Is(err, model.ErrNoContent) {
					c.Status(http.StatusNoContent)
					return
				}
				c.Error(classify(err, model.ErrInternalServerError))
				return
			}
		}
		// the cached time keeps the offset of its writer, the database one that of the session;
		// both go out in the local time zone, like the timestamps of every other endpoint
		entry.Time = model.LocalTime(entry.Time)

		writeJSON(c, entry)
	})
}
