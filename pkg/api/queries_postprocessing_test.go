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

func TestPostProcessing(t *testing.T) {
	t.Parallel()
	log.InitForTest()

	one := "1"
	two := "2"
	t1, _ := time.Parse(time.RFC3339, "2022-12-06T06:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2022-12-06T07:00:00+01:00")
	t.Run("Test Format as Table", func(t *testing.T) {
		t.Parallel()
		response, err := formatResponse(context.Background(), nil, model.Table, []model.QueriesRequestElement{{
			ExportId: &one,
			Columns:  []model.QueriesRequestElementColumn{{Name: one}},
		}, {
			ExportId: &two,
			Columns:  []model.QueriesRequestElementColumn{{Name: two}},
		}}, [][][]interface{}{
			{{t1, 1}},
			{{t2, 2}},
		}, 0, model.Asc, "", nil)
		if err != nil {
			t.Error(t)
		}
		res, _ := time.Parse(time.RFC3339, "2022-12-06T06:00:00Z")
		if !reflect.DeepEqual(response, [][]interface{}{{res, 1, 2}}) { //
			t.Error("unexpected result")
		}
	})

	// LIMIT is applied again in the post-processing, on a result set that may be shorter than
	// the caller asked for. Slicing to the limit without a length check panicked the process.
	t.Run("Test Limit", func(t *testing.T) {
		t.Parallel()
		request := func(limit int) []model.QueriesRequestElement {
			return []model.QueriesRequestElement{{
				ExportId: &one,
				Columns:  []model.QueriesRequestElementColumn{{Name: one}},
				Limit:    &limit,
			}}
		}
		results := func() [][][]interface{} {
			return [][][]interface{}{{{t1, 1}, {t2, 2}}}
		}

		t.Run("larger than the rows available", func(t *testing.T) {
			t.Parallel()
			response, err := formatResponse(context.Background(), nil, model.PerQuery, request(25000), results(), 0, model.Asc, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(response, [][][]interface{}{{{t1, 1}, {t2, 2}}}) {
				t.Errorf("unexpected result: %v", response)
			}
		})

		t.Run("smaller than the rows available", func(t *testing.T) {
			t.Parallel()
			response, err := formatResponse(context.Background(), nil, model.PerQuery, request(1), results(), 0, model.Asc, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(response, [][][]interface{}{{{t1, 1}}}) {
				t.Errorf("unexpected result: %v", response)
			}
		})
	})

	t.Run("Test With Conversions", func(t *testing.T) {
		t.Parallel()
		log.InitForTest()
		conf := configuration.ConfigStruct{}
		deviceRepo, testDb, err := client.NewTestClient()
		if err != nil {
			t.Fatal(err)
		}
		err = testDb.SetCharacteristic(context.Background(), models.Characteristic{
			Id: "1",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = testDb.SetCharacteristic(context.Background(), models.Characteristic{
			Id: "2",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = testDb.SetConcept(context.Background(), models.Concept{
			Id: "1",
			Conversions: []models.ConverterExtension{{
				From:            "1",
				To:              "2",
				Formula:         "x * 10",
				PlaceholderName: "x",
			}},
			CharacteristicIds: []string{"1", "2"},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		remoteCache := cache.NewRemote(&conf, deviceRepo, deviceSelection.NewTestClient())
		conv, err := converter.New()
		if err != nil {
			t.Fatal(err)
		}
		request := []model.QueriesRequestElement{{
			ExportId: &one,
			Columns:  []model.QueriesRequestElementColumn{{Name: one, SourceCharacteristicId: &one, TargetCharacteristicId: &two, ConceptId: &one}},
		}, {
			ExportId: &two,
			Columns:  []model.QueriesRequestElementColumn{{Name: two}},
		}}
		t.Run("as Table", func(t *testing.T) {
			t.Parallel()
			response, err := formatResponse(context.Background(), remoteCache, model.Table, request, [][][]interface{}{
				{{t1, 1}},
				{{t2, 2}},
			}, 0, model.Asc, "", conv)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := json.Marshal(response)
			if string(actual) != "[[\"2022-12-06T06:00:00Z\",10,2]]" {
				t.Fatal("unexpected result")
			}
		})
		t.Run("per Query", func(t *testing.T) {
			t.Parallel()
			response, err := formatResponse(context.Background(), remoteCache, model.PerQuery, request, [][][]interface{}{
				{{t1, 1}},
				{{t2, 2}},
			}, 0, model.Asc, "", conv)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := json.Marshal(response)
			if string(actual) != "[[[\"2022-12-06T06:00:00Z\",10]],[[\"2022-12-06T07:00:00+01:00\",2]]]" {
				t.Fatal("unexpected result")
			}
		})
	})
}
