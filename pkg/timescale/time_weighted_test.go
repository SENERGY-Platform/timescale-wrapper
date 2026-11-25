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

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestTimeWeightedQueryRejectsUnboundedBuckets(t *testing.T) {
	start, end := "2021-01-01T00:00:00Z", "2022-01-01T00:00:00Z"
	farStart, farEnd := "1000-01-01T00:00:00Z", "1300-01-01T00:00:00Z"
	last, ahead := "1y", "1d"
	cases := []struct {
		name      string
		time      *model.QueriesRequestElementTime
		groupTime string
		rejected  bool
	}{
		{name: "no time window", groupTime: "1h", rejected: true},
		{name: "one second over a year", time: &model.QueriesRequestElementTime{Start: &start, End: &end}, groupTime: "1s", rejected: true},
		{name: "one minute over the last year", time: &model.QueriesRequestElementTime{Last: &last}, groupTime: "1m", rejected: true},
		{name: "one year over 300 years", time: &model.QueriesRequestElementTime{Start: &farStart, End: &farEnd}, groupTime: "1y", rejected: true},
		{name: "one hour over a year", time: &model.QueriesRequestElementTime{Start: &start, End: &end}, groupTime: "1h"},
		{name: "one second over the next day", time: &model.QueriesRequestElementTime{Ahead: &ahead}, groupTime: "1s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			groupTime := c.groupTime
			element := model.QueriesRequestElement{Time: c.time, GroupTime: &groupTime}
			_, err := timeWeightedQuery(element, "time-weighted-mean-locf", `"v"`, "t", "Europe/Berlin", "")
			if c.rejected && !errors.Is(err, model.ErrBadRequest) {
				t.Errorf("expected a bad request, got %v", err)
			}
			if !c.rejected && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}
