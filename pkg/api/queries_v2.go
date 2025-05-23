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
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	"github.com/SENERGY-Platform/device-repository/lib/idmodifier"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"github.com/julienschmidt/httprouter"
)

func init() {
	endpoints = append(endpoints, QueriesV2Endpoint)
}

// Query godoc
// @Summary      last-values
// @Accept       json
// @Produce      json
// @Security Bearer
// @Param        payload body []model.QueriesRequestElement true "requested values"
// @Param		 format query string false "specifies output format. Use per_query (default) for a 3D array or table for a 2D array with merged timestamps"
// @Param		 order_column_index query string false "Column to order values by (includes time column). Only works in format table."
// @Param		 order_direction query string false "Direction to order values by. Allowed are 'asc' and 'desc'. Only works in format table."
// @Param        time_format query string false "Textual representation of the date 'Mon Jan 2 15:04:05 -0700 MST 2006'. Example: 2006-01-02T15:04:05.000Z07:00 would format timestamps as rfc3339 with ms precision. Find details here: https://golang.org/pkg/time/#Time.Format"
// @Param		 locate_lat query string false "Used to automatically select the clostest location on a multivalued import export. Only works with exportId set to an export of an import. User needs read access to the import type."
// @Param		 locate_lon query string false "Used to automatically select the clostest location on a multivalued import export. Only works with exportId set to an export of an import. User needs read access to the import type."
// @Param		 force_tz query string false "Calculate aggregations with the specified timezone instead of the default device timezone. Might increase calculation complexity and response time."
// @Success      200 {array} model.QueriesV2ResponseElement "requestIndex allows to match response and request elements (topmost array in request). If a device group is requested, each device will return its own time series. If multiple columns are requested, each will be return as a time series within the data field. If a criteria is selected and multiple paths match the criteria, all matching values will be part of the time series."
// @Failure      400
// @Failure      401
// @Failure      403
// @Failure      404
// @Failure      500
// @Router       /queries/v2 [POST]
func QueriesV2Endpoint(router *httprouter.Router, config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, converter *converter.Converter, deviceSelectionClient deviceSelection.Client) {
	router.POST("/queries/v2", func(writer http.ResponseWriter, request *http.Request, params httprouter.Params) {
		start := time.Now()
		_, _, _, err := queriesParseQueryparams(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		timeFormat := request.URL.Query().Get("time_format")

		var requestElements []model.QueriesRequestElement
		err = json.NewDecoder(request.Body).Decode(&requestElements)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		for i := range requestElements {
			if !requestElements[i].Valid() {
				http.Error(writer, "Invalid request body", http.StatusBadRequest)
				return
			}
		}

		userId, ownerUserIds, err, code := queriesVerify(requestElements, request, start, verifier, config)
		if err != nil {
			http.Error(writer, err.Error(), code)
			return
		}
		forceTz := request.URL.Query().Get("force_tz")
		var forceTzp *string
		if len(forceTz) > 0 {
			forceTzp = &forceTz
		}

		raw, dbRequestElements, dbRequestIndices := queriesGetFromCache(requestElements, remoteCache, config, forceTzp)
		response := []model.QueriesV2ResponseElement{}
		for i, r := range raw {
			if len(r) != 0 {
				response = append(response, model.QueriesV2ResponseElement{
					RequestIndex: i,
					Data:         [][][]interface{}{r},
				})
			}
		}

		locateLat := request.URL.Query().Get("locate_lat")
		var locateLatFloat float64
		if len(locateLat) > 0 {
			locateLatFloat, err = strconv.ParseFloat(locateLat, 64)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
		}
		locateLon := request.URL.Query().Get("locate_lon")
		var locateLonFloat float64
		if len(locateLon) > 0 {
			locateLonFloat, err = strconv.ParseFloat(locateLon, 64)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
		}

		beforeQueries := time.Now()
		mux := sync.Mutex{}
		wg := sync.WaitGroup{}
		devices := []models.Device{}
		raised := false
		token := getToken(request)
		for i, dbRequestElement := range dbRequestElements {
			wg.Add(1)
			i := i
			dbRequestElement := dbRequestElement
			columnMatch := map[int]struct {
				selIdx int
				colIdx int
				elem   *model.QueriesRequestElement
			}{}
			go func() {
				defer wg.Done()
				dbRequestElements := []model.QueriesRequestElement{}
				if dbRequestElement.DeviceGroupId == nil && dbRequestElement.LocationId == nil {
					for colIdx, col := range dbRequestElement.Columns {
						filters := []model.QueriesRequestElementFilter{}
						if dbRequestElement.Filters != nil {
							filters = append(filters, *dbRequestElement.Filters...)
						}
						if dbRequestElement.ExportId != nil && len(locateLat) > 0 && len(locateLon) > 0 {
							token := getToken(request)
							importFilters, err := wrapper.CreateFiltersForImport(*dbRequestElement.ExportId, userId, token, locateLatFloat, locateLonFloat)
							if err != nil {
								http.Error(writer, err.Error(), http.StatusBadRequest)
								return
							}
							filters = append(filters, importFilters...)
						}

						if dbRequestElement.DeviceId != nil {
							device, err := remoteCache.GetDevice(*dbRequestElement.DeviceId, token)
							if err != nil {
								mux.Lock()
								defer mux.Unlock()
								if !raised {
									http.Error(writer, err.Error(), http.StatusInternalServerError)
									raised = true
								}
								return
							}
							mux.Lock()
							devices = append(devices, device)
							mux.Unlock()
						}

						elem := model.QueriesRequestElement{
							ExportId:         dbRequestElement.ExportId,
							DeviceId:         dbRequestElement.DeviceId,
							ServiceId:        dbRequestElement.ServiceId,
							Time:             dbRequestElement.Time,
							Limit:            dbRequestElement.Limit,
							Columns:          []model.QueriesRequestElementColumn{col},
							Filters:          &filters,
							GroupTime:        dbRequestElement.GroupTime,
							OrderColumnIndex: dbRequestElement.OrderColumnIndex,
							OrderDirection:   dbRequestElement.OrderDirection,
						}
						columnMatch[len(dbRequestElements)] = struct {
							selIdx int
							colIdx int
							elem   *model.QueriesRequestElement
						}{
							selIdx: 0,
							colIdx: colIdx,
							elem:   &elem,
						}
						dbRequestElements = append(dbRequestElements, elem)
						ownerUserIds = append(ownerUserIds, userId)
					}
				} else {
					deviceGroupIds := []string{}
					deviceIds := []string{}
					if dbRequestElement.DeviceGroupId != nil {
						deviceGroupIds = append(deviceGroupIds, *dbRequestElement.DeviceGroupId)
					}
					if dbRequestElement.LocationId != nil {
						location, err := remoteCache.GetLocation(*dbRequestElement.LocationId, token)
						if err != nil {
							mux.Lock()
							defer mux.Unlock()
							if !raised {
								http.Error(writer, err.Error(), http.StatusInternalServerError)
								raised = true
							}
							return
						}
						deviceIds = append(deviceIds, location.DeviceIds...)
						deviceGroupIds = append(deviceGroupIds, location.DeviceGroupIds...)
					}

					for _, deviceGroupid := range deviceGroupIds {
						deviceGroup, err := remoteCache.GetDeviceGroup(deviceGroupid, token)
						if err != nil {
							mux.Lock()
							defer mux.Unlock()
							if !raised {
								http.Error(writer, err.Error(), http.StatusInternalServerError)
								raised = true
							}
							return
						}
						deviceIds = append(deviceIds, deviceGroup.DeviceIds...)
					}
					for colIdx, col := range dbRequestElement.Columns {
						f, err := remoteCache.GetFunction(col.Criteria.FunctionId)
						if err != nil {
							mux.Lock()
							defer mux.Unlock()
							if !raised {
								http.Error(writer, err.Error(), code)
								raised = true
							}
							return
						}

						criteria := []models.DeviceGroupFilterCriteria{col.Criteria}

						selectables, code, err := remoteCache.GetSelectables(userId, token, criteria, &deviceSelection.GetSelectablesOptions{
							IncludeDevices:    true,
							WithDeviceIds:     deviceIds,
							IncludeIdModified: true,
						})
						if err != nil {
							mux.Lock()
							defer mux.Unlock()
							if !raised {
								http.Error(writer, err.Error(), code)
								raised = true
							}
							return
						}
						for selIdx, selectable := range selectables {
							if !slices.Contains(deviceIds, selectable.Device.Id) {
								continue //ensures only correctly modified device ids included
							}
							pureDeviceId, _ := idmodifier.SplitModifier(selectable.Device.Id)
							for serviceId, paths := range selectable.ServicePathOptions {
								serviceId := serviceId
								columns := []model.QueriesRequestElementColumn{}
								for _, path := range paths {
									columns = append(columns, model.QueriesRequestElementColumn{
										Name:                   path.Path,
										GroupType:              col.GroupType,
										Math:                   col.Math,
										SourceCharacteristicId: &path.CharacteristicId,
										TargetCharacteristicId: col.TargetCharacteristicId,
										ConceptId:              &f.ConceptId,
									})
								}
								elem := model.QueriesRequestElement{
									DeviceId:         &pureDeviceId,
									ServiceId:        &serviceId,
									Time:             dbRequestElement.Time,
									Limit:            dbRequestElement.Limit,
									Columns:          columns,
									Filters:          dbRequestElement.Filters,
									GroupTime:        dbRequestElement.GroupTime,
									OrderColumnIndex: dbRequestElement.OrderColumnIndex,
									OrderDirection:   dbRequestElement.OrderDirection,
								}
								columnMatch[len(dbRequestElements)] = struct {
									selIdx int
									colIdx int
									elem   *model.QueriesRequestElement
								}{
									selIdx: selIdx,
									colIdx: colIdx,
									elem:   &elem,
								}
								dbRequestElements = append(dbRequestElements, elem)
								ownerUserIds = append(ownerUserIds, userId)
							}
						}
					}
				}

				queries, err := wrapper.GenerateQueries(dbRequestElements, userId, ownerUserIds, forceTz, devices)
				if err != nil {
					mux.Lock()
					defer mux.Unlock()
					if !raised {
						http.Error(writer, err.Error(), http.StatusInternalServerError)
						raised = true
					}
					return
				}
				if config.Debug {
					log.Println("DEBUG: Query generation took " + time.Since(beforeQueries).String())
				}
				beforeQuery := time.Now()
				data, err := wrapper.ExecuteQueries(queries)
				if err != nil {
					mux.Lock()
					defer mux.Unlock()
					if !raised {
						http.Error(writer, err.Error(), timescale.GetHTTPErrorCode(err))
						raised = true
					}
					return
				}
				if config.Debug {
					log.Println("DEBUG: Fetching took " + time.Since(beforeQuery).String())
				}
				orderColumnIndex := 0
				if dbRequestElement.OrderColumnIndex != nil {
					orderColumnIndex = *dbRequestElement.OrderColumnIndex
				}
				orderDirection := model.Asc
				if dbRequestElement.OrderDirection != nil {
					orderDirection = *dbRequestElement.OrderDirection
				}

				subResponse, err := formatResponse(remoteCache, model.PerQuery, dbRequestElements, data, orderColumnIndex, orderDirection, timeFormat, converter)
				if err != nil {
					mux.Lock()
					defer mux.Unlock()
					if !raised {
						http.Error(writer, err.Error(), http.StatusInternalServerError)
						raised = true
					}
					return
				}
				subResponseCasted, ok := subResponse.([][][]interface{})
				if !ok {
					mux.Lock()
					defer mux.Unlock()
					if !raised {
						http.Error(writer, "could not cast subresponse", http.StatusInternalServerError)
						raised = true
					}
				}

				// merge DB results with remoteCache results
				mux.Lock()
				defer mux.Unlock()
				for j := range subResponseCasted {
					if len(columnMatch) == 0 {
						columnNames := []string{}
						for _, col := range dbRequestElement.Columns {
							columnNames = append(columnNames, col.Name)
						}
						response = append(response, model.QueriesV2ResponseElement{
							DeviceId:     dbRequestElement.DeviceId,
							ServiceId:    dbRequestElement.ServiceId,
							ExportId:     dbRequestElement.ExportId,
							ColumnNames:  columnNames,
							RequestIndex: dbRequestIndices[i],
							Data:         [][][]interface{}{subResponseCasted[j]},
						})
					} else {
						found := false
						respIdx := 0
						var respElem model.QueriesV2ResponseElement
						for k, r := range response {
							if r.RequestIndex == dbRequestIndices[i] && r.SelIdx == columnMatch[j].selIdx {
								found = true
								respElem = r
								respIdx = k
								break
							}
						}
						if !found {
							columnNames := []string{}
							for _, col := range columnMatch[j].elem.Columns {
								columnNames = append(columnNames, col.Name)
							}
							respElem = model.QueriesV2ResponseElement{
								RequestIndex: dbRequestIndices[i],
								DeviceId:     columnMatch[j].elem.DeviceId,
								ServiceId:    columnMatch[j].elem.ServiceId,
								ExportId:     columnMatch[j].elem.ExportId,
								ColumnNames:  columnNames,
								Data:         [][][]interface{}{},
							}
							respIdx = len(response)
							response = append(response, respElem)
						}
						for len(respElem.Data) <= columnMatch[j].colIdx {
							respElem.Data = append(respElem.Data, [][]interface{}{})
						}
						respElem.Data[columnMatch[j].colIdx] = subResponseCasted[j]
						response[respIdx] = respElem
					}
				}
			}()
		}
		wg.Wait()
		if raised {
			return
		}

		writer.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(writer).Encode(response)
		if err != nil {
			fmt.Println("ERROR: " + err.Error())
		}
	})

}
