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
	"encoding/json"
	"reflect"
	"testing"
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
