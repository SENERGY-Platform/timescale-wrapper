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

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
)

// lookupSource is the part of *cache.RemoteCache that answers service and concept lookups.
type lookupSource interface {
	GetService(ctx context.Context, serviceId string) (models.Service, error)
	GetConcept(ctx context.Context, conceptId string) (models.Concept, error)
}

var _ lookupSource = (*cache.RemoteCache)(nil)

// requestLookups answers the service and concept lookups of one HTTP request. Each id is fetched from the source
// once per request, however many elements and columns ask for it and from however many goroutines: concurrent
// askers of an id wait for the one fetch. A failed fetch is not kept, so a later asker of the same id fetches
// again. Create one per request, it does not expire.
type requestLookups struct {
	source   lookupSource
	services memo[models.Service]
	concepts memo[models.Concept]
}

// newRequestLookups returns the lookups of a request over the source. A nil source answers every lookup with an
// error.
func newRequestLookups(source lookupSource) *requestLookups {
	return &requestLookups{source: source}
}

var errNoLookupSource = errors.New("no source for service and concept lookups")

func (l *requestLookups) GetService(ctx context.Context, serviceId string) (models.Service, error) {
	return l.services.get(serviceId, func() (models.Service, error) {
		if l.source == nil {
			return models.Service{}, errNoLookupSource
		}
		return l.source.GetService(ctx, serviceId)
	})
}

func (l *requestLookups) GetConcept(ctx context.Context, conceptId string) (models.Concept, error) {
	return l.concepts.get(conceptId, func() (models.Concept, error) {
		if l.source == nil {
			return models.Concept{}, errNoLookupSource
		}
		return l.source.GetConcept(ctx, conceptId)
	})
}

// memo holds the results of fetches by id. The zero value is ready to use.
type memo[T any] struct {
	mux     sync.Mutex
	entries map[string]*memoEntry[T]
}

type memoEntry[T any] struct {
	done  chan struct{}
	value T
	err   error
}

// get returns the value of id, calling fetch if no other call has done so or is doing so. Askers that
// arrived while a fetch ran share its result, including its error; the error is then dropped from the memo.
func (m *memo[T]) get(id string, fetch func() (T, error)) (T, error) {
	m.mux.Lock()
	if m.entries == nil {
		m.entries = map[string]*memoEntry[T]{}
	}
	if entry, ok := m.entries[id]; ok {
		m.mux.Unlock()
		<-entry.done
		return entry.value, entry.err
	}
	entry := &memoEntry[T]{done: make(chan struct{})}
	m.entries[id] = entry
	m.mux.Unlock()

	// release the waiters even if fetch panics
	defer func() {
		m.mux.Lock()
		if entry.err != nil {
			delete(m.entries, id)
		}
		m.mux.Unlock()
		close(entry.done)
	}()
	entry.err = errors.New("lookup did not finish")
	entry.value, entry.err = fetch()
	return entry.value, entry.err
}
