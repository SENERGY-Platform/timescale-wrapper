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
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
)

func TestGetTZ(t *testing.T) {
	const defaultTZ = "Europe/Berlin"
	const deviceId = "urn:infai:ses:device:tz-device"
	const otherId = "urn:infai:ses:device:tz-other"
	withTZ := func(id, key, value string) models.Device {
		return models.Device{Id: id, Attributes: []models.Attribute{{Key: key, Value: value}}}
	}
	withoutTZ := models.Device{Id: deviceId, Attributes: []models.Attribute{{Key: "location", Value: "cellar"}}}

	tests := []struct {
		name     string
		deviceId string
		devices  []models.Device
		want     string
	}{
		{
			name:     "timezone attribute",
			deviceId: deviceId,
			devices:  []models.Device{withTZ(deviceId, "timezone", "Europe/Berlin")},
			want:     "Europe/Berlin",
		},
		{
			name:     "attribute key is matched case insensitively",
			deviceId: deviceId,
			devices:  []models.Device{withTZ(deviceId, "TimeZone", "America/New_York")},
			want:     "America/New_York",
		},
		{
			name:     "syntactically invalid value falls back to default",
			deviceId: deviceId,
			devices:  []models.Device{withTZ(deviceId, "timezone", "Not A Zone")},
			want:     defaultTZ,
		},
		{
			name:     "device without timezone attribute falls back to default",
			deviceId: deviceId,
			devices:  []models.Device{withoutTZ},
			want:     defaultTZ,
		},
		{
			name:     "device id not in list falls back to default",
			deviceId: deviceId,
			devices:  []models.Device{withTZ(otherId, "timezone", "Asia/Tokyo")},
			want:     defaultTZ,
		},
		{
			name:     "empty device list falls back to default",
			deviceId: deviceId,
			devices:  []models.Device{},
			want:     defaultTZ,
		},
		{
			name:     "second device matches",
			deviceId: deviceId,
			devices: []models.Device{
				withTZ(otherId, "timezone", "Asia/Tokyo"),
				withTZ(deviceId, "timezone", "Europe/Paris"),
			},
			want: "Europe/Paris",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getTZ(tt.deviceId, tt.devices, defaultTZ); got != tt.want {
				t.Errorf("getTZ() = %q, want %q", got, tt.want)
			}
		})
	}
}
