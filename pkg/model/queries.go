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
}

func (element *QueriesRequestElement) Valid() bool {
	if element.ExportId == nil && (element.DeviceId == nil || element.ServiceId == nil || !serviceIdValid(*element.ServiceId)) && element.DeviceGroupId == nil {
		return false
	}
	if element.ExportId != nil && (element.DeviceId != nil || element.ServiceId != nil || element.DeviceGroupId != nil) {
		return false
	}
	if element.DeviceGroupId != nil && (element.DeviceId != nil || element.ServiceId != nil || element.ExportId != nil) {
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
	Last  *string `json:"last,omitempty"`
	Ahead *string `json:"ahead,omitempty"`
	Start *string `json:"start,omitempty"`
	End   *string `json:"end,omitempty"`
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

type QueriesRequestElementColumn struct {
	Name                   string                           `json:"name,omitempty"`
	GroupType              *string                          `json:"groupType,omitempty"`
	Math                   *string                          `json:"math,omitempty"`
	SourceCharacteristicId *string                          `json:"sourceCharacteristicId,omitempty"`
	TargetCharacteristicId *string                          `json:"targetCharacteristicId,omitempty"`
	ConceptId              *string                          `json:"conceptId,omitempty"`
	Criteria               models.DeviceGroupFilterCriteria `json:"criteria,omitempty"`
}

func (elementColumn *QueriesRequestElementColumn) Valid(hasTime bool) bool {
	nameValid := columnNameValid(elementColumn.Name)
	criteriaValid := DeviceGroupFilterCriteriaValid(elementColumn.Criteria)
	if !nameValid && !criteriaValid {
		return false
	}
	if nameValid && criteriaValid {
		return false
	}
	if elementColumn.GroupType != nil && !hasTime {
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

type QueriesRequestElementFilter struct {
	Column string      `json:"column,omitempty"`
	Math   *string     `json:"math,omitempty"`
	Type   string      `json:"type,omitempty"`
	Value  interface{} `json:"value,omitempty"`
}

var valueMatcher = regexp.MustCompile("[a-zA-Z0-9äöüß:{}\"\\.\\-_\\/ ]*")

func (filter *QueriesRequestElementFilter) Valid() bool {
	if filter.Math != nil && !mathValid(*filter.Math) {
		return false
	}
	allowedTypes := []interface{}{}
	allowedTypes = append(allowedTypes, "=", "<>", "!=", ">", ">=", "<", "<=")
	if filter.Value == nil {
		return false
	}
	s, ok := filter.Value.(string)
	if ok {
		if len(s) != len(valueMatcher.FindString(s)) {
			return false
		}
	}
	return ElementInArray(filter.Type, allowedTypes) && columnNameValid(filter.Column)
}

var mathMatcher = regexp.MustCompile("([+\\-*/])\\d+(([.,])\\d+)?")

func mathValid(math string) bool {
	return len(mathMatcher.FindString(math)) == len(math)
}

var timeMatcher = regexp.MustCompile("(\\d)+\\s*(ms|s|months|mon|m|h|day|d|w|y|:\\d\\d:\\d\\d)")

func timeIntervalValid(timeInterval string) bool {
	lengthOfFoundMatch := len(timeMatcher.FindString(timeInterval))
	return lengthOfFoundMatch == len(timeInterval) && lengthOfFoundMatch > 0
}

var columnMatcher = regexp.MustCompile("([a-zA-Z0-9\\.\\-_])+")

func columnNameValid(column string) bool {
	return len(column) != 0 && len(column) == len(columnMatcher.FindString(column))
}

var uuidMatcher = regexp.MustCompile("([a-z0-9\\-_])+")

func serviceIdValid(serviceId string) bool {
	splitted := strings.Split(serviceId, "urn:infai:ses:service:")
	return len(splitted) == 2 && len(splitted[1]) == 36 && len(splitted[1]) == len(uuidMatcher.FindString(splitted[1]))
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
