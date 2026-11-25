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

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

func TestDecodeElements(t *testing.T) {
	t.Parallel()
	valid := `{"deviceId":"d","serviceId":"` + badRequestServiceId + `","columns":[{"name":"root.a"}]}`
	cases := []struct {
		name      string
		body      string
		wantErr   bool
		wantCount int
		groups    bool // rejectDeviceGroups refuses the elements
	}{
		{name: "valid", body: "[" + valid + "," + valid + "]", wantCount: 2},
		{name: "empty list", body: "[]"},
		{name: "invalid JSON", body: "{", wantErr: true},
		{name: "not a list", body: `{"deviceId":"d"}`, wantErr: true},
		{name: "element without columns", body: `[` + valid + `,{"deviceId":"d","serviceId":"` + badRequestServiceId + `"}]`, wantErr: true},
		{name: "device group", body: `[{"deviceGroupId":"g","columns":[{"name":"root.a"}]}]`, wantCount: 1, groups: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			elements, err := decodeElements(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(c.body)))
			if c.wantErr {
				if err == nil || model.GetStatusCode(err) != http.StatusBadRequest || countClasses(err) != 1 {
					t.Fatalf("expected one bad request, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(elements) != c.wantCount {
				t.Errorf("got %d elements, want %d", len(elements), c.wantCount)
			}
			err = rejectDeviceGroups(elements)
			if c.groups != (err != nil) {
				t.Errorf("rejectDeviceGroups = %v, want rejected %v", err, c.groups)
			}
			if err != nil && model.GetStatusCode(err) != http.StatusBadRequest {
				t.Errorf("status %d", model.GetStatusCode(err))
			}
		})
	}
}

type fakeLookup struct {
	rows map[string][][]interface{} // by device id
}

func (f fakeLookup) GetLastValuesFromCache(_ context.Context, request model.QueriesRequestElement, _ *string) ([][]interface{}, error) {
	if request.DeviceId != nil {
		if rows, ok := f.rows[*request.DeviceId]; ok {
			return rows, nil
		}
	}
	return nil, cache.NotCachableError
}

type fakeWrapper struct {
	generateErr error
	executeErr  error
	gotElements []model.QueriesRequestElement
	gotOwners   []string
	gotUser     string
	gotTz       string
	gotQueries  []string
}

func (f *fakeWrapper) GenerateQueries(_ context.Context, elements []model.QueriesRequestElement, userId string, owners []string, tz string, _ []models.Device) ([]string, error) {
	f.gotElements, f.gotOwners, f.gotUser, f.gotTz = elements, owners, userId, tz
	queries := make([]string, len(elements))
	for i := range elements {
		queries[i] = *elements[i].DeviceId
	}
	return queries, f.generateErr
}

func (f *fakeWrapper) ExecuteQueries(_ context.Context, queries []string) ([][][]interface{}, error) {
	f.gotQueries = queries
	if f.executeErr != nil {
		return nil, f.executeErr
	}
	data := make([][][]interface{}, len(queries))
	for i, q := range queries {
		data[i] = [][]interface{}{{"db", q}}
	}
	return data, nil
}

func TestFetchMergesCacheAndDatabase(t *testing.T) {
	t.Parallel()
	element := func(device string) model.QueriesRequestElement {
		return model.QueriesRequestElement{DeviceId: &device}
	}
	elements := []model.QueriesRequestElement{element("db0"), element("cached1"), element("db2"), element("cached3"), element("db4")}
	owners := []string{"o0", "o1", "o2", "o3", "o4"}
	lookup := fakeLookup{rows: map[string][][]interface{}{
		"cached1": {{"cache", "c1"}},
		"cached3": {{"cache", "c3"}},
	}}
	build := func(w *fakeWrapper) *pipeline {
		return &pipeline{config: &configuration.ConfigStruct{}, wrapper: w, lookup: lookup}
	}

	t.Run("db results go back to their slots", func(t *testing.T) {
		w := &fakeWrapper{}
		raw, err := build(w).fetch(context.Background(), elements, "user", owners, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := [][][]interface{}{
			{{"db", "db0"}}, {{"cache", "c1"}}, {{"db", "db2"}}, {{"cache", "c3"}}, {{"db", "db4"}},
		}
		if !reflect.DeepEqual(raw, want) {
			t.Errorf("got %v, want %v", raw, want)
		}
		if !reflect.DeepEqual(w.gotOwners, []string{"o0", "o2", "o4"}) {
			t.Errorf("owners of the db elements: %v", w.gotOwners)
		}
		if !reflect.DeepEqual(w.gotQueries, []string{"db0", "db2", "db4"}) || w.gotUser != "user" || w.gotTz != "" {
			t.Errorf("queries %v user %q tz %q", w.gotQueries, w.gotUser, w.gotTz)
		}
	})

	t.Run("force_tz is passed to the generation", func(t *testing.T) {
		w := &fakeWrapper{}
		tz := "Europe/Berlin"
		if _, err := build(w).fetch(context.Background(), elements, "user", owners, &tz, nil); err != nil {
			t.Fatal(err)
		}
		if w.gotTz != tz {
			t.Errorf("tz %q", w.gotTz)
		}
	})

	t.Run("generation error gets a class and keeps a given one", func(t *testing.T) {
		_, err := build(&fakeWrapper{generateErr: errors.New("boom")}).fetch(context.Background(), elements, "user", owners, nil, nil)
		if model.GetStatusCode(err) != http.StatusInternalServerError || countClasses(err) != 1 {
			t.Errorf("got %v", err)
		}
		_, err = build(&fakeWrapper{generateErr: errors.Join(errors.New("x"), model.ErrBadRequest)}).fetch(context.Background(), elements, "user", owners, nil, nil)
		if model.GetStatusCode(err) != http.StatusBadRequest || countClasses(err) != 1 {
			t.Errorf("got %v", err)
		}
	})

	t.Run("execution error is classified", func(t *testing.T) {
		_, err := build(&fakeWrapper{executeErr: errors.New("boom")}).fetch(context.Background(), elements, "user", owners, nil, nil)
		if model.GetStatusCode(err) != http.StatusInternalServerError || countClasses(err) != 1 {
			t.Errorf("got %v", err)
		}
	})

	t.Run("everything cached", func(t *testing.T) {
		w := &fakeWrapper{}
		cached := []model.QueriesRequestElement{element("cached1")}
		raw, err := build(w).fetch(context.Background(), cached, "user", owners[:1], nil, nil)
		if err != nil || !reflect.DeepEqual(raw, [][][]interface{}{{{"cache", "c1"}}}) {
			t.Errorf("got %v %v", raw, err)
		}
		if len(w.gotQueries) != 0 {
			t.Errorf("queries: %v", w.gotQueries)
		}
	})
}
