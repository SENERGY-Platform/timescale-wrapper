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
	"encoding/json"
	"errors"
	"net/http"

	"github.com/SENERGY-Platform/converter/lib/converter"
	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/timescale"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tracing"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/verification"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// queryWrapper is the part of *timescale.Wrapper the pipeline needs.
type queryWrapper interface {
	GenerateQueries(ctx context.Context, elements []model.QueriesRequestElement, userId string, ownerUserIds []string, forceTz string, devices []models.Device) ([]string, error)
	ExecuteQueries(ctx context.Context, queries []string) ([][][]interface{}, error)
}

// lastValuesCache is the part of *cache.RemoteCache that answers an element from the cache.
type lastValuesCache interface {
	GetLastValuesFromCache(ctx context.Context, request model.QueriesRequestElement, forceTZ *string) ([][]interface{}, error)
}

var (
	_ queryWrapper    = (*timescale.Wrapper)(nil)
	_ lastValuesCache = (*cache.RemoteCache)(nil)
)

// pipeline holds the dependencies of the steps every query endpoint is made of: decode, verify, resolve json paths,
// answer from the cache, query the database, format. The errors the steps return carry exactly one status class.
type pipeline struct {
	config      configuration.Config
	wrapper     queryWrapper
	verifier    accessVerifier
	remoteCache *cache.RemoteCache
	lookup      lastValuesCache
	converter   *converter.Converter
}

func newPipeline(config configuration.Config, wrapper *timescale.Wrapper, verifier *verification.Verifier, remoteCache *cache.RemoteCache, conv *converter.Converter) *pipeline {
	return &pipeline{config: config, wrapper: wrapper, verifier: verifier, remoteCache: remoteCache, lookup: remoteCache, converter: conv}
}

func (p *pipeline) tracer() trace.Tracer {
	return otel.Tracer("api/pipeline.go")
}

// phase runs f in a span of its own that ends on every path.
func phase[T any](ctx context.Context, tracer trace.Tracer, name string, f func(ctx context.Context) (T, error)) (T, error) {
	ctx, span := tracer.Start(ctx, name)
	defer span.End()
	return f(ctx)
}

// decodeElements reads the request elements from the body. An element that is not valid is a bad request.
func decodeElements(request *http.Request) ([]model.QueriesRequestElement, error) {
	var elements []model.QueriesRequestElement
	err := json.NewDecoder(request.Body).Decode(&elements)
	if err != nil {
		return nil, errors.Join(err, model.ErrBadRequest)
	}
	for i := range elements {
		if !elements[i].Valid() {
			return nil, errors.Join(errors.New("Invalid request body"), model.ErrBadRequest)
		}
	}
	return elements, nil
}

// rejectDeviceGroups refuses elements with a device group, which only /queries/v2 resolves.
func rejectDeviceGroups(elements []model.QueriesRequestElement) error {
	for i := range elements {
		if elements[i].DeviceGroupId != nil {
			return errors.Join(errors.New("Setting DeviceGroupId is invalid for /queries, use /queries/v2"), model.ErrBadRequest)
		}
	}
	return nil
}

// verify checks that the user of the request may access the elements, see verifyRequest.
func (p *pipeline) verify(ctx context.Context, request *http.Request, elements []model.QueriesRequestElement) (userId string, ownerUserIds []string, err error) {
	type result struct {
		userId string
		owners []string
	}
	r, err := phase(ctx, p.tracer(), "verify access", func(ctx context.Context) (result, error) {
		userId, owners, err := verifyRequest(ctx, request, p.verifier, elements)
		return result{userId, owners}, err
	})
	return r.userId, r.owners, err
}

// resolvePaths replaces the json paths of the elements, before the cache, so that cache and database reject the
// same requests.
func (p *pipeline) resolvePaths(ctx context.Context, lookups *requestLookups, elements []model.QueriesRequestElement) error {
	return classify(resolveJSONPaths(ctx, lookups, elements), model.ErrInternalServerError)
}

// lookupCache answers what it can from the cache. dbRequestIndices holds the positions of the elements that are
// left for the database, in the order of dbRequestElements.
func (p *pipeline) lookupCache(ctx context.Context, elements []model.QueriesRequestElement, forceTz *string) (raw [][][]interface{}, dbRequestElements []model.QueriesRequestElement, dbRequestIndices []int) {
	ctx, span := p.tracer().Start(ctx, "fetch from cache")
	defer span.End()
	return queriesGetFromCache(ctx, elements, p.lookup, p.config, forceTz)
}

// fetch returns the rows of every element, in the order of elements: from the cache where possible, otherwise from
// the database. ownerUserIds holds the owner of each of the elements.
func (p *pipeline) fetch(ctx context.Context, elements []model.QueriesRequestElement, userId string, ownerUserIds []string, forceTz *string, devices []models.Device) ([][][]interface{}, error) {
	raw, dbElements, dbIndices := p.lookupCache(ctx, elements, forceTz)

	tz := ""
	if forceTz != nil {
		tz = *forceTz
	}
	queries, err := phase(ctx, p.tracer(), "generate queries", func(ctx context.Context) ([]string, error) {
		return p.wrapper.GenerateQueries(ctx, dbElements, userId, ownersOfIndices(ownerUserIds, dbIndices), tz, devices)
	})
	if err != nil {
		return nil, classify(err, model.ErrInternalServerError)
	}
	data, err := phase(ctx, p.tracer(), "execute queries", func(ctx context.Context) ([][][]interface{}, error) {
		return p.wrapper.ExecuteQueries(ctx, queries)
	})
	if err != nil {
		return nil, classifyDB(err)
	}
	for i := range data {
		raw[dbIndices[i]] = data[i]
	}
	return raw, nil
}

// newLookups returns the lookups of one request. Every request creates its own, see requestLookups.
func (p *pipeline) newLookups() *requestLookups {
	if p.remoteCache == nil {
		return newRequestLookups(nil) // not a nil *cache.RemoteCache in a non-nil interface
	}
	return newRequestLookups(p.remoteCache)
}

// formatPerQuery brings the rows into one series per element. ctx should carry the span of the caller; the spans
// below it follow DetailedTracing.
func (p *pipeline) formatPerQuery(ctx context.Context, lookups *requestLookups, elements []model.QueriesRequestElement, raw [][][]interface{}, orderColumnIndex int, orderDirection model.Direction, timeFormat string) ([][][]interface{}, error) {
	response, err := formatPerQuery(tracing.SuppressChildren(ctx, p.config.DetailedTracing), lookups, elements, raw, orderColumnIndex, orderDirection, timeFormat, p.converter)
	return response, classify(err, model.ErrInternalServerError)
}

// formatTable brings the rows into one table with a row per timestamp, see formatPerQuery for ctx.
func (p *pipeline) formatTable(ctx context.Context, lookups *requestLookups, elements []model.QueriesRequestElement, raw [][][]interface{}, orderColumnIndex int, orderDirection model.Direction, timeFormat string) ([][]interface{}, error) {
	response, err := formatTable(tracing.SuppressChildren(ctx, p.config.DetailedTracing), lookups, elements, raw, orderColumnIndex, orderDirection, timeFormat, p.converter)
	return response, classify(err, model.ErrInternalServerError)
}

// ownersOfIndices selects the owners of the elements that are left after the cache, given their
// indices in the full list of elements that the owners are indexed by.
func ownersOfIndices(owners []string, indices []int) []string {
	selected := make([]string, len(indices))
	for i, index := range indices {
		selected[i] = owners[index]
	}
	return selected
}
