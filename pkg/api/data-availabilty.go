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
	"encoding/json"
	"errors"
	"fmt"

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
		writer := c.Writer
		request := c.Request
		deviceId := request.URL.Query().Get("device_id")
		userId, err := getUserId(request)
		if err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return
		}
		access, err := verifier.VerifyAccessOnce(model.QueriesRequestElement{
			DeviceId: &deviceId,
		}, getToken(request), userId)
		if err != nil {
			c.Error(errors.Join(err, model.ErrInternalServerError))
			return
		}
		if !access.Ok {
			c.Error(errors.Join(errors.New("not found"), model.ErrNotFound))
			return
		}
		response, err := wrapper.GetDataAvailability(deviceId)
		if err != nil {
			c.Error(errors.Join(err, model.ErrInternalServerError))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(response)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})
}
