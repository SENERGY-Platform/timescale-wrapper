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

package util

import (
	"log"
	"net/http"
)

func NewLogger(handler http.Handler) *LoggerMiddleWare {
	return &LoggerMiddleWare{handler: handler}
}

type LoggerMiddleWare struct {
	handler http.Handler
}

func (this *LoggerMiddleWare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	this.log(r)
	if this.handler != nil {
		this.handler.ServeHTTP(w, r)
	} else {
		http.Error(w, "Forbidden", 403)
	}
}

func (this *LoggerMiddleWare) log(request *http.Request) {
	log.Printf("[%v] %v \n", request.Method, request.URL)
}
