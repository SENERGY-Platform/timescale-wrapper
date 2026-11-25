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

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestNewElementQueryDoesNotChangeTheElement(t *testing.T) {
	s := func(v string) *string { return &v }
	element := model.QueriesRequestElement{
		Columns:   []model.QueriesRequestElementColumn{{Name: "a", GroupType: s("difference-last")}},
		Time:      &model.QueriesRequestElementTime{Last: s("5h")},
		GroupTime: s("1h"),
	}
	query, err := newElementQuery(element, "t", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if *element.Time.Last != "5h" {
		t.Errorf("the passed element was widened to %s", *element.Time.Last)
	}
	if *query.element.Time.Last != "6h" || query.differenceBuckets == nil || *query.differenceBuckets != 5 {
		t.Errorf("unexpected widening: %s, %v", *query.element.Time.Last, query.differenceBuckets)
	}
}

func TestNewElementQueryRejectsMixedAndUnknownGroupTypes(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := map[string]model.QueriesRequestElement{
		"aggregate column in raw query": {Columns: []model.QueriesRequestElementColumn{{Name: "a", GroupType: s("mean")}}},
		"raw column in grouped query":   {Columns: []model.QueriesRequestElementColumn{{Name: "a"}}, GroupTime: s("1h")},
		"unknown group type":            {Columns: []model.QueriesRequestElementColumn{{Name: "a", GroupType: s("avg")}}, GroupTime: s("1h")},
	}
	for name, element := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := newElementQuery(element, "t", "UTC"); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
