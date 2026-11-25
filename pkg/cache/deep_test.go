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

package cache

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
)

// deepEntryFixture covers the shapes getDeepEntry walks: null values, nested lists and structures in lists.
const deepEntryFixture = `{
	"a": {"b": 1, "n": null, "s": "str"},
	"list": [10, null, 30],
	"nested": [[1, 2], [3], []],
	"structs": [{"v": 1, "n": null}, {"v": 2}, {"w": 3}, null],
	"empty": []
}`

func TestGetDeepEntryEdgeCases(t *testing.T) {
	log.InitForTest()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(deepEntryFixture), &m); err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]interface{}{
		// no wildcard: nil for missing and for null alike
		"a.b":            float64(1),
		"a.n":            nil,
		"a.missing":      nil,
		"missing":        nil,
		"missing.deeper": nil,
		"a.b.deeper":     nil, // below a scalar
		"a.s.0":          nil, // index into a string
		"list.0":         float64(10),
		"list.1":         nil,
		"list.2":         float64(30),
		"list.3":         nil, // out of range
		"list.-1":        nil, // negative
		"list.x":         nil, // not an index
		"list.":          nil,
		"nested.0.1":     float64(2),
		"nested.1.1":     nil,
		"nested.2.0":     nil,
		"structs.0.v":    float64(1),
		"structs.1.v":    float64(2),
		"structs.2.v":    nil,
		"structs.3.v":    nil, // below a null
		"empty.0":        nil,
		"a":              m["a"],
		"list":           m["list"],
		// wildcard: flat list of present values, null stays, missing is dropped
		"list.*":        []interface{}{float64(10), nil, float64(30)},
		"empty.*":       []interface{}{},
		"missing.*":     []interface{}{},
		"a.*":           []interface{}{}, // "*" on a map is no match
		"a.b.*":         []interface{}{},
		"nested.*":      []interface{}{[]interface{}{float64(1), float64(2)}, []interface{}{float64(3)}, []interface{}{}},
		"nested.*.*":    []interface{}{float64(1), float64(2), float64(3)},
		"nested.*.0":    []interface{}{float64(1), float64(3)},
		"nested.0.*":    []interface{}{float64(1), float64(2)},
		"nested.5.*":    []interface{}{},
		"structs.*.v":   []interface{}{float64(1), float64(2)},
		"structs.*.n":   []interface{}{nil},
		"structs.*.w":   []interface{}{float64(3)},
		"structs.*.v.x": []interface{}{},
		"structs.-1.*":  []interface{}{},
		"structs.x.*":   []interface{}{},
		"structs.0.*":   []interface{}{}, // "*" on a map
		"a.n.*":         []interface{}{}, // below a null
	} {
		if actual := getDeepEntry(m, path); !reflect.DeepEqual(actual, expected) {
			t.Errorf("%q: expected %#v, got %#v", path, expected, actual)
		}
	}
}
