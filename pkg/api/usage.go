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
	"fmt"
	"net/http"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
)

func init() {
	endpoints = append(endpoints, UsageEndpoint)
}

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

func UsageEndpoint(router *httprouter.Router, _ configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, _ *cache.RemoteCache, _ *converter.Converter, _ deviceSelection.Client) {
	router.POST("/usage/devices", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		deviceIds := []string{}
		err := json.NewDecoder(request.Body).Decode(&deviceIds)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		elems := []model.QueriesRequestElement{}
		for _, deviceId := range deviceIds {
			elems = append(elems, model.QueriesRequestElement{
				DeviceId: &deviceId,
			})
		}
		userId, err := getUserId(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		ok, _, err := verifier.VerifyAccess(elems, getToken(request), userId)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(writer, "not found", http.StatusNotFound)
			return
		}
		response, err := wrapper.GetDeviceUsage(deviceIds)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(response)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})

	router.POST("/usage/exports", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		exportIds := []string{}
		err := json.NewDecoder(request.Body).Decode(&exportIds)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		elems := []model.QueriesRequestElement{}
		for _, exportId := range exportIds {
			elems = append(elems, model.QueriesRequestElement{
				ExportId: &exportId,
			})
		}
		userId, err := getUserId(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		ok, _, err := verifier.VerifyAccess(elems, getToken(request), userId)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(writer, "not found", http.StatusNotFound)
			return
		}
		response, err := wrapper.GetExportUsage(exportIds)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(response)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})
}
