/*
 *    Copyright 2026 InfAI (CC SES)
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
	"slices"

	"github.com/SENERGY-Platform/device-repository/lib/idmodifier"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	dsmodel "github.com/SENERGY-Platform/device-selection/pkg/model"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
)

// subQuery is one element that is queried on behalf of a request element of /queries/v2.
type subQuery struct {
	element model.QueriesRequestElement
	// requestIndex is the index of the request element the result belongs to.
	requestIndex int
	// columnIndex is the position in the data of the response element at which the series is filed.
	columnIndex int
	// allColumns is set if element holds all columns of the request element and one query answers all of them.
	// The rows are then split per column, and the series of column i is filed at i.
	allColumns bool
}

// v2Series is the time series of one column or criteria, ready to be filed with mergeQueriesV2Series.
type v2Series struct {
	requestIndex int
	element      model.QueriesRequestElement
	columnIndex  int
	rows         [][]interface{}
}

// v2Params are the parameters of a /queries/v2 request that are the same for all of its elements.
type v2Params struct {
	userId     string
	token      string
	forceTz    string
	timeFormat string
	// locate is set if the request has locate_lat and locate_lon, as {lat, lon}.
	locate *[2]float64
}

// v2Handler holds what the steps of /queries/v2 call. Generation, execution and formatting come from the pipeline.
type v2Handler struct {
	*pipeline
	timescale *timescale.Wrapper // creates the filters of a located import
	verifier  *verification.Verifier
}

// combinable reports whether one query answers all columns of a request element with the rows each column's own
// query returns. That holds for a raw element (no group time: the buckets of grouped columns are full outer joined,
// so the rows would differ) without a limit (a limit cuts the rows of the combined query, not of each column) that
// is ordered by time or not at all (an order by another column refers to the position in the select list, which
// differs). The filters and the time window apply to the rows, never to a column.
func combinable(element model.QueriesRequestElement) bool {
	if element.GroupTime != nil || element.Limit != nil || len(element.Columns) < 2 {
		return false
	}
	return element.OrderColumnIndex == nil || *element.OrderColumnIndex == 0
}

// expandDirect expands a request element of a device or an export. The filters of a located import and the device
// are read once per element. A raw element without limit is queried at once, any other per column.
func (h *v2Handler) expandDirect(ctx context.Context, params v2Params, element model.QueriesRequestElement, requestIndex int) (subs []subQuery, devices []models.Device, err error) {
	filters := []model.QueriesRequestElementFilter{}
	if element.Filters != nil {
		filters = append(filters, *element.Filters...)
	}
	if element.ExportId != nil && params.locate != nil {
		importFilters, err := h.timescale.CreateFiltersForImport(ctx, *element.ExportId, params.userId, params.token, params.locate[0], params.locate[1])
		if err != nil {
			return nil, nil, errors.Join(err, model.ErrBadRequest)
		}
		filters = append(filters, importFilters...)
	}
	devices = []models.Device{}
	if element.DeviceId != nil {
		device, err := h.remoteCache.GetDevice(ctx, *element.DeviceId, params.token)
		if err != nil {
			return nil, nil, classify(err, model.ErrInternalServerError)
		}
		devices = append(devices, device)
	}

	sub := model.QueriesRequestElement{
		ExportId:         element.ExportId,
		DeviceId:         element.DeviceId,
		ServiceId:        element.ServiceId,
		Time:             element.Time,
		Limit:            element.Limit,
		Columns:          element.Columns,
		Filters:          &filters,
		GroupTime:        element.GroupTime,
		OrderColumnIndex: element.OrderColumnIndex,
		OrderDirection:   element.OrderDirection,
	}
	if combinable(element) {
		return []subQuery{{element: sub, requestIndex: requestIndex, allColumns: true}}, devices, nil
	}
	return perColumnSubs(sub, requestIndex), devices, nil
}

// perColumnSubs splits an element into one sub query per column.
func perColumnSubs(element model.QueriesRequestElement, requestIndex int) (subs []subQuery) {
	for columnIndex, column := range element.Columns {
		single := element
		single.Columns = []model.QueriesRequestElementColumn{column}
		subs = append(subs, subQuery{element: single, requestIndex: requestIndex, columnIndex: columnIndex})
	}
	return subs
}

// expandCriteria expands a request element of a device group or a location: the columns are criteria, and every
// device that matches one gets a sub query per service. Functions and selectables are read once per distinct
// function and criteria.
func (h *v2Handler) expandCriteria(ctx context.Context, params v2Params, element model.QueriesRequestElement, requestIndex int) (subs []subQuery, devices []models.Device, err error) {
	deviceGroupIds := []string{}
	deviceIds := []string{}
	if element.DeviceGroupId != nil {
		deviceGroupIds = append(deviceGroupIds, *element.DeviceGroupId)
	}
	if element.LocationId != nil {
		location, err := h.remoteCache.GetLocation(ctx, *element.LocationId, params.token)
		if err != nil {
			return nil, nil, classify(err, model.ErrInternalServerError)
		}
		deviceIds = append(deviceIds, location.DeviceIds...)
		err = verifyDerivedDeviceGroups(ctx, location.DeviceGroupIds, func(ctx context.Context, id string) (bool, error) {
			entry, err := h.verifier.VerifyAccessOnce(ctx, model.QueriesRequestElement{DeviceGroupId: &id}, params.token, params.userId)
			return entry.Ok, err
		})
		if err != nil {
			return nil, nil, err
		}
		deviceGroupIds = append(deviceGroupIds, location.DeviceGroupIds...)
	}
	for _, deviceGroupId := range deviceGroupIds {
		deviceGroup, err := h.remoteCache.GetDeviceGroup(ctx, deviceGroupId, params.token)
		if err != nil {
			return nil, nil, classify(err, model.ErrInternalServerError)
		}
		deviceIds = append(deviceIds, deviceGroup.DeviceIds...)
	}

	type selectablesResult struct {
		selectables []dsmodel.Selectable
		code        int
		err         error
	}
	functions := map[string]models.Function{}
	selectablesByCriteria := map[string]selectablesResult{}
	for columnIndex, column := range element.Columns {
		f, ok := functions[column.Criteria.FunctionId]
		if !ok {
			f, err = h.remoteCache.GetFunction(ctx, column.Criteria.FunctionId)
			if err != nil {
				return nil, nil, classify(err, model.ErrInternalServerError)
			}
			functions[column.Criteria.FunctionId] = f
		}

		// the options are the same for all columns, so the criteria are the key
		key, keyErr := json.Marshal(column.Criteria)
		result, ok := selectablesByCriteria[string(key)]
		if !ok || keyErr != nil {
			result.selectables, result.code, result.err = h.remoteCache.GetSelectables(ctx, params.userId, params.token, []models.DeviceGroupFilterCriteria{column.Criteria}, &deviceSelection.GetSelectablesOptions{
				IncludeDevices:    true,
				WithDeviceIds:     deviceIds,
				IncludeIdModified: true,
			})
			if keyErr == nil {
				selectablesByCriteria[string(key)] = result
			}
		}
		if result.err != nil {
			return nil, nil, classify(result.err, model.GetError(result.code))
		}
		for _, selectable := range result.selectables {
			if !slices.Contains(deviceIds, selectable.Device.Id) {
				continue // ensures only correctly modified device ids included
			}
			pureDeviceId, _ := idmodifier.SplitModifier(selectable.Device.Id)
			for serviceId, paths := range selectable.ServicePathOptions {
				columns := []model.QueriesRequestElementColumn{}
				for _, path := range paths {
					columns = append(columns, model.QueriesRequestElementColumn{
						Name:                   path.Path,
						GroupType:              column.GroupType,
						Math:                   column.Math,
						SourceCharacteristicId: &path.CharacteristicId,
						TargetCharacteristicId: column.TargetCharacteristicId,
						ConceptId:              &f.ConceptId,
					})
				}
				subs = append(subs, subQuery{
					element: model.QueriesRequestElement{
						DeviceId:         &pureDeviceId,
						ServiceId:        &serviceId,
						Time:             element.Time,
						Limit:            element.Limit,
						Columns:          columns,
						Filters:          element.Filters,
						GroupTime:        element.GroupTime,
						OrderColumnIndex: element.OrderColumnIndex,
						OrderDirection:   element.OrderDirection,
					},
					requestIndex: requestIndex,
					columnIndex:  columnIndex,
				})
			}
		}
	}
	return subs, []models.Device{}, nil
}

// querySubs resolves the json paths of the sub queries, generates and executes their queries and formats the
// result into one series per sub query, or per column for a sub query of all columns. The series are in the order
// of the sub queries. requestElement is the element they were expanded from, ownerUserId its owner.
func (h *v2Handler) querySubs(ctx context.Context, params v2Params, requestElement model.QueriesRequestElement, ownerUserId string, subs []subQuery, devices []models.Device) ([]v2Series, error) {
	elements := make([]model.QueriesRequestElement, len(subs))
	for i := range subs {
		elements[i] = subs[i].element
	}
	err := h.resolvePaths(ctx, elements)
	if err != nil {
		return nil, err
	}
	queries, err := h.wrapper.GenerateQueries(ctx, elements, params.userId, slices.Repeat([]string{ownerUserId}, len(elements)), params.forceTz, devices)
	if err != nil {
		return nil, classify(err, model.ErrInternalServerError)
	}
	data, err := h.wrapper.ExecuteQueries(ctx, queries)
	if err != nil {
		return nil, classifyDB(err)
	}

	orderColumnIndex, orderDirection := v2Order(requestElement)
	var series []v2Series
	for i, sub := range subs {
		single := func(element model.QueriesRequestElement, columnIndex int, rows [][]interface{}) error {
			formatted, err := h.format(ctx, model.PerQuery, []model.QueriesRequestElement{element}, [][][]interface{}{rows}, orderColumnIndex, orderDirection, params.timeFormat)
			if err != nil {
				return err
			}
			casted, ok := formatted.([][][]interface{})
			if !ok || len(casted) != 1 {
				return errors.Join(errors.New("could not cast subresponse"), model.ErrInternalServerError)
			}
			series = append(series, v2Series{requestIndex: sub.requestIndex, element: element, columnIndex: columnIndex, rows: casted[0]})
			return nil
		}
		if !sub.allColumns {
			if err := single(sub.element, sub.columnIndex, data[i]); err != nil {
				return nil, err
			}
			continue
		}
		for columnIndex, column := range sub.element.Columns {
			columnElement := sub.element
			columnElement.Columns = []model.QueriesRequestElementColumn{column}
			rows := make([][]interface{}, 0, len(data[i]))
			for _, row := range data[i] {
				if len(row) != len(sub.element.Columns)+1 {
					return nil, errors.Join(errors.New("row does not match the requested columns"), model.ErrInternalServerError)
				}
				rows = append(rows, []interface{}{row[0], row[columnIndex+1]})
			}
			if err := single(columnElement, columnIndex, rows); err != nil {
				return nil, err
			}
		}
	}
	return series, nil
}
