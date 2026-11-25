/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package timescale

import (
	"testing"

	importModel "github.com/SENERGY-Platform/import-repository/lib/model"
)

func TestFindImportTypeContentVariable(t *testing.T) {
	tree := importModel.ContentVariable{Name: "value", SubContentVariables: []importModel.ContentVariable{
		{Name: "a", SubContentVariables: []importModel.ContentVariable{
			{Name: "x", CharacteristicId: "c1"},
			{Name: "deep", SubContentVariables: []importModel.ContentVariable{{Name: "y", UseAsTag: true, CharacteristicId: "c2"}}},
		}},
		{Name: "b", CharacteristicId: "c3"},
	}}
	for name, tc := range map[string]struct {
		find findImportTypeContentVariableSearchOptions
		want string // empty means not found
	}{
		"tag, depth first":     {findImportTypeContentVariableSearchOptions{identifiesMeasurement: true}, "value.a.deep.y"},
		"characteristic first": {findImportTypeContentVariableSearchOptions{characteristicId: "c1"}, "value.a.x"},
		"characteristic deep":  {findImportTypeContentVariableSearchOptions{characteristicId: "c2"}, "value.a.deep.y"},
		"characteristic last":  {findImportTypeContentVariableSearchOptions{characteristicId: "c3"}, "value.b"},
		"unknown":              {findImportTypeContentVariableSearchOptions{characteristicId: "nope"}, ""},
		"no criteria":          {findImportTypeContentVariableSearchOptions{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := findImportTypeContentVariable(tree, tc.find, "")
			if tc.want == "" {
				if err == nil {
					t.Errorf("expected an error, got %q", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("expected %q, got %q, %v", tc.want, got, err)
			}
		})
	}
	// the path prefix is prepended
	if got, err := findImportTypeContentVariable(importModel.ContentVariable{Name: "b", CharacteristicId: "c"}, findImportTypeContentVariableSearchOptions{characteristicId: "c"}, "p"); err != nil || got != "p.b" {
		t.Errorf("unexpected %q, %v", got, err)
	}
}
