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
	"context"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/api/util"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"time"
)

var endpoints = []func(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper){}

const userHeader = "X-UserID"

//starts http server; if wg is not nil it will be set as done when the server is stopped
func Start(ctx context.Context, wg *sync.WaitGroup, config configuration.Config, wrapper *timescale.Wrapper) (err error) {
	log.Println("start api")
	router := Router(config, wrapper)
	server := &http.Server{Addr: ":" + config.ApiPort, Handler: router, WriteTimeout: 10 * time.Second, ReadTimeout: 2 * time.Second, ReadHeaderTimeout: 2 * time.Second}
	wg.Add(1)
	go func() {
		log.Println("Listening on ", server.Addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Println("ERROR: api server error", err)
			log.Fatal(err)
		}
	}()
	go func() {
		<-ctx.Done()
		log.Println("DEBUG: api shutdown", server.Shutdown(context.Background()))
		wg.Done()
	}()
	return nil
}

func Router(config configuration.Config, influx *timescale.Wrapper) http.Handler {
	router := httprouter.New()
	for _, e := range endpoints {
		log.Println("add endpoints: " + runtime.FuncForPC(reflect.ValueOf(e).Pointer()).Name())
		e(router, config, influx)
	}
	log.Println("add logging and cors")
	corsHandler := util.NewCors(router)
	return util.NewLogger(corsHandler)
}
