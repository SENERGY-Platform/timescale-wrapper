/*
 * Copyright 2022 InfAI (CC SES)
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

package meta

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

// variableListElement is the name of the single sub content variable that describes the elements
// of a variable length list.
const variableListElement = "*"

// maxIndexLength bounds list indexes, so that they fit into an int and into a jsonpath.
const maxIndexLength = 9

// GetDeepContentVariable returns the content variable at path below root, or nil if there is none.
// The element of a variable list matches any list index and "*".
func GetDeepContentVariable(root models.ContentVariable, path []string) *models.ContentVariable {
	if len(path) == 0 {
		return &root
	}
	if root.SubContentVariables == nil {
		return nil
	}
	for _, sub := range root.SubContentVariables {
		if sub.Name == path[0] || (sub.Name == variableListElement && (path[0] == "*" || isIndex(path[0]))) {
			return GetDeepContentVariable(sub, path[1:])
		}
	}
	return nil
}

// isIndex reports whether s is a non-negative list index.
func isIndex(s string) bool {
	if len(s) == 0 || len(s) > maxIndexLength {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isVariableList(cv models.ContentVariable) bool {
	return cv.Type == models.List && len(cv.SubContentVariables) > 0 && cv.SubContentVariables[0].Name == variableListElement
}

func findSub(cv models.ContentVariable, name string) *models.ContentVariable {
	for i := range cv.SubContentVariables {
		if cv.SubContentVariables[i].Name == name {
			return &cv.SubContentVariables[i]
		}
	}
	return nil
}

func badColumn(name string, reason string) error {
	return errors.Join(fmt.Errorf("column %q: %s", name, reason), model.ErrBadRequest)
}

// ResolveColumn checks the dot separated column name against the outputs of a service. If a
// variable length list lies on the path, the list is stored as one JSONB column and the result
// addresses a value inside of it. Otherwise the name is the name of a regular column and the
// result is nil. A name that does not exist in the outputs is an error wrapping
// model.ErrBadRequest.
//
// A path into a variable list may continue with a list index or "*" for all elements, and then
// with field names of structures and indexes of lists. Field names that consist only of digits
// cannot be told from list indexes and are rejected.
func ResolveColumn(outputs []models.Content, name string) (*model.JSONPath, error) {
	segments := strings.Split(name, ".")
	var cur *models.ContentVariable
	for i := range outputs {
		if outputs[i].ContentVariable.Name == segments[0] {
			cur = &outputs[i].ContentVariable
			break
		}
	}
	if cur == nil {
		return nil, badColumn(name, "not found in service output")
	}
	for i := 1; ; i++ {
		if i == len(segments) {
			return nil, nil // a regular column, or the JSONB column of a list as a whole
		}
		if isVariableList(*cur) {
			return resolveInList(*cur, strings.Join(segments[:i], "."), segments[i:], name)
		}
		cur = findSub(*cur, segments[i])
		if cur == nil {
			return nil, badColumn(name, "not found in service output")
		}
	}
}

func resolveInList(node models.ContentVariable, column string, rest []string, name string) (*model.JSONPath, error) {
	for _, segment := range rest {
		switch {
		case isVariableList(node):
			if segment != "*" && !isIndex(segment) {
				return nil, badColumn(name, fmt.Sprintf("invalid list index %q", segment))
			}
			node = node.SubContentVariables[0]
		case node.Type == models.List: // fixed list inside an element
			if !isIndex(segment) {
				return nil, badColumn(name, fmt.Sprintf("invalid list index %q", segment))
			}
			sub := findSub(node, segment)
			if sub == nil {
				return nil, badColumn(name, "not found in service output")
			}
			node = *sub
		case node.Type == models.Structure:
			if segment == "*" || isIndex(segment) {
				return nil, badColumn(name, fmt.Sprintf("%q is not a field name", segment))
			}
			sub := findSub(node, segment)
			if sub == nil {
				return nil, badColumn(name, "not found in service output")
			}
			node = *sub
		default:
			return nil, badColumn(name, fmt.Sprintf("%q is below a value that has no elements", segment))
		}
	}
	// must match the column types of the table worker, which stores variables inside a list as JSON
	sqlType := ""
	switch node.Type {
	case models.String:
		sqlType = "text"
	case models.Boolean:
		sqlType = "boolean"
	case models.Float:
		sqlType = "double precision"
	case models.Integer:
		sqlType = "bigint"
	case models.Structure, models.List:
		sqlType = "" // jsonb
	default:
		return nil, badColumn(name, fmt.Sprintf("unsupported type %q", node.Type))
	}
	return &model.JSONPath{Column: column, Segments: append([]string{}, rest...), SQLType: sqlType}, nil
}
