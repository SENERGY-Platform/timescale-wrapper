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
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	gin_mw "github.com/SENERGY-Platform/gin-middleware"
	"github.com/SENERGY-Platform/gin-middleware/otelx"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// deps holds the dependencies the endpoints are built from. pipeline is built once from the other fields, so all
// endpoints of a router share it.
type deps struct {
	config          configuration.Config
	wrapper         *timescale.Wrapper
	verifier        *verification.Verifier
	remoteCache     *cache.RemoteCache
	converter       *converter.Converter
	deviceSelection deviceSelection.Client
	pipeline        *pipeline
}

func newDeps(config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, conv *converter.Converter, deviceSelection deviceSelection.Client) deps {
	return deps{
		config:          config,
		wrapper:         wrapper,
		verifier:        verifier,
		remoteCache:     remoteCache,
		converter:       conv,
		deviceSelection: deviceSelection,
		pipeline:        newPipeline(config, wrapper, verifier, remoteCache, conv),
	}
}

// authenticatedEndpoints are registered on the router of Router, in this order.
var authenticatedEndpoints = []func(router gin.IRouter, d deps){
	LastValuesEndpoint,
	DataAvailabilityEndpoint,
	DocEndpoint,
	PrepareDownloadEndpoints,
	LastMessageEndpoint,
	QueriesEndpoint,
	QueriesV2Endpoint,
	RawValueEndpoint,
	UsageEndpoint,
}

// unauthenticatedEndpoints are registered on the router of UnauthenticatedRouter.
var unauthenticatedEndpoints = []func(router gin.IRouter, d deps){
	DownloadEndpoints,
}

func Start(ctx context.Context, wg *sync.WaitGroup, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client) (err error) {
	log.Logger.Info("start api")
	http.DefaultClient.Timeout = 10 * time.Second
	router, err := Router(ctx, config, wrapper, verifier, cache, converter, deviceSelection)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: ":" + config.ApiPort, Handler: router, WriteTimeout: 30 * time.Second, ReadTimeout: 2 * time.Second, ReadHeaderTimeout: 2 * time.Second}
	unauthenticatedRouter, err := UnauthenticatedRouter(ctx, config, wrapper, verifier, cache, converter, deviceSelection)
	if err != nil {
		return err
	}
	unauthenticatedServer := &http.Server{Addr: ":" + config.UnauthenticatedApiPort, Handler: unauthenticatedRouter, WriteTimeout: 30 * time.Minute, ReadTimeout: 2 * time.Second, ReadHeaderTimeout: 2 * time.Second}
	wg.Add(1)
	go func() {
		log.Logger.Info("Listening on " + server.Addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Logger.Error("api server error", attributes.ErrorKey, err)
			panic(err)
		}
	}()
	go func() {
		log.Logger.Info("Listening on " + unauthenticatedServer.Addr)

		if err := unauthenticatedServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Logger.Error("api unauthenticatedServer error", attributes.ErrorKey, err)
			panic(err)
		}
	}()
	go func() {
		<-ctx.Done()
		log.Logger.Debug("api shutdown")
		server.Shutdown(context.Background())
		log.Logger.Debug("unauthenticated api shutdown")
		unauthenticatedServer.Shutdown(context.Background())
		wg.Done()
	}()
	return nil
}

// GetRouter doc
// @title         Timescale Wrapper API
// @version       0.1
// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html
// @BasePath  /
// @securityDefinitions.apikey Bearer
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.
func Router(ctx context.Context, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client) (http.Handler, error) {
	return newRouter(ctx, "endpoints", authenticatedEndpoints, newDeps(config, wrapper, verifier, cache, converter, deviceSelection))
}

func UnauthenticatedRouter(ctx context.Context, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, cache *cache.RemoteCache, converter *converter.Converter, deviceSelection deviceSelection.Client) (http.Handler, error) {
	return newRouter(ctx, "unauthenticatedEndpoints", unauthenticatedEndpoints, newDeps(config, wrapper, verifier, cache, converter, deviceSelection))
}

// newRouter returns a router with the middleware of configureMW and the given endpoints. label names the list in the log.
func newRouter(ctx context.Context, label string, list []func(router gin.IRouter, d deps), d deps) (http.Handler, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	err := configureMW(ctx, router)
	if err != nil {
		return nil, err
	}
	for _, e := range list {
		log.Logger.Info("add " + label + ": " + runtime.FuncForPC(reflect.ValueOf(e).Pointer()).Name())
		e(router, d)
	}
	return router, nil
}

func configureMW(ctx context.Context, router *gin.Engine) error {
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AddAllowHeaders("Authorization")
	otel, err := otelx.GinOpenTelemetry(ctx, "timescale-wrapper", "")
	if err != nil {
		return err
	}

	router.Use(
		otel,
		gin_mw.StructLoggerHandlerWithDefaultGenerators(
			log.Logger.With(attributes.LogRecordTypeKey, attributes.HttpAccessLogRecordTypeVal),
			attributes.Provider,
			[]string{},
			nil,
		),
		requestid.New(requestid.WithCustomHeaderStrKey("X-Request-ID")),
		cors.New(corsConfig),
		gin_mw.ErrorHandler(model.GetStatusCode, ", "),
		gin_mw.StructRecoveryHandler(log.Logger, gin_mw.DefaultRecoveryFunc),
	)
	return nil
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
	// ParseUnverified checks no claims, so a token without a subject would pass as the empty user
	if err = t.Valid(); err != nil {
		return "", err
	}
	return t.Subject, nil
}

type Token struct {
	jwt.RegisteredClaims
	RealmAccess map[string][]string `json:"realm_access,omitempty"`
}

func (this *Token) Valid() error {
	if this.Subject == "" {
		return errors.New("missing subject")
	}
	return nil
}
