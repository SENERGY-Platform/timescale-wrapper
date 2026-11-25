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

package model

import "strings"

// AggregationKind tells how a group type reduces the values of a time bucket.
type AggregationKind int

const (
	// Plain aggregates the values of one bucket with a single function, e.g. "mean" or "last".
	Plain AggregationKind = iota
	// Difference aggregates like Plain and subtracts the value of the previous bucket,
	// e.g. "difference-last".
	Difference
	// TimeWeighted weights the values by the time they were valid, e.g. "time-weighted-mean-linear".
	TimeWeighted
)

const differencePrefix = "difference-"

// Aggregation is a parsed GroupType.
type Aggregation struct {
	Kind AggregationKind
	// Function is the aggregation function name: for Plain the group type itself ("mean"), for
	// Difference the part after "difference-" ("last" for "difference-last"), for TimeWeighted the
	// full group type ("time-weighted-mean-linear").
	Function string
	// groupType is the string this Aggregation was parsed from.
	groupType string
}

// String returns the group type the Aggregation was parsed from.
func (a Aggregation) String() string {
	return a.groupType
}

var plainFunctions = map[string]bool{
	"mean": true, "sum": true, "count": true, "median": true,
	"min": true, "max": true, "first": true, "last": true,
}

var timeWeightedFunctions = map[string]bool{
	"time-weighted-mean-linear": true,
	"time-weighted-mean-locf":   true,
}

// ParseAggregation parses a groupType of QueriesRequestElementColumn. It reports false for
// everything that is not an allowed group type; matching is exact and case sensitive.
func ParseAggregation(groupType string) (Aggregation, bool) {
	if plainFunctions[groupType] {
		return Aggregation{Kind: Plain, Function: groupType, groupType: groupType}, true
	}
	if timeWeightedFunctions[groupType] {
		return Aggregation{Kind: TimeWeighted, Function: groupType, groupType: groupType}, true
	}
	if function, ok := strings.CutPrefix(groupType, differencePrefix); ok && plainFunctions[function] {
		return Aggregation{Kind: Difference, Function: function, groupType: groupType}, true
	}
	return Aggregation{}, false
}

// Aggregation parses the column's GroupType. It reports false if there is none (nil) or it is
// not an allowed group type.
func (elementColumn QueriesRequestElementColumn) Aggregation() (Aggregation, bool) {
	if elementColumn.GroupType == nil {
		return Aggregation{}, false
	}
	return ParseAggregation(*elementColumn.GroupType)
}
