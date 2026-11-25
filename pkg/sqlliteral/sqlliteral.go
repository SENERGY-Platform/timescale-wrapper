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

// Package sqlliteral renders values as SQL literals and identifiers. It is the single place that
// decides how request-controlled text is embedded into query text.
//
// This is a leaf package on purpose: it imports no other package of this module, so pkg/timescale,
// pkg/locate and pkg/tablenames can all use it without import cycles.
package sqlliteral

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// QuoteIdentifier renders name as a quoted SQL identifier, escaping any embedded quotes.
func QuoteIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// EscapeString escapes s for use inside a single quoted SQL string literal, without adding the
// surrounding quotes.
func EscapeString(s string) string {
	s = strings.ReplaceAll(s, string([]byte{0}), "")
	return strings.ReplaceAll(s, "'", "''")
}

// QuoteString renders s as a single quoted SQL string literal.
func QuoteString(s string) string {
	return "'" + EscapeString(s) + "'"
}

// ValueLiteral renders value as a SQL literal. Values may originate from a request body or from a
// previous query (see CreateFiltersForImport), so unknown types are rejected rather than printed
// with %v.
func ValueLiteral(value interface{}) (string, error) {
	switch val := value.(type) {
	case string:
		return QuoteString(val), nil
	case bool:
		return strconv.FormatBool(val), nil
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(val), nil
	case int16:
		return strconv.FormatInt(int64(val), 10), nil
	case int32:
		return strconv.FormatInt(int64(val), 10), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case json.Number:
		if _, err := val.Float64(); err != nil {
			return "", fmt.Errorf("invalid numeric filter value %v", val)
		}
		return val.String(), nil
	default:
		return "", fmt.Errorf("unsupported filter value type %T", value)
	}
}

// FunctionName maps a GroupType aggregation name to the start of the SQL function call
// that implements it, e.g. "mean" to "avg(" and "difference-last" to "last(".
func FunctionName(name string) string {
	switch name {
	case "mean":
		return "avg("
	case "median":
		return "percentile_disc(0.5) WITHIN GROUP (ORDER BY "
	case "time-weighted-mean-linear":
		return "average(time_weight('Linear', \"time\", "
	case "time-weighted-mean-locf":
		return "average(time_weight('LOCF', \"time\", "
	default:
		if _, function, found := strings.Cut(name, "-"); found && strings.HasPrefix(name, "difference") {
			return function + "("
		}
		return name + "("
	}
}

// maxJSONIndexLength bounds list indexes taken from a request.
const maxJSONIndexLength = 9

// JSONPath renders the segments of a model.JSONPath as a jsonpath string literal. Indexes
// consist of numerals only, fields are quoted keys, anything else is rejected, so no segment can
// leave the literal or add path syntax.
func JSONPath(segments []string) (string, error) {
	path := strings.Builder{}
	path.WriteString("$")
	for _, segment := range segments {
		switch {
		case segment == "*":
			path.WriteString("[*]")
		case isJSONIndex(segment):
			index, err := strconv.Atoi(segment) // drops leading zeros
			if err != nil {
				return "", err
			}
			path.WriteString("[" + strconv.Itoa(index) + "]")
		case len(segment) == 0:
			return "", errors.New("empty json path segment")
		default:
			key := strings.Builder{}
			for _, r := range segment {
				switch {
				case r < 0x20 || r == 0x7f:
					return "", errors.New("invalid character in json path segment")
				case r == '\\' || r == '"':
					key.WriteRune('\\')
					key.WriteRune(r)
				default:
					key.WriteRune(r)
				}
			}
			path.WriteString(".\"" + key.String() + "\"")
		}
	}
	return QuoteString(path.String()) + "::jsonpath", nil
}

func isJSONIndex(segment string) bool {
	if len(segment) == 0 || len(segment) > maxJSONIndexLength {
		return false
	}
	for _, r := range segment {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
