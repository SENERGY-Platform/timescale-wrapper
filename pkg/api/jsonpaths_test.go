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
	"reflect"
	"testing"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	"github.com/SENERGY-Platform/device-repository/lib/client"
	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

const (
	listTestDeviceId  = "urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4"
	listTestServiceId = "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050"
)

func listTestCache(t *testing.T) *cache.RemoteCache {
	t.Helper()
	log.InitForTest()
	deviceRepo, testDb, err := client.NewTestClient()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1", "2"} {
		if err = testDb.SetCharacteristic(context.Background(), models.Characteristic{Id: id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	err = testDb.SetConcept(context.Background(), models.Concept{
		Id:                "1",
		Conversions:       []models.ConverterExtension{{From: "1", To: "2", Formula: "x * 10", PlaceholderName: "x"}},
		CharacteristicIds: []string{"1", "2"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = testDb.SetDeviceType(context.Background(), models.DeviceType{
		Id: "dt",
		Services: []models.Service{{
			Id: listTestServiceId,
			Outputs: []models.Content{{
				ContentVariable: models.ContentVariable{
					Name: "root",
					Type: models.Structure,
					SubContentVariables: []models.ContentVariable{
						{Name: "level", Type: models.Float, CharacteristicId: "1"},
						{Name: "fixed", Type: models.List, SubContentVariables: []models.ContentVariable{
							{Name: "0", Type: models.Integer, CharacteristicId: "1"},
						}},
						{Name: "list", Type: models.List, SubContentVariables: []models.ContentVariable{
							{Name: "*", Type: models.Structure, SubContentVariables: []models.ContentVariable{
								{Name: "errorcode", Type: models.Integer, CharacteristicId: "1"},
								{Name: "result", Type: models.Integer, CharacteristicId: "1"},
							}},
						}},
					},
				},
			}},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return cache.NewRemote(&configuration.ConfigStruct{}, deviceRepo, deviceSelection.NewTestClient())
}

func TestResolveJSONPaths(t *testing.T) {
	remoteCache := listTestCache(t)
	device, service := listTestDeviceId, listTestServiceId
	export := "export"
	sum := "sum"
	group := "1h"

	t.Run("columns and filters", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			DeviceId:  &device,
			ServiceId: &service,
			GroupTime: &group,
			Columns: []model.QueriesRequestElementColumn{
				{Name: "root.list.0.errorcode"},
				{Name: "root.list.*.result", GroupType: &sum},
				{Name: "root.level"},
				{Name: "root.fixed.0"},
			},
			Filters: &[]model.QueriesRequestElementFilter{{Column: "root.list.1.result", Type: "!=", Value: nil}, {Column: "root.list.0.errorcode", Type: "=", Value: 1}},
		}}
		if err := resolveJSONPaths(context.Background(), remoteCache, elements); err != nil {
			t.Fatal(err)
		}
		expected := map[string]model.JSONPath{
			"root.list.0.errorcode": {Column: "root.list", Segments: []string{"0", "errorcode"}, SQLType: "bigint"},
			"root.list.*.result":    {Column: "root.list", Segments: []string{"*", "result"}, SQLType: "bigint"},
			"root.list.1.result":    {Column: "root.list", Segments: []string{"1", "result"}, SQLType: "bigint"},
		}
		if !reflect.DeepEqual(elements[0].JSONPaths, expected) {
			t.Errorf("unexpected paths %#v", elements[0].JSONPaths)
		}
	})

	t.Run("regular columns need no paths", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			DeviceId:  &device,
			ServiceId: &service,
			Columns:   []model.QueriesRequestElementColumn{{Name: "root.level"}, {Name: "root.fixed.0"}},
		}}
		if err := resolveJSONPaths(context.Background(), remoteCache, elements); err != nil {
			t.Fatal(err)
		}
		if elements[0].JSONPaths != nil {
			t.Errorf("unexpected paths %#v", elements[0].JSONPaths)
		}
	})

	t.Run("not in the service", func(t *testing.T) {
		for _, name := range []string{"root.list.0.unknown", "root.list.-1.result", "root.list.result", "root.fixed.1", "root.level.0", "other.list.0.result"} {
			elements := []model.QueriesRequestElement{{
				DeviceId:  &device,
				ServiceId: &service,
				Columns:   []model.QueriesRequestElementColumn{{Name: name}},
			}}
			// names without an index or wildcard are not looked up, see listPathNames
			err := resolveJSONPaths(context.Background(), remoteCache, elements)
			if name == "root.list.result" {
				if err != nil {
					t.Errorf("%s: unexpected error %v", name, err)
				}
				continue
			}
			if err == nil || model.GetStatusCode(err) != 400 {
				t.Errorf("%s: expected a bad request, got %v", name, err)
			}
		}
	})

	// criteria columns are created after Valid ran, so resolveJSONPaths has to apply the rules
	t.Run("wildcard rules for generated columns", func(t *testing.T) {
		plus, twmean, target, concept := "+5", "time-weighted-mean-linear", "2", "1"
		for name, column := range map[string]model.QueriesRequestElementColumn{
			"math without group type":   {Name: "root.list.*.result", Math: &plus},
			"target without group type": {Name: "root.list.*.result", TargetCharacteristicId: &target, ConceptId: &concept},
			"time weighted":             {Name: "root.list.*.result", GroupType: &twmean},
		} {
			elements := []model.QueriesRequestElement{{DeviceId: &device, ServiceId: &service, GroupTime: &group, Columns: []model.QueriesRequestElementColumn{column}}}
			err := resolveJSONPaths(context.Background(), remoteCache, elements)
			if err == nil || model.GetStatusCode(err) != 400 {
				t.Errorf("%s: expected a bad request, got %v", name, err)
			}
		}
		elements := []model.QueriesRequestElement{{DeviceId: &device, ServiceId: &service, GroupTime: &group,
			Columns: []model.QueriesRequestElementColumn{{Name: "root.list.*.result", GroupType: &sum, Math: &plus, TargetCharacteristicId: &target, ConceptId: &concept}}}}
		if err := resolveJSONPaths(context.Background(), remoteCache, elements); err != nil {
			t.Errorf("aggregated wildcard column: %v", err)
		}
	})

	t.Run("unknown service", func(t *testing.T) {
		unknown := "urn:infai:ses:service:00000000-0000-0000-0000-000000000000"
		elements := []model.QueriesRequestElement{{
			DeviceId:  &device,
			ServiceId: &unknown,
			Columns:   []model.QueriesRequestElementColumn{{Name: "root.list.0.errorcode"}},
		}}
		err := resolveJSONPaths(context.Background(), remoteCache, elements)
		if err == nil || model.GetStatusCode(err) != 500 {
			t.Errorf("expected an internal server error, got %v", err)
		}
	})

	t.Run("wildcard on an export", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			ExportId: &export,
			Columns:  []model.QueriesRequestElementColumn{{Name: "root.list.*.result", GroupType: &sum}},
		}}
		err := resolveJSONPaths(context.Background(), remoteCache, elements)
		if err == nil || model.GetStatusCode(err) != 400 {
			t.Errorf("expected a bad request, got %v", err)
		}
	})

	t.Run("index on an export is a column name", func(t *testing.T) {
		elements := []model.QueriesRequestElement{{
			ExportId: &export,
			Columns:  []model.QueriesRequestElementColumn{{Name: "values.0"}},
		}}
		if err := resolveJSONPaths(context.Background(), remoteCache, elements); err != nil {
			t.Fatal(err)
		}
		if elements[0].JSONPaths != nil {
			t.Errorf("unexpected paths %#v", elements[0].JSONPaths)
		}
	})
}

// The conversion of a value inside a list needs the characteristic of its variable.
func TestPostProcessingListElementConversion(t *testing.T) {
	remoteCache := listTestCache(t)
	conv, err := converter.New()
	if err != nil {
		t.Fatal(err)
	}
	service, target, concept := listTestServiceId, "2", "1"
	ts, _ := time.Parse(time.RFC3339, "2022-12-06T06:00:00Z")
	for _, name := range []string{"root.list.0.errorcode", "root.list.*.errorcode"} {
		t.Run(name, func(t *testing.T) {
			groupType := "max"
			request := []model.QueriesRequestElement{{
				ServiceId: &service,
				Columns:   []model.QueriesRequestElementColumn{{Name: name, GroupType: &groupType, TargetCharacteristicId: &target, ConceptId: &concept}},
			}}
			response, err := formatResponse(context.Background(), remoteCache, model.PerQuery, request, [][][]interface{}{{{ts, 3}}}, 0, model.Asc, "", conv)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := json.Marshal(response)
			if string(actual) != `[[["2022-12-06T06:00:00Z",30]]]` {
				t.Errorf("unexpected result %s", actual)
			}
		})
	}
}
