/*
 *    Copyright 2021 InfAI (CC SES)
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

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/SENERGY-Platform/models/go/models"
)

type QueriesRequestElement struct {
	ExportId         *string                        `json:"exportId,omitempty"`
	DeviceId         *string                        `json:"deviceId,omitempty"`
	ServiceId        *string                        `json:"serviceId,omitempty"`
	Time             *QueriesRequestElementTime     `json:"time,omitempty"`
	Limit            *int                           `json:"limit,omitempty"`
	Columns          []QueriesRequestElementColumn  `json:"columns,omitempty"`
	Filters          *[]QueriesRequestElementFilter `json:"filters,omitempty"`
	GroupTime        *string                        `json:"groupTime,omitempty"`
	OrderColumnIndex *int                           `json:"orderColumnIndex,omitempty"`
	OrderDirection   *Direction                     `json:"orderDirection,omitempty"`
	DeviceGroupId    *string                        `json:"deviceGroupId,omitempty"`
	LocationId       *string                        `json:"locationId,omitempty"`

	// JSONPaths holds, per requested column or filter name, the address of a value inside a JSONB
	// column. It is filled server side from the service description (see api.resolveJSONPaths)
	// and never read from or written to a request.
	JSONPaths map[string]JSONPath `json:"-"`
}

// JSONPath addresses a value inside the JSONB column of a variable length list. Column names are
// dot separated paths. If a variable list lies on the path, everything up to and including the
// list is the column and the remainder is a path into its JSON value.
type JSONPath struct {
	// Column is the name of the JSONB column, as the table worker names it before hashing.
	Column string
	// Segments is the path inside the JSON value. Each segment is a non-negative list index, "*"
	// for all elements of a list, or the name of a structure field.
	Segments []string
	// SQLType is the SQL type of the addressed value. Empty means jsonb, without a cast.
	SQLType string
}

// Wildcard reports whether the path addresses all elements of a list.
func (path JSONPath) Wildcard() bool {
	return slices.Contains(path.Segments, "*")
}

func (element *QueriesRequestElement) Valid() bool {
	if element.ExportId == nil && (element.DeviceId == nil || element.ServiceId == nil || !serviceIdValid(*element.ServiceId)) && element.DeviceGroupId == nil && element.LocationId == nil {
		return false
	}
	if element.ExportId != nil && (element.DeviceId != nil || element.ServiceId != nil || element.DeviceGroupId != nil || element.LocationId != nil) {
		return false
	}
	if element.DeviceGroupId != nil && (element.DeviceId != nil || element.ServiceId != nil || element.ExportId != nil || element.LocationId != nil) {
		for _, col := range element.Columns {
			if !DeviceGroupFilterCriteriaValid(col.Criteria) {
				return false
			}
		}
		return false
	}
	if element.LocationId != nil && (element.DeviceId != nil || element.ServiceId != nil || element.ExportId != nil || element.DeviceGroupId != nil) {
		for _, col := range element.Columns {
			if !DeviceGroupFilterCriteriaValid(col.Criteria) {
				return false
			}
		}
		return false
	}
	if element.Time != nil && !element.Time.Valid() {
		return false
	}
	if len(element.Columns) == 0 {
		return false
	}
	for _, column := range element.Columns {
		if !column.Valid(element.GroupTime != nil) {
			return false
		}
		if column.TargetCharacteristicId != nil && column.SourceCharacteristicId == nil && element.ExportId != nil {
			return false
		}
	}
	if element.Filters != nil {
		for _, filter := range *element.Filters {
			if !filter.Valid() {
				return false
			}
		}
	}
	if element.GroupTime != nil && !timeIntervalValid(*element.GroupTime) {
		return false
	}
	if element.OrderDirection != nil && *element.OrderDirection != Asc && *element.OrderDirection != Desc {
		return false
	}
	if element.OrderColumnIndex != nil && (*element.OrderColumnIndex < 0 || *element.OrderColumnIndex > len(element.Columns)) {
		return false
	}
	if element.Limit != nil && *element.Limit < 0 {
		return false
	}
	/*
		if element.OrderColumnIndex == nil {
			zero := 0
			element.OrderColumnIndex = &zero
		}
		if element.OrderDirection == nil {
			desc := Desc
			element.OrderDirection = &desc
		}
	*/
	return true
}

