/*
 *    Copyright 2021 InfAI (CC SES)
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
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tracing"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func init() {
	endpoints = append(endpoints, QueriesEndpoint)
}

// Query godoc
// @Summary      queries
// @deprecated
// @Accept       json
// @Produce      json
// @Security Bearer
// @Param        payload body []model.QueriesRequestElement true "requested values"
// @Param		 format query string false "specifies output format. Use per_query (default) for a 3D array or table for a 2D array with merged timestamps"
// @Param		 order_column_index query string false "Column to order values by (includes time column). Only works in format table."
// @Param		 order_direction query string false "Direction to order values by. Allowed are 'asc' and 'desc'. Only works in format table."
// @Param        time_format query string false "Textual representation of the date 'Mon Jan 2 15:04:05 -0700 MST 2006'. Example: 2006-01-02T15:04:05.000Z07:00 would format timestamps as rfc3339 with ms precision. Find details here: https://golang.org/pkg/time/#Time.Format"
// @Param        precision query int false "Number of decimal places floating point values are rounded to, 0 to 15. Integers and other types are returned as they are."
// @Success      200 {array} interface{} "2D or 3D array"
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /queries [POST]
func QueriesEndpoint(router gin.IRouter, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	p := newPipeline(config, wrapper, verifier, remoteCache, converter)
	router.POST("/queries", func(c *gin.Context) {
		writer := c.Writer
		request := c.Request
		requestedFormat, orderColumnIndex, orderDirection, err := queriesParseQueryparams(request)
		if err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return
		}
		precision, err := parsePrecision(request)
		if err != nil {
			c.Error(errors.Join(err, model.ErrBadRequest))
			return
		}

		requestElements, err := decodeElements(request)
		if err == nil {
			err = rejectDeviceGroups(requestElements)
		}
		if err != nil {
			c.Error(err)
			return
		}

		userId, ownerUserIds, err := p.verify(request.Context(), request, requestElements)
		if err != nil {
			c.Error(err)
			return
		}
		lookups := p.newLookups()
		err = p.resolvePaths(request.Context(), lookups, requestElements)
		if err != nil {
			c.Error(err)
			return
		}
		raw, err := p.fetch(request.Context(), requestElements, userId, ownerUserIds, nil, []models.Device{})
		if err != nil {
			c.Error(err)
			return
		}

		ppCtx, ppSpan := p.tracer().Start(request.Context(), "postprocess results")
		defer ppSpan.End()
		timeFormat := request.URL.Query().Get("time_format")
		var response interface{}
		switch requestedFormat {
		case model.Table:
			response, err = p.formatTable(ppCtx, lookups, requestElements, raw, orderColumnIndex, orderDirection, timeFormat)
		default:
			response, err = p.formatPerQuery(ppCtx, lookups, requestElements, raw, orderColumnIndex, orderDirection, timeFormat)
		}
		if err != nil {
			c.Error(err)
			return
		}
		roundResponse(response, precision)
		if request.Header.Get("Accept") == "application/csv" || request.Header.Get("Accept") == "text/csv" {
			writer.Header().Set("Content-Type", request.Header.Get("Accept"))
			csvWriter := csv.NewWriter(writer)
			headers := []string{"time"}
			for i := range requestElements {
				for j := range requestElements[i].Columns {
					headers = append(headers, requestElements[i].Columns[j].Name)
				}
			}
			err = csvWriter.Write(headers)
			if err != nil {
				c.Error(classify(err, model.ErrInternalServerError))
				return
			}
			err = writeCsv(response, csvWriter)
			if err != nil {
				logResponseError(c, err)
			}
		} else {
			writeJSON(c, response)
		}
	})
}

func queriesParseQueryparams(request *http.Request) (requestedFormat model.Format, orderColumnIndex int, orderDirection model.Direction, err error) {
	requestedFormat = model.Format(request.URL.Query().Get("format"))
	paramOrderColumnIndex := request.URL.Query().Get("order_column_index")
	if paramOrderColumnIndex == "" {
		orderColumnIndex = 0
	} else {
		orderColumnIndex, err = strconv.Atoi(paramOrderColumnIndex)
		if err != nil {
			err = errors.New("invalid param order_column_index")
			return
		}
	}
	orderDirection = model.Direction(request.URL.Query().Get("order_direction"))
	if orderDirection == "" {
		orderDirection = model.Desc
	} else if orderDirection != "asc" && orderDirection != "desc" {
		err = errors.New("invalid param orderDirection")
		return
	}
	return
}

func queriesGetFromCache(ctx context.Context, requestElements []model.QueriesRequestElement, remoteCache lastValuesCache, config configuration.Config, forceTz *string) (raw [][][]interface{}, dbRequestElements []model.QueriesRequestElement, dbRequestIndices []int) {
	tracer := otel.Tracer("api/queries.go:queriesGetFromCache")
	c, span := tracer.Start(ctx, "queriesGetFromCache")
	defer span.End()
	dbRequestElements = []model.QueriesRequestElement{}
	dbRequestIndices = []int{}

	raw = make([][][]interface{}, len(requestElements))

	elementCtx := tracing.SuppressChildren(c, config.DetailedTracing)
	missed := make([]bool, len(requestElements)) // each goroutine writes its own index
	wg := sync.WaitGroup{}
	for i := range requestElements {
		wg.Go(func() {
			var err error
			raw[i], err = remoteCache.GetLastValuesFromCache(elementCtx, requestElements[i], forceTz)
			if err != nil {
				missed[i] = true
				if !errors.Is(err, cache.NotCachableError) {
					log.Logger.WarnContext(c, "Could not get data from remoteCache", attributes.ErrorKey, err)
				}
			}
		})
	}
	wg.Wait()
	// in request order, so that the queries and their results keep a stable order
	for i := range requestElements {
		if missed[i] {
			dbRequestElements = append(dbRequestElements, requestElements[i])
			dbRequestIndices = append(dbRequestIndices, i)
		}
	}
	span.SetAttributes(
		attribute.Int("request.elements", len(requestElements)),
		attribute.Int("cache.hits", len(requestElements)-len(dbRequestIndices)),
		attribute.Int("cache.misses", len(dbRequestIndices)),
	)
	if config.Debug {
		log.Logger.DebugContext(c, "Got "+strconv.Itoa(len(requestElements)-len(dbRequestIndices))+" from remoteCache, requesting "+strconv.Itoa(len(dbRequestIndices))+" from db")
	}
	return
}
