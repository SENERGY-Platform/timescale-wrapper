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
	"fmt"
	"slices"
	"strings"

	"github.com/SENERGY-Platform/go-service-base/struct-logger/attributes"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/cache"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/meta"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// resolveJSONPaths fills JSONPaths of the elements: column and filter names that address a value
// inside the JSONB column of a variable list are checked against the service description and
// replaced by the column and the path inside it.
//
// Only names with a list index or "*" can do that, so only elements with such a name need the
// service. It comes from the device repository through memcached. Errors wrap model.ErrBadRequest
// for names that do not fit the service, and model.ErrInternalServerError if the service cannot
// be read for a name with "*". Names with an index only are then used as plain columns. It
// returns errors and does not panic: it runs on goroutines without recover.
func resolveJSONPaths(ctx context.Context, remoteCache *cache.RemoteCache, elements []model.QueriesRequestElement) error {
	for i := range elements {
		names := listPathNames(elements[i])
		if len(names) == 0 {
			continue
		}
		if elements[i].DeviceId == nil || elements[i].ServiceId == nil {
			// without a service there is nothing to check against, and only services have variable lists
			for _, name := range names {
				if model.HasWildcardSegment(name) {
					return errors.Join(fmt.Errorf("column %q: \"*\" is only supported for devices", name), model.ErrBadRequest)
				}
			}
			continue
		}
		service, err := remoteCache.GetService(ctx, *elements[i].ServiceId)
		if err != nil {
			if slices.ContainsFunc(names, model.HasWildcardSegment) {
				return errors.Join(err, model.ErrInternalServerError)
			}
			// an index may as well address a list of fixed length, whose columns are plain. Use
			// the names as columns, as before list paths existed, so that an outage of the device
			// repository does not break those requests.
			log.Logger.WarnContext(ctx, "could not read service, using list paths as columns", attributes.ErrorKey, err)
			continue
		}
		// columns of a device group or location are built from criteria after the request was
		// validated, so the rules for "*" apply here as well
		for _, column := range elements[i].Columns {
			if !column.WildcardValid() {
				return errors.Join(fmt.Errorf("column %q: \"*\" needs an aggregation and does not work with math, a target characteristic or time weighted means", column.Name), model.ErrBadRequest)
			}
		}
		if elements[i].Filters != nil {
			for _, filter := range *elements[i].Filters {
				if model.HasWildcardSegment(filter.Column) {
					return errors.Join(fmt.Errorf("filter %q: \"*\" is not supported in filters", filter.Column), model.ErrBadRequest)
				}
			}
		}
		paths := map[string]model.JSONPath{}
		for _, name := range names {
			path, err := meta.ResolveColumn(service.Outputs, name)
			if err != nil {
				return err
			}
			if path != nil {
				paths[name] = *path
			}
		}
		if len(paths) > 0 {
			elements[i].JSONPaths = paths
		}
	}
	return nil
}

// listPathNames returns the distinct column and filter names of the element that have a segment
// that is a list index or "*".
func listPathNames(element model.QueriesRequestElement) []string {
	names := []string{}
	add := func(name string) {
		if !hasListSegment(name) {
			return
		}
		for _, existing := range names {
			if existing == name {
				return
			}
		}
		names = append(names, name)
	}
	for _, column := range element.Columns {
		add(column.Name)
	}
	if element.Filters != nil {
		for _, filter := range *element.Filters {
			add(filter.Column)
		}
	}
	return names
}

// hasListSegment reports whether a segment of the name is "*" or an integer. Negative integers
// count, so that ResolveColumn can reject them instead of leaving them to the database.
func hasListSegment(name string) bool {
	for _, segment := range strings.Split(name, ".") {
		if segment == "*" {
			return true
		}
		digits := strings.TrimPrefix(segment, "-")
		if len(digits) == 0 {
			continue
		}
		numeric := true
		for _, r := range digits {
			if r < '0' || r > '9' {
				numeric = false
				break
			}
		}
		if numeric {
			return true
		}
	}
	return false
}
