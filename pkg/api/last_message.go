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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
)

func init() {
	endpoints = append(endpoints, LastMessageEndpoint)
}

// Query godoc
// @Summary      Last Message
// @Produce      json
// @Security Bearer
// @Param		 device_id query string true "device_id"
// @Param		 service_id query string true "service_id"
// @Success      200 {object} cache.Entry "The last message"
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /last-message [GET]
func LastMessageEndpoint(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	router.GET("/last-message", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		deviceId := request.URL.Query().Get("device_id")
		serviceId := request.URL.Query().Get("service_id")
		if len(deviceId) == 0 {
			http.Error(writer, "missing query param device_id", http.StatusBadRequest)
			return
		}
		if len(serviceId) == 0 {
			http.Error(writer, "missing query param service_id", http.StatusBadRequest)
			return
		}

		ok, err := verifier.VerifyDevice(deviceId, getToken(request))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok.Ok {
			http.Error(writer, "not found", http.StatusNotFound)
			return
		}

		entry, err := remoteCache.GetLastMessageFromCache(deviceId, serviceId)
		if err != nil {
			service, err := remoteCache.GetService(serviceId)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
			entry, err = wrapper.GetLastMessage(deviceId, serviceId, service)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(entry)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})
}
