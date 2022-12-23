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
	"github.com/SENERGY-Platform/converter/lib/converter"
	"github.com/SENERGY-Platform/device-repository/lib/client"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"reflect"
	"testing"
	"time"
)

func TestPostProcessing(t *testing.T) {
	t.Parallel()

	one := "1"
	two := "2"
	t1, _ := time.Parse(time.RFC3339, "2022-12-06T06:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2022-12-06T07:00:00+01:00")
	t.Run("Test Format as Table", func(t *testing.T) {
		t.Parallel()
		response, err := formatResponse(nil, model.Table, []model.QueriesRequestElement{{
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

	t.Run("Test With Conversions", func(t *testing.T) {
		t.Parallel()
		conf := configuration.ConfigStruct{}
		deviceRepo, testDb, err := client.NewTestClient()
		if err != nil {
			t.Fatal(err)
		}
		err = testDb.SetCharacteristic(nil, models.Characteristic{
			Id: "1",
		})
		if err != nil {
			t.Fatal(err)
		}
		err = testDb.SetCharacteristic(nil, models.Characteristic{
			Id: "2",
		})
		if err != nil {
			t.Fatal(err)
		}
		err = testDb.SetConcept(nil, models.Concept{
			Id: "1",
			Conversions: []models.ConverterExtension{{
				From:            "1",
				To:              "2",
				Formula:         "x * 10",
				PlaceholderName: "x",
			}},
			CharacteristicIds: []string{"1", "2"},
		})
		if err != nil {
			t.Fatal(err)
		}
		remoteCache := cache.NewRemote(&conf, deviceRepo)
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
			response, err := formatResponse(remoteCache, model.Table, request, [][][]interface{}{
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
			response, err := formatResponse(remoteCache, model.PerQuery, request, [][][]interface{}{
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
