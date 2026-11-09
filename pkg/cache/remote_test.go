/*
 * Copyright 2022 InfAI (CC SES)
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
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestDeepEntryInLists(t *testing.T) {
	var entry Entry
	err := json.Unmarshal([]byte("{\"time\":\"1970-01-01T00:00:13Z\",\"value\":{\"metrics\":{\"level\":42,\"level_unit\":\"test2\",\"listfixed\":[{\"value\":12,\"value2\":34},{\"value\":56,\"value2\":78}],\"listvariable\":[{\"value\":12,\"value2\":34},{\"value\":56,\"value2\":78},{\"value\":90,\"value2\":12}],\"title\":\"event\",\"updateTime\":13},\"other_var\":\"foo\"}}"), &entry)
	if err != nil {
		t.Fatal(err)
		return
	}

	value := getDeepEntry(entry.Value, "metrics.listfixed.1.value2")
	expected := entry.Value["metrics"].(map[string]interface{})["listfixed"].([]interface{})[1].(map[string]interface{})["value2"]
	if !reflect.DeepEqual(value, expected) {
		t.Errorf("unexpected value %v != %v", value, expected)
		return
	}

	value = getDeepEntry(entry.Value, "metrics.listvariable")
	expected = entry.Value["metrics"].(map[string]interface{})["listvariable"]
	if !reflect.DeepEqual(value, expected) {
		t.Errorf("unexpected value %v != %v", value, expected)
		return
	}

	value = getDeepEntry(entry.Value, "metrics.level")
	expected = entry.Value["metrics"].(map[string]interface{})["level"]
	if !reflect.DeepEqual(value, expected) {
		t.Errorf("unexpected value %v != %v", value, expected)
		return
	}
}

// Index paths come from the request. An index outside the list used to panic inside a
// goroutine, and a path continuing below a scalar returned the scalar.
func TestDeepEntryBounds(t *testing.T) {
	log.InitForTest()
	var entry Entry
	err := json.Unmarshal([]byte(`{"time":"1970-01-01T00:00:13Z","value":{"root":{"level":42,"list":[{"a":1},{"a":2}],"empty":[]}}}`), &entry)
	if err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]interface{}{
		"root.list.0.a":       float64(1),
		"root.list.1.a":       float64(2),
		"root.list.2.a":       nil,
		"root.list.2":         nil,
		"root.list.-1":        nil,
		"root.list.-1.a":      nil,
		"root.list.x":         nil,
		"root.empty.0":        nil,
		"root.level.a":        nil,
		"root.level.0":        nil,
		"root.list.0.a.b":     nil,
		"root.list.0.missing": nil,
	} {
		if value := getDeepEntry(entry.Value, path); !reflect.DeepEqual(value, expected) {
			t.Errorf("%s: unexpected value %#v != %#v", path, value, expected)
		}
	}
}

// "*" collects the values of all list elements flat and leaves out elements without one, like
// jsonb_path_query_array does for the database.
func TestDeepEntryWildcard(t *testing.T) {
	log.InitForTest()
	var entry Entry
	err := json.Unmarshal([]byte(`{"time":"1970-01-01T00:00:13Z","value":{"root":{
		"list":[{"a":1,"tags":["x","y"]},{"b":2},{"a":3,"tags":[]},{"a":null}],
		"matrix":[[1,2],[3],[]],
		"empty":[],
		"level":42,"nulls":[1,null,3]}}}`), &entry)
	if err != nil {
		t.Fatal(err)
	}
	for path, expected := range map[string]interface{}{
		"root.list.*.a":       []interface{}{float64(1), float64(3), nil},
		"root.list.*.b":       []interface{}{float64(2)},
		"root.list.*.missing": []interface{}{},
		"root.list.*.tags":    []interface{}{[]interface{}{"x", "y"}, []interface{}{}},
		"root.list.*.tags.*":  []interface{}{"x", "y"},
		"root.list.*.tags.1":  []interface{}{"y"},
		"root.matrix.*.*":     []interface{}{float64(1), float64(2), float64(3)},
		"root.matrix.*.0":     []interface{}{float64(1), float64(3)},
		"root.matrix.1.*":     []interface{}{float64(3)},
		"root.nulls.*":        []interface{}{float64(1), nil, float64(3)},
		"root.empty.*":        []interface{}{},
		"root.level.*":        []interface{}{},
		"root.unknown.*":      []interface{}{},
		"root.list.5.*":       []interface{}{},
		"root.list.-1.*":      []interface{}{},
		"root.list.*.a.*":     []interface{}{},
		"root.list.*":         []interface{}{map[string]interface{}{"a": float64(1), "tags": []interface{}{"x", "y"}}, map[string]interface{}{"b": float64(2)}, map[string]interface{}{"a": float64(3), "tags": []interface{}{}}, map[string]interface{}{"a": nil}},
	} {
		if value := getDeepEntry(entry.Value, path); !reflect.DeepEqual(value, expected) {
			t.Errorf("%s: unexpected value %#v != %#v", path, value, expected)
		}
	}
}

// A failed memcached call replaces the client while other requests use it.
// Only meaningful with -race.
func TestConcurrentMemcachedReinit(t *testing.T) {
	log.InitForTest()
	rc := NewRemote(&configuration.ConfigStruct{MemcachedUrls: []string{"127.0.0.1:1"}}, nil, nil) // nothing listens, every call fails
	wg := sync.WaitGroup{}
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rc.GetLastMessageFromCache(context.Background(), "device", "service")
		}()
	}
	wg.Wait()
}

// The same instant must come out of the cache as the database returns it: in the local time zone,
// whatever offset the writer of the entry used.
func TestLastValuesRowTimeZone(t *testing.T) {
	var entry Entry
	err := json.Unmarshal([]byte(`{"time":"2022-12-06T07:00:00+01:00","value":{"root":{"level":42}}}`), &entry)
	if err != nil {
		t.Fatal(err)
	}
	row := lastValuesRow(entry, []model.QueriesRequestElementColumn{{Name: "root.level"}, {Name: "root.missing"}})
	ts, ok := row[0].(time.Time)
	if !ok {
		t.Fatalf("expected a time, got %#v", row[0])
	}
	expected := time.Date(2022, 12, 6, 6, 0, 0, 0, time.UTC)
	if !ts.Equal(expected) || ts.Location() != time.Local {
		t.Errorf("expected %v in the local time zone, got %v", expected, ts)
	}
	if !reflect.DeepEqual(row[1:], []interface{}{42.0, nil}) {
		t.Errorf("unexpected values %#v", row[1:])
	}
}
