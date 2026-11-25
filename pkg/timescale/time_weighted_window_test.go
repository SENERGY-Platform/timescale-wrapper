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
	"errors"
	"testing"
	"time"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestTimeWeightedBucketsAllowedErrors(t *testing.T) {
	start := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		window    [2]time.Time
		groupTime string
		rejected  bool
	}{
		{name: "not an interval", window: [2]time.Time{start, start.Add(time.Hour)}, groupTime: "abc", rejected: true},
		{name: "milliseconds", window: [2]time.Time{start, start.Add(time.Hour)}, groupTime: "500ms", rejected: true},
		{name: "zero width", window: [2]time.Time{start, start.Add(time.Hour)}, groupTime: "0s", rejected: true},
		{name: "one hour over one day", window: [2]time.Time{start, start.Add(24 * time.Hour)}, groupTime: "1h"},
		{name: "buckets at the limit", window: [2]time.Time{start, start.Add(maxTimeWeightedBuckets * time.Second)}, groupTime: "1s"},
		{name: "one bucket over the limit", window: [2]time.Time{start, start.Add((maxTimeWeightedBuckets + 1) * time.Second)}, groupTime: "1s", rejected: true},
		{name: "days at the limit", window: [2]time.Time{start, start.Add(maxTimeWeightedDays * 24 * time.Hour)}, groupTime: "1y"},
		{name: "one day over the limit", window: [2]time.Time{start, start.Add((maxTimeWeightedDays + 1) * 24 * time.Hour)}, groupTime: "1y", rejected: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := timeWeightedBucketsAllowed(c.window, c.groupTime)
			if c.rejected && !errors.Is(err, model.ErrBadRequest) {
				t.Errorf("expected a bad request, got %v", err)
			}
			if !c.rejected && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}
