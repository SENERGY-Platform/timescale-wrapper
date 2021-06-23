/*
 *    Copyright 2020 InfAI (CC SES)
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
	"fmt"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/julienschmidt/httprouter"
	"io/ioutil"
	"log"
	"net/http"
)

const swaggerJSONLocation = "pkg/resources/swagger.json"

func init() {
	endpoints = append(endpoints, DocEndpoint)
}

func DocEndpoint(router *httprouter.Router, _ configuration.Config, _ *timescale.Wrapper) {
	json, readErr := ioutil.ReadFile(swaggerJSONLocation)
	if readErr != nil {
		log.Println("ERROR reading swagger definition from ", swaggerJSONLocation)
	}

	router.GET("/doc", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		if readErr != nil {
			http.Error(writer, "Error reading doc file", http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, err := writer.Write(json)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})
}
