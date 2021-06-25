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
	"regexp"
	"time"
)

type QueriesRequestElement struct {
	ExportId         *string // TODO check ID in valid format
	DeviceId         *string // TODO check ID in valid format
	ServiceId        *string // TODO check ID in valid format
	Time             *QueriesRequestElementTime
	Limit            *int
	Columns          []QueriesRequestElementColumn
	Filters          *[]QueriesRequestElementFilter
	GroupTime        *string
	OrderColumnIndex *int
	OrderDirection   *Direction
}

func (element *QueriesRequestElement) Valid() bool {
	if element.ExportId == nil && (element.DeviceId == nil || element.ServiceId == nil) {
		return false
	}
	if element.ExportId != nil && (element.DeviceId != nil || element.ServiceId != nil) {
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
	if element.OrderColumnIndex != nil && (*element.OrderColumnIndex < 1 || *element.OrderColumnIndex > len(element.Columns)) {
		return false
	}
	if element.OrderColumnIndex == nil {
		zero := 0
		element.OrderColumnIndex = &zero
	}
	if element.OrderDirection == nil {
		desc := Desc
		element.OrderDirection = &desc
	}
	return true
}

type QueriesRequestElementTime struct {
	Last  *string
	Start *string
	End   *string
}

func (elementTime *QueriesRequestElementTime) Valid() bool {
	if elementTime.Last != nil {
		if elementTime.Start != nil || elementTime.End != nil {
			return false
		}
		if !timeIntervalValid(*elementTime.Last) {
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

type QueriesRequestElementColumn struct {
	Name      string // TODO valid format
	GroupType *string
	Math      *string
}

func (elementColumn *QueriesRequestElementColumn) Valid(hasTime bool) bool {
	if len(elementColumn.Name) == 0 {
		return false
	}
	if elementColumn.GroupType != nil && !hasTime {
		return false
	}
	if elementColumn.GroupType != nil {
		allowedTypes := []interface{}{}
		allowedTypes = append(allowedTypes, "mean", "sum", "count", "median", "min", "max", "first", "last",
			"difference-first", "difference-last", "difference-min", "difference-max", "difference-count", "difference-mean",
			"difference-sum", "difference-median")
		if !ElementInArray(*elementColumn.GroupType, allowedTypes) {
			return false
		}
	}
	if elementColumn.Math != nil && !mathValid(*elementColumn.Math) {
		return false
	}
	return true
}

type QueriesRequestElementFilter struct {
	Column string // TODO valid format
	Math   *string
	Type   string
	Value  interface{} // TODO valid format
}

func (filter *QueriesRequestElementFilter) Valid() bool {
	if filter.Math != nil && !mathValid(*filter.Math) {
		return false
	}
	allowedTypes := []interface{}{}
	allowedTypes = append(allowedTypes, "=", "<>", "!=", ">", ">=", "<", "<=")
	return ElementInArray(filter.Type, allowedTypes) && len(filter.Column) > 0 && filter.Value != nil
}

func mathValid(math string) bool {
	mathMatcher := regexp.MustCompile("([+\\-*/])\\d+(([.,])\\d+)?")
	return len(mathMatcher.FindString(math)) == len(math)
}

func timeIntervalValid(timeInterval string) bool {
	timeMatcher := regexp.MustCompile("\\d+(ns|u|µ|ms|s|m|h|d|w)")
	return len(timeMatcher.FindString(timeInterval)) == len(timeInterval)
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
