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
	"testing"

	deviceSelection "github.com/SENERGY-Platform/device-selection/pkg/client"
	"github.com/SENERGY-Platform/models/go/models"
)

func TestSelectablesCacheKey(t *testing.T) {
	criteria := []models.DeviceGroupFilterCriteria{{FunctionId: "f1", AspectId: "a1"}}
	options := &deviceSelection.GetSelectablesOptions{IncludeDevices: true, WithDeviceIds: []string{"d1"}}

	a1, err := selectablesCacheKey("userA", criteria, options)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := selectablesCacheKey("userA", criteria, options)
	if err != nil {
		t.Fatal(err)
	}
	b, err := selectablesCacheKey("userB", criteria, options)
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Errorf("same user must get the same key: %v != %v", a1, a2)
	}
	if a1 == b {
		t.Errorf("different users must get different keys: %v", a1)
	}
}
