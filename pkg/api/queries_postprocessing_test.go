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
	"fmt"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"reflect"
	"testing"
	"time"
)

func TestFormatTable(t *testing.T) {
	one := "1"
	two := "2"
	t1, _ := time.Parse(time.RFC3339, "2022-12-06T06:00:00Z")
	t2, _ := time.Parse(time.RFC3339, "2022-12-06T07:00:00+01:00")
	fmt.Println(t1.Equal(t2))
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
}
