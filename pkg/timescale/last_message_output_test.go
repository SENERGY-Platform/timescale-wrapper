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

package timescale

import (
	"reflect"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	util "github.com/SENERGY-Platform/timescale-tableworker/pkg/lib/handler"
)

func TestBuildOutput(t *testing.T) {
	// Same key form as buildOutput uses: the path after HashFieldNameIfNeeded, without quotes.
	key := func(path string) string {
		return strings.ReplaceAll(util.HashFieldNameIfNeeded(path), "\"", "")
	}
	longName := strings.Repeat("l", 70)

	tests := []struct {
		name    string
		cv      models.ContentVariable
		values  map[string]interface{}
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name: "structure with float and string",
			cv: models.ContentVariable{Name: "value", Type: models.Structure, SubContentVariables: []models.ContentVariable{
				{Name: "temp", Type: models.Float},
				{Name: "unit", Type: models.String},
			}},
			values: map[string]interface{}{key("value.temp"): 21.5, key("value.unit"): "C"},
			want: map[string]interface{}{"value": map[string]interface{}{
				"temp": 21.5,
				"unit": "C",
			}},
		},
		{
			name: "nested structure two levels",
			cv: models.ContentVariable{Name: "a", Type: models.Structure, SubContentVariables: []models.ContentVariable{
				{Name: "b", Type: models.Structure, SubContentVariables: []models.ContentVariable{
					{Name: "c", Type: models.Integer},
				}},
			}},
			values: map[string]interface{}{key("a.b.c"): 7.0},
			want: map[string]interface{}{"a": map[string]interface{}{
				"b": map[string]interface{}{"c": 7.0},
			}},
		},
		{
			name: "list with indices in reverse order keeps index order",
			cv: models.ContentVariable{Name: "values", Type: models.List, SubContentVariables: []models.ContentVariable{
				{Name: "1", Type: models.Float},
				{Name: "0", Type: models.Float},
			}},
			values: map[string]interface{}{key("values.0"): 1.0, key("values.1"): 2.0},
			want:   map[string]interface{}{"values": []interface{}{1.0, 2.0}},
		},
		{
			name: "list with wildcard sub reads the whole list value",
			cv: models.ContentVariable{Name: "values", Type: models.List, SubContentVariables: []models.ContentVariable{
				{Name: "*", Type: models.Float},
			}},
			values: map[string]interface{}{key("values"): []interface{}{1.0, 2.0, 3.0}},
			want:   map[string]interface{}{"values": []interface{}{1.0, 2.0, 3.0}},
		},
		{
			name: "list with non numeric sub name",
			cv: models.ContentVariable{Name: "values", Type: models.List, SubContentVariables: []models.ContentVariable{
				{Name: "x", Type: models.Float},
			}},
			values:  map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "list with index larger than list length",
			cv: models.ContentVariable{Name: "values", Type: models.List, SubContentVariables: []models.ContentVariable{
				{Name: "3", Type: models.Float},
			}},
			values:  map[string]interface{}{},
			wantErr: true,
		},
		{
			name:    "unknown content variable type",
			cv:      models.ContentVariable{Name: "value", Type: models.Type("foo")},
			values:  map[string]interface{}{},
			wantErr: true,
		},
		{
			name:   "missing value yields nil without error",
			cv:     models.ContentVariable{Name: "temp", Type: models.Float},
			values: map[string]interface{}{},
			want:   map[string]interface{}{"temp": nil},
		},
		{
			name:   "long field name is found under its hashed key",
			cv:     models.ContentVariable{Name: longName, Type: models.Float},
			values: map[string]interface{}{key(longName): 3.0},
			want:   map[string]interface{}{longName: 3.0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildOutput(tc.cv, tc.values, "")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got result %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