type QueriesRequestElementTime struct {
	Last        *string `json:"last,omitempty"`
	Ahead       *string `json:"ahead,omitempty"`
	Start       *string `json:"start,omitempty"`
	End         *string `json:"end,omitempty"`
	EndOriginal *string `json:"-"`
}

func (elementTime *QueriesRequestElementTime) Valid() bool {
	if elementTime.Last != nil {
		if elementTime.Start != nil || elementTime.End != nil || elementTime.Ahead != nil {
			return false
		}
		if !timeIntervalValid(*elementTime.Last) {
			return false
		}
	} else if elementTime.Ahead != nil {
		if elementTime.Start != nil || elementTime.End != nil || elementTime.Last != nil {
			return false
		}
		if !timeIntervalValid(*elementTime.Ahead) {
			return false
		}
	} else {
		if elementTime.Start == nil || elementTime.End == nil {
			return false
		}
		_, err := time.Parse(time.RFC3339, *elementTime.Start)
		if err != nil {
			return false
		}
		_, err = time.Parse(time.RFC3339, *elementTime.End)
		if err != nil {
			return false
		}
	}
	return true
}

func (elementTime *QueriesRequestElementTime) Copy() QueriesRequestElementTime {
	cpy := QueriesRequestElementTime{}
	if elementTime.Last != nil {
		s := *elementTime.Last
		cpy.Last = &s
	}
	if elementTime.Ahead != nil {
		s := *elementTime.Ahead
		cpy.Ahead = &s
	}

	if elementTime.Start != nil {
		s := *elementTime.Start
		cpy.Start = &s
	}
	if elementTime.End != nil {
		s := *elementTime.End
		cpy.End = &s
	}
	return cpy
}

type QueriesRequestElementColumn struct {
	// Dot separated path of the value, starting with the name of the output variable. Below a variable length list the path continues with a list index (0, 1, ...), with * for all elements of the list, and with field names of structures, for example root.list.0.field or root.list.*.field. The value has the type of the variable, a missing element or field is null. Without groupType, * returns the matching values of each message as a flat array. With groupType, the aggregation runs over all matching elements of all messages in a time bucket; first and last order by time, then by position in the list. Without a groupType, * can not be combined with math or targetCharacteristicId. * can not be combined with time-weighted-* group types or used in filters.
	Name                   string                           `json:"name,omitempty"`
	GroupType              *string                          `json:"groupType,omitempty"`
	Math                   *string                          `json:"math,omitempty"`
	SourceCharacteristicId *string                          `json:"sourceCharacteristicId,omitempty"`
	TargetCharacteristicId *string                          `json:"targetCharacteristicId,omitempty"`
	ConceptId              *string                          `json:"conceptId,omitempty"`
	Criteria               models.DeviceGroupFilterCriteria `json:"criteria,omitempty"`
}

func (elementColumn *QueriesRequestElementColumn) Valid(hasTime bool) bool {
	// exactly one of name/criteria may be used. the name of a criteria based column is resolved
	// server side, so any client supplied name must be rejected instead of being left unvalidated.
	if DeviceGroupFilterCriteriaValid(elementColumn.Criteria) {
		if len(elementColumn.Name) > 0 {
			return false
		}
	} else if !columnNameValid(elementColumn.Name) {
		return false
	}
	if elementColumn.GroupType != nil && !hasTime {
		return false
	}
	if !elementColumn.WildcardValid() {
		return false
	}
	if elementColumn.GroupType != nil {
		allowedTypes := []interface{}{}
		allowedTypes = append(allowedTypes, "mean", "sum", "count", "median", "min", "max", "first", "last",
			"difference-first", "difference-last", "difference-min", "difference-max", "difference-count", "difference-mean",
			"difference-sum", "difference-median", "time-weighted-mean-linear", "time-weighted-mean-locf")
		if !ElementInArray(*elementColumn.GroupType, allowedTypes) {
			return false
		}
	}
	if elementColumn.Math != nil && !mathValid(*elementColumn.Math) {
		return false
	}
	if elementColumn.TargetCharacteristicId != nil && elementColumn.ConceptId == nil && elementColumn.Criteria.FunctionId == "" {
		return false
	}
	return true
}

