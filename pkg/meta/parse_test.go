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

package meta

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func cv(name string, t models.Type, sub ...models.ContentVariable) models.ContentVariable {
	return models.ContentVariable{Name: name, Type: t, SubContentVariables: sub}
}

// outputs models the output of a SITEC ECM (a variable list of structures), a Tasmota LED (a
// variable list of integers), a fixed list and a list of lists.
func outputs() []models.Content {
	return []models.Content{{ContentVariable: cv("root", models.Structure,
		cv("level", models.Float),
		cv("module_1_processdata", models.List,
			cv("*", models.Structure,
				cv("station", models.Integer),
				cv("process", models.Integer),
				cv("part", models.Integer),
				cv("main_id", models.String),
				cv("errorcode", models.Integer),
				cv("result", models.Integer),
				cv("ok", models.Boolean),
				cv("ratio", models.Float),
				cv("tags", models.List, cv("0", models.String), cv("1", models.String)),
				cv("0", models.Integer), // a field name that looks like an index
			)),
		cv("Channel", models.List, cv("*", models.Integer)),
		cv("matrix", models.List, cv("*", models.List, cv("*", models.Float))),
		cv("fixed", models.List, cv("0", models.Structure, cv("value", models.Integer)), cv("1", models.Structure, cv("value", models.Integer))),
	)}}
}

func TestResolveColumn(t *testing.T) {
	tt := []struct {
		name     string
		column   string
		expected *model.JSONPath
	}{
		{name: "regular column", column: "root.level"},
		{name: "list as a whole", column: "root.module_1_processdata"},
		{name: "fixed list", column: "root.fixed.1.value"},
		{name: "index and field", column: "root.module_1_processdata.0.errorcode",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"0", "errorcode"}, SQLType: "bigint"}},
		{name: "text", column: "root.module_1_processdata.3.main_id",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"3", "main_id"}, SQLType: "text"}},
		{name: "boolean", column: "root.module_1_processdata.3.ok",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"3", "ok"}, SQLType: "boolean"}},
		{name: "float", column: "root.module_1_processdata.3.ratio",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"3", "ratio"}, SQLType: "double precision"}},
		{name: "element is a structure", column: "root.module_1_processdata.2",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"2"}}},
		{name: "wildcard", column: "root.module_1_processdata.*.result",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"*", "result"}, SQLType: "bigint"}},
		{name: "wildcard on the elements", column: "root.module_1_processdata.*",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"*"}}},
		{name: "fixed list inside an element", column: "root.module_1_processdata.*.tags.1",
			expected: &model.JSONPath{Column: "root.module_1_processdata", Segments: []string{"*", "tags", "1"}, SQLType: "text"}},
		{name: "list of integers", column: "root.Channel.1",
			expected: &model.JSONPath{Column: "root.Channel", Segments: []string{"1"}, SQLType: "bigint"}},
		{name: "nested lists", column: "root.matrix.1.*",
			expected: &model.JSONPath{Column: "root.matrix", Segments: []string{"1", "*"}, SQLType: "double precision"}},
		{name: "nested lists with a single index", column: "root.matrix.1.2",
			expected: &model.JSONPath{Column: "root.matrix", Segments: []string{"1", "2"}, SQLType: "double precision"}},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := ResolveColumn(outputs(), tc.column)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("expected %#v, got %#v", tc.expected, actual)
			}
		})
	}
}

func TestResolveColumnErrors(t *testing.T) {
	for _, column := range []string{
		"unknown.x",
		"root.unknown",
		"root.fixed.2.value",
		"root.level.0",
		"root.module_1_processdata.-1.errorcode",
		"root.module_1_processdata.errorcode",
		"root.module_1_processdata.0.unknown",
		"root.module_1_processdata.0.errorcode.x",
		"root.module_1_processdata.0.0", // digits as a field name are not addressable
		"root.module_1_processdata.*.*",
		"root.module_1_processdata.0.tags.2",
		"root.module_1_processdata.0.tags.x",
		"root.module_1_processdata.1234567890.errorcode",
		"root.Channel.0.x",
		"root.Channel.a",
		"root.module_1_processdata.a*.result",
		"root..x",
		"root.module_1_processdata.0.",
	} {
		t.Run(column, func(t *testing.T) {
			actual, err := ResolveColumn(outputs(), column)
			if err == nil {
				t.Fatalf("expected an error, got %#v", actual)
			}
			if !errors.Is(err, model.ErrBadRequest) {
				t.Errorf("expected bad request, got %v", err)
			}
		})
	}
	t.Run("unsupported type", func(t *testing.T) {
		out := []models.Content{{ContentVariable: cv("root", models.List, cv("*", models.Type("x")))}}
		_, err := ResolveColumn(out, "root.0")
		if err == nil || !errors.Is(err, model.ErrBadRequest) || !strings.Contains(err.Error(), "unsupported type") {
			t.Errorf("unexpected error %v", err)
		}
	})
	t.Run("no outputs", func(t *testing.T) {
		if _, err := ResolveColumn(nil, "root.0"); !errors.Is(err, model.ErrBadRequest) {
			t.Errorf("unexpected error %v", err)
		}
	})
}

func TestGetDeepContentVariable(t *testing.T) {
	root := outputs()[0].ContentVariable
	for path, expected := range map[string]string{
		"module_1_processdata.0.errorcode": "errorcode",
		"module_1_processdata.*.result":    "result",
		"module_1_processdata.12":          "*",
		"Channel.3":                        "*",
		"fixed.1.value":                    "value",
		"level":                            "level",
	} {
		actual := GetDeepContentVariable(root, strings.Split(path, "."))
		if actual == nil || actual.Name != expected {
			t.Errorf("%s: unexpected %#v", path, actual)
		}
	}
	for _, path := range []string{"module_1_processdata.-1.errorcode", "module_1_processdata.x.errorcode", "module_1_processdata.0.unknown", "unknown"} {
		if actual := GetDeepContentVariable(root, strings.Split(path, ".")); actual != nil {
			t.Errorf("%s: expected nil, got %#v", path, actual)
		}
	}
}
