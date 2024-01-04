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
	"errors"
	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/api/util"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/golang-jwt/jwt"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
)

var endpoints = []func(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client){}
var unauthenticatedEndpoints = []func(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client){}

func Start(ctx context.Context, wg *sync.WaitGroup, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client) (err error) {
	log.Println("start api")
	router := Router(config, wrapper, verifier, cache, converter, deviceSelection)
	server := &http.Server{Addr: ":" + config.ApiPort, Handler: router, WriteTimeout: 30 * time.Second, ReadTimeout: 2 * time.Second, ReadHeaderTimeout: 2 * time.Second}
	unauthenticatedRouter := UnauthenticatedRouter(config, wrapper, verifier, cache, converter, deviceSelection)
	unauthenticatedServer := &http.Server{Addr: ":" + config.UnauthenticatedApiPort, Handler: unauthenticatedRouter, WriteTimeout: 30 * time.Minute, ReadTimeout: 2 * time.Second, ReadHeaderTimeout: 2 * time.Second}
	wg.Add(1)
	go func() {
		log.Println("Listening on ", server.Addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Println("ERROR: api server error", err)
			log.Fatal(err)
		}
	}()
	go func() {
		log.Println("Listening on ", unauthenticatedServer.Addr)

		if err := unauthenticatedServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Println("ERROR: api unauthenticatedServer error", err)
			log.Fatal(err)
		}
	}()
	go func() {
		<-ctx.Done()
		log.Println("DEBUG: api shutdown", server.Shutdown(context.Background()))
		log.Println("DEBUG: unauthenticated api shutdown", unauthenticatedServer.Shutdown(context.Background()))
		wg.Done()
	}()
	return nil
}

func Router(config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client) http.Handler {
	router := httprouter.New()
	for _, e := range endpoints {
		log.Println("add endpoints: " + runtime.FuncForPC(reflect.ValueOf(e).Pointer()).Name())
		e(router, config, wrapper, verifier, cache, converter, deviceSelection)
	}
	log.Println("add logging and cors")
	corsHandler := util.NewCors(router)
	return util.NewLogger(corsHandler)
}

func UnauthenticatedRouter(config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client) http.Handler {
	router := httprouter.New()
	for _, e := range unauthenticatedEndpoints {
		log.Println("add unauthenticatedEndpoints: " + runtime.FuncForPC(reflect.ValueOf(e).Pointer()).Name())
		e(router, config, wrapper, verifier, cache, converter, deviceSelection)
	}
	log.Println("add logging and cors")
	corsHandler := util.NewCors(router)
	return util.NewLogger(corsHandler)
}

func getToken(request *http.Request) string {
	return request.Header.Get("Authorization")
}

func getUserId(request *http.Request) (string, error) {
	orig := request.Header.Get("Authorization")
	if len(orig) > 7 && strings.ToLower(orig[:7]) == "bearer " {
		orig = orig[7:]
	}
	t := Token{}
	_, _, err := new(jwt.Parser).ParseUnverified(orig, &t)
	if err != nil {
		return "", err
	}
	return t.Sub, nil
}

type Token struct {
	Token       string              `json:"-"`
	Sub         string              `json:"sub,omitempty"`
	RealmAccess map[string][]string `json:"realm_access,omitempty"`
}

func (this *Token) Valid() error {
	if this.Sub == "" {
		return errors.New("missing subject")
	}
	return nil
}
