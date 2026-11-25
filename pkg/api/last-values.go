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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/gin-gonic/gin"
)

func init() {
	endpoints = append(endpoints, LastValuesEndpoint)
}

type queriesRequestElementColumn struct {
	*model.QueriesRequestElementColumn
	requestIndex int
}

// Query godoc
// @Summary      last-values
// @Accept       json
// @Produce      json
// @Security Bearer
// @Param        payload body []model.LastValuesRequestElement true "requested values"
// @Param        time_format query string false "Textual representation of the date 'Mon Jan 2 15:04:05 -0700 MST 2006'. Example: 2006-01-02T15:04:05.000Z07:00 would format timestamps as rfc3339 with ms precision. Find details here: https://golang.org/pkg/time/#Time.Format"
// @Param        precision query int false "Number of decimal places floating point values are rounded to, 0 to 15. Integers and other types are returned as they are."
// @Success      200 {array} model.LastValuesResponseElement
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /last-values [POST]
func LastValuesEndpoint(router gin.IRouter, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter, _ deviceSelection.Client) {
	handler := lastValueHandler(config, wrapper, verifier, remoteCache, converter)

	router.POST("/last-values", func(c *gin.Context) {
		resp, code, err := handler(c.Request.Context(), c.Request)
		if err != nil {
			c.Error(classify(err, model.GetError(code)))
			return
		}
		writeJSON(c, resp)
	})
}

func lastValueHandler(config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter) func(ctx context.Context, request *http.Request) ([]model.LastValuesResponseElement, int, error) {
	p := newPipeline(config, wrapper, verifier, remoteCache, converter)
	return func(ctx context.Context, request *http.Request) ([]model.LastValuesResponseElement, int, error) {
		var requestElements []model.LastValuesRequestElement
		err := json.NewDecoder(request.Body).Decode(&requestElements)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		return p.lastValues(ctx, request, requestElements)
	}
}

// lastValues answers the last value of each requested element. The precision and the time format come from the
// query of the request.
func (p *pipeline) lastValues(ctx context.Context, request *http.Request, requestElements []model.LastValuesRequestElement) ([]model.LastValuesResponseElement, int, error) {
	fail := func(err error) ([]model.LastValuesResponseElement, int, error) {
		return nil, model.GetStatusCode(err), err
	}
	precision, err := parsePrecision(request)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	fullRequestElements, outputIndexToInputIndex, err := buildLastValuesElements(requestElements)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	userId, ownerUserIds, err := p.verify(ctx, request, fullRequestElements)
	if err != nil {
		return fail(err)
	}
	if err = p.resolvePaths(ctx, fullRequestElements); err != nil {
		return fail(err)
	}
	raw, err := p.fetch(ctx, fullRequestElements, userId, ownerUserIds, nil, []models.Device{})
	if err != nil {
		return fail(err)
	}

	ppCtx, ppSpan := p.tracer().Start(ctx, "postprocess")
	defer ppSpan.End()
	timeFormat := request.URL.Query().Get("time_format")
	if timeFormat == "" {
		timeFormat = time.RFC3339Nano
	}
	responseRawData, err := p.format(ppCtx, model.PerQuery, fullRequestElements, raw, 0, model.Desc, timeFormat)
	if err != nil {
		return fail(err)
	}
	responseData, ok := responseRawData.([][][]interface{})
	if !ok {
		return nil, http.StatusInternalServerError, errors.New("could not cast response")
	}

	responseElements := make([]model.LastValuesResponseElement, len(requestElements))
	inserted := 0
	for i := range responseData {
		if len(responseData[i]) == 0 {
			inserted += len(fullRequestElements[i].Columns)
			continue
		}
		t := ""
		for j := range responseData[i][0] {
			if j == 0 {
				if responseData[i][0][0] != nil {
					t, ok = responseData[i][0][0].(string)
					if !ok {
						return nil, http.StatusInternalServerError, fmt.Errorf("unexpected time %v", responseData[i][0][0])
					}
				}
			} else {
				// use known order to insert result at correct location
				var tP *string
				if responseData[i][0][0] != nil {
					tP = &t
				}
				responseElements[outputIndexToInputIndex[inserted]] = model.LastValuesResponseElement{
					Time:  tP,
					Value: roundOptional(responseData[i][0][j], precision),
				}
				inserted++
			}
		}
	}
	return responseElements, http.StatusOK, nil
}

