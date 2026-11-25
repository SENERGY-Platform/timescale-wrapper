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

package cache

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/log"
)

// childStatus tells how one path segment resolved against a decoded JSON value.
type childStatus int

const (
	childFound    childStatus = iota // the key or index exists, its value may be a JSON null
	childMissing                     // the key is absent, the index is outside of the list, or the value has no children
	childBadIndex                    // the value is a list and the segment is not an integer
)

// jsonChild resolves one segment below a decoded JSON value. A segment addresses a key of an
// object and an index of a list; "*" is not special here and is a plain key of an object.
func jsonChild(sub interface{}, segment string) (interface{}, childStatus) {
	switch parent := sub.(type) {
	case map[string]interface{}:
		if child, ok := parent[segment]; ok {
			return child, childFound
		}
	case []interface{}:
		n, err := strconv.Atoi(segment)
		if err != nil {
			return nil, childBadIndex
		}
		if n >= 0 && n < len(parent) {
			return parent[n], childFound
		}
	}
	return nil, childMissing
}

// lookupJSON returns the value at path below sub and whether it is present. A JSON null is
// present and yields (nil, true), a missing value (nil, false). A path segment that is no list
// index is logged and counts as missing.
func lookupJSON(sub interface{}, path []string) (interface{}, bool) {
	for _, segment := range path {
		child, status := jsonChild(sub, segment)
		switch status {
		case childFound:
			sub = child
		case childBadIndex:
			log.Logger.Warn(fmt.Sprintf("Could not extract index of list with index %#v", segment))
			return nil, false
		default:
			return nil, false
		}
	}
	return sub, true
}

// matchJSON collects the values at path below sub. Matches of a "*" segment are flattened into
// one list, but the values themselves are not, even if they are lists. Missing keys and indexes
// yield no match, a JSON null yields a nil match.
func matchJSON(sub interface{}, path []string) []interface{} {
	matches := []interface{}{}
	if len(path) == 0 {
		// reaching the end of the path means the value is present, even if it is a JSON null
		return append(matches, sub)
	}
	if list, ok := sub.([]interface{}); ok && path[0] == "*" {
		for _, element := range list {
			matches = append(matches, matchJSON(element, path[1:])...)
		}
		return matches
	}
	if child, status := jsonChild(sub, path[0]); status == childFound {
		matches = append(matches, matchJSON(child, path[1:])...)
	}
	return matches
}

// getDeepEntry returns the value at the dot separated path, or nil if there is none. A "*" segment
// addresses all elements of a list, and the result is then the list of all matches, flat and
// without missing values, like jsonb_path_query_array in lax mode returns for the database. Lax
// mode drops only what is missing, a key that is absent or an index outside of the list. A JSON
// null is present and stays in the result.
//
// Without a "*" segment the result does not tell a missing value from a JSON null, both are nil.
// lookupJSON does.
func getDeepEntry(m map[string]interface{}, path string) interface{} {
	segments := strings.Split(path, ".")
	if slices.Contains(segments, "*") {
		return matchJSON(m, segments)
	}
	value, _ := lookupJSON(m, segments)
	return value
}