// WildcardValid checks the rules for a column name with "*", which addresses all elements of a
// list. The elements can only be reduced to one value by an aggregation. Without one the column is
// a list, which math and conversions cannot work on. Time weighted means need one value per
// message. Columns without "*" are always valid here. Columns built after Valid ran, such as those
// of criteria, have to be checked with this as well.
func (elementColumn *QueriesRequestElementColumn) WildcardValid() bool {
	if !HasWildcardSegment(elementColumn.Name) {
		return true
	}
	if elementColumn.GroupType == nil && (elementColumn.Math != nil || elementColumn.TargetCharacteristicId != nil) {
		return false
	}
	if elementColumn.GroupType != nil && strings.HasPrefix(*elementColumn.GroupType, "time-weighted-") {
		return false
	}
	return true
}

type QueriesRequestElementFilter struct {
	Column string      `json:"column,omitempty"`
	Math   *string     `json:"math,omitempty"`
	Type   string      `json:"type,omitempty"`
	Value  interface{} `json:"value,omitempty"`
}

var valueMatcher = regexp.MustCompile("^[a-zA-Z0-9äöüß:{}\"\\.\\-_\\/ ]*$")

func (filter *QueriesRequestElementFilter) Valid() bool {
	if filter.Math != nil && !mathValid(*filter.Math) {
		return false
	}
	allowedTypes := []interface{}{}
	allowedTypes = append(allowedTypes, "=", "!=")
	if filter.Value != nil {
		allowedTypes = append(allowedTypes, "<>", ">", ">=", "<", "<=")
		switch v := filter.Value.(type) {
		case string:
			if !valueMatcher.MatchString(v) {
				return false
			}
		case bool, float64, float32, int, int32, int64, json.Number:
			// rendered as a numeric/boolean literal
		default:
			// anything else (json objects, arrays) would be rendered with %v and reach the
			// query unvalidated
			return false
		}
	}
	return ElementInArray(filter.Type, allowedTypes) && columnNameValid(filter.Column) && !HasWildcardSegment(filter.Column)
}

// the comma used to be allowed as a decimal separator, but postgres reads it as a list
// separator, which turns "+1,5" into an additional select list item
var mathMatcher = regexp.MustCompile("^([+\\-*/])\\d+(\\.\\d+)?$")

func mathValid(math string) bool {
	return mathMatcher.MatchString(math)
}

var timeMatcher = regexp.MustCompile("^(\\d)+\\s*(ms|s|months|mon|m|h|day|d|w|y|:\\d\\d:\\d\\d)$")

func timeIntervalValid(timeInterval string) bool {
	return timeMatcher.MatchString(timeInterval)
}

var columnMatcher = regexp.MustCompile("^([a-zA-Z0-9\\.\\-_\\*])+$")

// columnNameValid allows the same characters as before plus "*", which addresses all elements of a
// variable list. It has to be a segment of its own.
func columnNameValid(column string) bool {
	if !columnMatcher.MatchString(column) {
		return false
	}
	for _, segment := range strings.Split(column, ".") {
		if strings.Contains(segment, "*") && segment != "*" {
			return false
		}
	}
	return true
}

// HasWildcardSegment reports whether a segment of the dot separated column name is "*".
func HasWildcardSegment(column string) bool {
	return slices.Contains(strings.Split(column, "."), "*")
}

// TimezoneValid checks a timezone name before it is placed into a query. It intentionally does
// not check that the zone actually exists (postgres will report that) but rules out anything
// that could terminate the surrounding string literal.
var timezoneMatcher = regexp.MustCompile("^[a-zA-Z0-9_/+:.-]{1,64}$")

func TimezoneValid(timezone string) bool {
	return timezoneMatcher.MatchString(timezone)
}

var uuidMatcher = regexp.MustCompile("^([a-z0-9\\-_])+$")

func serviceIdValid(serviceId string) bool {
	splitted := strings.Split(serviceId, "urn:infai:ses:service:")
	return len(splitted) == 2 && len(splitted[1]) == 36 && uuidMatcher.MatchString(splitted[1])
}

type Format string

const (
	PerQuery Format = "per_query"
	Table    Format = "table"
)

type Direction string

const (
	Asc  Direction = "asc"
	Desc Direction = "desc"
)

type PreparedQueriesRequestElement struct {
	QueriesRequestElement
	Token      string `json:"token,omitempty"`
	TimeFormat string `json:"timeFormat,omitempty"`
	Precision  *int   `json:"precision,omitempty"`
}

func DeviceGroupFilterCriteriaValid(criteria models.DeviceGroupFilterCriteria) bool {
	if len(criteria.FunctionId) == 0 {
		return false
	}
	if len(criteria.AspectId) == 0 {
		return false
	}
	return true
}
