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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SENERGY-Platform/converter/lib/converter"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// countingSource counts the lookups that reach the source. The first failures lookups of a service fail.
type countingSource struct {
	inner    lookupSource
	services atomic.Int32
	concepts atomic.Int32
	failures atomic.Int32
}

func (s *countingSource) GetService(ctx context.Context, id string) (models.Service, error) {
	s.services.Add(1)
	if s.failures.Add(-1) >= 0 {
		return models.Service{}, errors.New("device repository down")
	}
	return s.inner.GetService(ctx, id)
}

func (s *countingSource) GetConcept(ctx context.Context, id string) (models.Concept, error) {
	s.concepts.Add(1)
	return s.inner.GetConcept(ctx, id)
}

func fiveListElements() []model.QueriesRequestElement {
	device, service := listTestDeviceId, listTestServiceId
	elements := make([]model.QueriesRequestElement, 5)
	for i := range elements {
		elements[i] = model.QueriesRequestElement{
			DeviceId:  &device,
			ServiceId: &service,
			Columns:   []model.QueriesRequestElementColumn{{Name: "root.list.0.errorcode"}, {Name: "root.list.1.result"}},
		}
	}
	return elements
}

func TestResolveJSONPathsFetchesServiceOnce(t *testing.T) {
	source := &countingSource{inner: listTestCache(t)}
	elements := fiveListElements()
	if err := resolveJSONPaths(context.Background(), newRequestLookups(source), elements); err != nil {
		t.Fatal(err)
	}
	for i := range elements {
		if len(elements[i].JSONPaths) != 2 {
			t.Errorf("element %d: unexpected paths %#v", i, elements[i].JSONPaths)
		}
	}
	if n := source.services.Load(); n != 1 {
		t.Errorf("expected one service lookup for 5 elements, got %d", n)
	}
}

// The request elements of /queries/v2 resolve their paths on goroutines of their own.
func TestLookupsFetchOnceAcrossGoroutines(t *testing.T) {
	source := &countingSource{inner: listTestCache(t)}
	lookups := newRequestLookups(source)
	var wg sync.WaitGroup
	for _, element := range fiveListElements() {
		wg.Go(func() {
			if err := resolveJSONPaths(context.Background(), lookups, []model.QueriesRequestElement{element}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := source.services.Load(); n != 1 {
		t.Errorf("expected one service lookup, got %d", n)
	}
}

func TestFormatConvertsWithOneLookupPerId(t *testing.T) {
	source := &countingSource{inner: listTestCache(t)}
	conv, err := converter.New()
	if err != nil {
		t.Fatal(err)
	}
	service, target, concept := listTestServiceId, "2", "1"
	ts := time.Date(2022, 12, 6, 6, 0, 0, 0, time.UTC)
	request := make([]model.QueriesRequestElement, 5)
	data := make([][][]interface{}, 5)
	for i := range request {
		request[i] = model.QueriesRequestElement{
			ServiceId: &service,
			Columns:   []model.QueriesRequestElementColumn{{Name: "root.level", TargetCharacteristicId: &target, ConceptId: &concept}},
		}
		data[i] = [][]interface{}{{ts, 1.5}}
	}
	lookups := newRequestLookups(source)
	if _, err = formatPerQuery(context.Background(), lookups, request, data, 0, model.Asc, "", conv); err != nil {
		t.Fatal(err)
	}
	if _, err = formatTable(context.Background(), lookups, request, [][][]interface{}{{{ts, 1.5}}, {{ts, 1.5}}, {{ts, 1.5}}, {{ts, 1.5}}, {{ts, 1.5}}}, 0, model.Asc, "", conv); err != nil {
		t.Fatal(err)
	}
	if s, c := source.services.Load(), source.concepts.Load(); s != 1 || c != 1 {
		t.Errorf("expected one service and one concept lookup, got %d and %d", s, c)
	}
}

// A failed lookup is not kept beyond the call that saw it.
func TestLookupsDoNotKeepErrors(t *testing.T) {
	source := &countingSource{inner: listTestCache(t)}
	source.failures.Store(1)
	lookups := newRequestLookups(source)
	if _, err := lookups.GetService(context.Background(), listTestServiceId); err == nil {
		t.Fatal("expected the first lookup to fail")
	}
	if _, err := lookups.GetService(context.Background(), listTestServiceId); err != nil {
		t.Fatalf("expected the second lookup to succeed, got %v", err)
	}
	if _, err := lookups.GetService(context.Background(), listTestServiceId); err != nil {
		t.Fatal(err)
	}
	if n := source.services.Load(); n != 2 {
		t.Errorf("expected 2 lookups, got %d", n)
	}
}

func TestLookupsWithoutSource(t *testing.T) {
	if _, err := newRequestLookups(nil).GetConcept(context.Background(), "1"); err == nil {
		t.Error("expected an error")
	}
}
