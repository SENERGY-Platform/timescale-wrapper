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

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/gin-gonic/gin"
)

// Query godoc
// @Summary      Device Usage
// @Accept		 json
// @Produce      json
// @Security Bearer
// @Param		 device_ids body []string true "device_ids"
// @Success      200 {array} model.Usage "usage"
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /usage/devices [GET]
func UsageDevices() {} // for doc

// Query godoc
// @Summary      Export Usage
// @Accept		 json
// @Produce      json
// @Security Bearer
// @Param		 export_ids body []string true "export_ids"
// @Success      200 {array} model.Usage "usage"
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /usage/exports [GET]
func UsageExports() {} // for doc

func UsageEndpoint(router gin.IRouter, d deps) {
	router.POST("/usage/devices", func(c *gin.Context) {
		request := c.Request
		deviceIds := []string{}
		err := json.NewDecoder(request.Body).Decode(&deviceIds)
		if err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return
		}
		elems := []model.QueriesRequestElement{}
		for _, deviceId := range deviceIds {
			elems = append(elems, model.QueriesRequestElement{
				DeviceId: &deviceId,
			})
		}
		_, _, err = verifyRequest(c.Request.Context(), request, d.verifier, elems)
		if err != nil {
			c.Error(err)
			return
		}
		response, err := d.wrapper.GetDeviceUsage(c.Request.Context(), deviceIds)
		if err != nil {
			c.Error(classify(err, model.ErrInternalServerError))
			return
		}
		writeJSON(c, response)
	})

	router.POST("/usage/exports", func(c *gin.Context) {
		request := c.Request
		exportIds := []string{}
		err := json.NewDecoder(request.Body).Decode(&exportIds)
		if err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return
		}
		elems := []model.QueriesRequestElement{}
		for _, exportId := range exportIds {
			elems = append(elems, model.QueriesRequestElement{
				ExportId: &exportId,
			})
		}
		_, _, err = verifyRequest(c.Request.Context(), request, d.verifier, elems)
		if err != nil {
			c.Error(err)
			return
		}
		response, err := d.wrapper.GetExportUsage(c.Request.Context(), exportIds)
		if err != nil {
			c.Error(classify(err, model.ErrInternalServerError))
			return
		}
		writeJSON(c, response)
	})
}
