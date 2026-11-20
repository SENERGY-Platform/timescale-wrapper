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

package api

import (
	"testing"
	"time"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestResolveRelativeDownloadTime(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	last, ahead, start := "2d", "1:30:00", "2026-11-01T00:00:00Z"
	end := "2026-11-02T00:00:00Z"
	for _, tc := range []struct {
		time       model.QueriesRequestElementTime
		start, end string
	}{
		{model.QueriesRequestElementTime{Last: &last}, "2026-11-17T12:00:00Z", "2026-11-19T12:00:00Z"},
		{model.QueriesRequestElementTime{Ahead: &ahead}, "2026-11-19T12:00:00Z", "2026-11-19T13:30:00Z"},
		{model.QueriesRequestElementTime{Start: &start, End: &end}, start, end},
	} {
		if err := resolveRelativeDownloadTime(&tc.time, now); err != nil {
			t.Fatal(err)
		}
		if *tc.time.Start != tc.start || *tc.time.End != tc.end || tc.time.Last != nil || tc.time.Ahead != nil {
			t.Errorf("got %+v, want start %s and end %s without last and ahead", tc.time, tc.start, tc.end)
		}
		if !tc.time.Valid() {
			t.Errorf("%+v is not a valid time of /queries", tc.time)
		}
	}
}