// buildLastValuesElements groups the requested values by export or device and service into one
// query element each. outputIndexToInputIndex maps the position of a result column in the
// concatenated elements back to its position in the request.
// Only the not_null flags of an element's own columns become filters of that element. An element
// without any keeps Filters nil, which is what makes it eligible for the last value cache.
func buildLastValuesElements(requestElements []model.LastValuesRequestElement) (elements []model.QueriesRequestElement, outputIndexToInputIndex map[int]int, err error) {
	one := 1
	exportQueriesRequestElementColumnMap := map[string][]queriesRequestElementColumn{}
	deviceQueriesRequestElementColumnMap := map[string][]queriesRequestElementColumn{}

	for i := range requestElements {
		if requestElements[i].ExportId != nil {
			fillColumnMap(exportQueriesRequestElementColumnMap, *requestElements[i].ExportId, i,
				requestElements[i].ColumnName, requestElements[i].Math, requestElements[i].SourceCharacteristicId,
				requestElements[i].TargetCharacteristicId, requestElements[i].ConceptId)
		} else if requestElements[i].DeviceId != nil && requestElements[i].ServiceId != nil {
			fillColumnMap(deviceQueriesRequestElementColumnMap, *requestElements[i].DeviceId+","+*requestElements[i].ServiceId,
				i, requestElements[i].ColumnName, requestElements[i].Math, requestElements[i].SourceCharacteristicId,
				requestElements[i].TargetCharacteristicId, requestElements[i].ConceptId)
		} else {
			return nil, nil, errors.New("invalid request body")
		}
	}
	elements = make([]model.QueriesRequestElement, len(exportQueriesRequestElementColumnMap)+len(deviceQueriesRequestElementColumnMap))
	outputIndexToInputIndex = map[int]int{} // Remember order of requested columns
	zero := 0
	desc := model.Desc
	elementsIndex := 0
	inserted := 0 // Count the number of requested columns
	for k, v := range exportQueriesRequestElementColumnMap {
		var cols []model.QueriesRequestElementColumn
		cols, inserted = prepColumns(inserted, outputIndexToInputIndex, v)
		elements[elementsIndex] = model.QueriesRequestElement{
			ExportId:         &k,
			Time:             nil,
			Limit:            &one,
			Columns:          cols,
			OrderColumnIndex: &zero,
			OrderDirection:   &desc,
			Filters:          notNullFilters(requestElements, v),
		}
		if !elements[elementsIndex].Valid() {
			return nil, nil, errors.New("invalid request body")
		}
		elementsIndex++
	}
	for k, v := range deviceQueriesRequestElementColumnMap {
		s := strings.Split(k, ",")
		var cols []model.QueriesRequestElementColumn
		cols, inserted = prepColumns(inserted, outputIndexToInputIndex, v)
		elements[elementsIndex] = model.QueriesRequestElement{
			DeviceId:         &s[0],
			ServiceId:        &s[1],
			Time:             nil,
			Limit:            &one,
			Columns:          cols,
			OrderColumnIndex: &zero,
			OrderDirection:   &desc,
			Filters:          notNullFilters(requestElements, v),
		}
		if !elements[elementsIndex].Valid() {
			return nil, nil, errors.New("invalid request body")
		}
		elementsIndex++
	}
	return elements, outputIndexToInputIndex, nil
}

// notNullFilters returns the not_null filters of the given columns, or nil if there are none.
func notNullFilters(requestElements []model.LastValuesRequestElement, cols []queriesRequestElementColumn) *[]model.QueriesRequestElementFilter {
	var filters []model.QueriesRequestElementFilter
	for _, col := range cols {
		if !requestElements[col.requestIndex].NotNull {
			continue
		}
		filters = append(filters, model.QueriesRequestElementFilter{
			Column: col.Name,
			Type:   "!=",
			Value:  nil,
		})
	}
	if filters == nil {
		return nil
	}
	return &filters
}

func fillColumnMap(m map[string][]queriesRequestElementColumn, id string, i int, columnName string, columnMath *string, sourceCharacteristicId *string, targetCharacteristicId *string, conceptId *string) {
	existing, ok := m[id]
	if ok {
		existing = append(existing, queriesRequestElementColumn{
			requestIndex: i,
			QueriesRequestElementColumn: &model.QueriesRequestElementColumn{
				Name:                   columnName,
				Math:                   columnMath,
				SourceCharacteristicId: sourceCharacteristicId,
				TargetCharacteristicId: targetCharacteristicId,
				ConceptId:              conceptId,
			}})
	} else {
		existing = []queriesRequestElementColumn{{
			requestIndex: i,
			QueriesRequestElementColumn: &model.QueriesRequestElementColumn{
				Name:                   columnName,
				Math:                   columnMath,
				SourceCharacteristicId: sourceCharacteristicId,
				TargetCharacteristicId: targetCharacteristicId,
				ConceptId:              conceptId,
			}}}
	}
	m[id] = existing
}

func prepColumns(inserted int, outputIndexToInputIndex map[int]int, v []queriesRequestElementColumn) ([]model.QueriesRequestElementColumn, int) {
	cols := make([]model.QueriesRequestElementColumn, len(v))
	for j := range v {
		cols[j] = *v[j].QueriesRequestElementColumn
		outputIndexToInputIndex[inserted] = v[j].requestIndex
		inserted++
	}
	return cols, inserted
}
