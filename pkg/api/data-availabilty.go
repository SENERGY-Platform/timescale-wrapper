/*
 *    Copyright 2023 InfAI (CC SES)
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
	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/gin-gonic/gin"
)

func init() {
	endpoints = append(endpoints, DataAvailabilityEndpoint)
}

// Query godoc
// @Summary      query data availabilty
// @Description  query data availabilty of a device
// @Accept       json
// @Produce      json
// @Security Bearer
// @Param        device_id query string true "ID of requested device"
// @Success      200 {array}  model.DataAvailabilityResponseElement
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /data-availability [GET]
func DataAvailabilityEndpoint(router gin.IRouter, _ configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, _ *cache.RemoteCache, _ *converter.Converter, _ deviceSelection.Client) {
	router.GET("/data-availability", func(c *gin.Context) {
		request := c.Request
		deviceId := request.URL.Query().Get("device_id")
		_, _, err := verifyRequest(c.Request.Context(), request, verifier, []model.QueriesRequestElement{{DeviceId: &deviceId}})
		if err != nil {
			c.Error(err)
			return
		}
		response, err := wrapper.GetDataAvailability(c.Request.Context(), deviceId)
		if err != nil {
			c.Error(classify(err, model.ErrInternalServerError))
			return
		}
		writeJSON(c, response)
	})
}
