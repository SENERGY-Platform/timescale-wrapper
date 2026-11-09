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

package timescale

import (
	"strings"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	util "github.com/SENERGY-Platform/timescale-tableworker/pkg/lib/handler"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

const (
	jsonListColumn = "root.module_1_processdata"
	jsonErrorcode  = "root.module_1_processdata.0.errorcode"
	jsonResults    = "root.module_1_processdata.*.result"
	jsonStations   = "root.module_1_processdata.*.station"
)

func jsonPaths() map[string]model.JSONPath {
	return map[string]model.JSONPath{
		jsonErrorcode:                         {Column: jsonListColumn, Segments: []string{"0", "errorcode"}, SQLType: "bigint"},
		jsonResults:                           {Column: jsonListColumn, Segments: []string{"*", "result"}, SQLType: "bigint"},
		jsonStations:                          {Column: jsonListColumn, Segments: []string{"*", "station"}, SQLType: "bigint"},
		"root.module_1_processdata.1":         {Column: jsonListColumn, Segments: []string{"1"}},
		"root.module_1_processdata.1.main_id": {Column: jsonListColumn, Segments: []string{"1", "main_id"}, SQLType: "text"},
	}
}

func TestGenerateQueriesJSONPaths(t *testing.T) {
	wrapper := injWrapper()
	h1 := "1h"
	last := "last"
	first := "first"
	sum := "sum"
	mean := "mean"
	count := "count"
	diffLast := "difference-last"
	plus5 := "+5"
	d1 := &model.QueriesRequestElementTime{Last: strPtr("1d")}
	table := `"` + injTable + `"`
	col := `"` + jsonListColumn + `"`
	one := 1

	tt := []struct {
		name     string
		element  model.QueriesRequestElement
		expected string
	}{
		{
			name: "indexed value is cast to the type of the variable",
			element: model.QueriesRequestElement{
				Time:    d1,
				Limit:   &one,
				Columns: []model.QueriesRequestElementColumn{{Name: jsonErrorcode, Math: &plus5}},
			},
			expected: `SELECT "time", (jsonb_path_query_first(` + col + `, '$[0]."errorcode"'::jsonpath) #>> '{}')::bigint+5 AS "` + jsonErrorcode + `" FROM ` + table +
				` WHERE "time" > now() - interval '1d' LIMIT 1`,
		},
		{
			name: "element that is a structure stays jsonb",
			element: model.QueriesRequestElement{
				Columns: []model.QueriesRequestElementColumn{{Name: "root.module_1_processdata.1"}},
			},
			expected: `SELECT "time", jsonb_path_query_first(` + col + `, '$[1]'::jsonpath) AS "root.module_1_processdata.1" FROM ` + table,
		},
		{
			name: "wildcard without an aggregation is an array",
			element: model.QueriesRequestElement{
				Columns: []model.QueriesRequestElementColumn{{Name: jsonResults}, {Name: "root.level"}},
			},
			expected: `SELECT "time", jsonb_path_query_array(` + col + `, '$[*]."result"'::jsonpath) AS "` + jsonResults + `", "root.level" AS "root.level" FROM ` + table,
		},
		{
			name: "filter on a list path",
			element: model.QueriesRequestElement{
				Columns: []model.QueriesRequestElementColumn{{Name: "root.level"}},
				Filters: &[]model.QueriesRequestElementFilter{
					{Column: jsonErrorcode, Type: "!=", Value: 0},
					{Column: "root.module_1_processdata.1.main_id", Type: "=", Value: "abc"},
					{Column: jsonErrorcode, Type: "!=", Value: nil},
				},
			},
			expected: `SELECT "time", "root.level" AS "root.level" FROM ` + table +
				` WHERE (jsonb_path_query_first(` + col + `, '$[0]."errorcode"'::jsonpath) #>> '{}')::bigint!= 0` +
				` AND (jsonb_path_query_first(` + col + `, '$[1]."main_id"'::jsonpath) #>> '{}')::text= 'abc'` +
				` AND (jsonb_path_query_first(` + col + `, '$[0]."errorcode"'::jsonpath) #>> '{}')::bigint IS NOT NULL`,
		},
		{
			name: "aggregation over an index",
			element: model.QueriesRequestElement{
				Time:      d1,
				GroupTime: &h1,
				Columns:   []model.QueriesRequestElementColumn{{Name: jsonErrorcode, GroupType: &mean}, {Name: jsonErrorcode, GroupType: &last}},
			},
			expected: `SELECT sub0.time AS "time", (sub0.value) AS "` + jsonErrorcode + `", (sub1.value) AS "` + jsonErrorcode + `" FROM ` +
				`(SELECT time_bucket('1h', "time", 'Europe/Berlin') AS "time", avg((jsonb_path_query_first(` + col + `, '$[0]."errorcode"'::jsonpath) #>> '{}')::bigint) AS value FROM ` + table +
				` WHERE "time" > now() - interval '1d' GROUP BY 1 ORDER BY 1 ASC) sub0 FULL OUTER JOIN ` +
				`(SELECT time_bucket('1h', "time", 'Europe/Berlin') AS "time", last((jsonb_path_query_first(` + col + `, '$[0]."errorcode"'::jsonpath) #>> '{}')::bigint, "time") AS value FROM ` + table +
				` WHERE "time" > now() - interval '1d' GROUP BY 1 ORDER BY 1 DESC) sub1 on sub0.time = sub1.time`,
		},
		{
			name: "aggregation over all elements",
			element: model.QueriesRequestElement{
				Time:      d1,
				GroupTime: &h1,
				Columns:   []model.QueriesRequestElementColumn{{Name: jsonResults, GroupType: &sum}, {Name: jsonResults, GroupType: &count}},
			},
			expected: `SELECT sub0.time AS "time", (sub0.value) AS "` + jsonResults + `", (sub1.value) AS "` + jsonResults + `" FROM ` +
				`(SELECT time_bucket('1h', "time", 'Europe/Berlin') AS "time", sum(("__e"."__v" #>> '{}')::bigint) AS value FROM ` + table +
				` LEFT JOIN LATERAL jsonb_path_query(` + col + `, '$[*]."result"'::jsonpath) WITH ORDINALITY AS "__e"("__v", "__n") ON TRUE` +
				` WHERE "time" > now() - interval '1d' GROUP BY 1 ORDER BY 1 ASC) sub0 FULL OUTER JOIN ` +
				`(SELECT time_bucket('1h', "time", 'Europe/Berlin') AS "time", count(("__e"."__v" #>> '{}')::bigint) AS value FROM ` + table +
				` LEFT JOIN LATERAL jsonb_path_query(` + col + `, '$[*]."result"'::jsonpath) WITH ORDINALITY AS "__e"("__v", "__n") ON TRUE` +
				` WHERE "time" > now() - interval '1d' GROUP BY 1 ORDER BY 1 ASC) sub1 on sub0.time = sub1.time`,
		},
		{
			name: "first and last over all elements order by time and position",
			element: model.QueriesRequestElement{
				Time:      d1,
				GroupTime: &h1,
				Columns:   []model.QueriesRequestElementColumn{{Name: jsonResults, GroupType: &first}, {Name: jsonStations, GroupType: &last}},
			},
			expected: `SELECT sub0.time AS "time", (sub0.value) AS "` + jsonResults + `", (sub1.value) AS "` + jsonStations + `" FROM ` +
				`(SELECT time_bucket('1h', "time", 'Europe/Berlin') AS "time", (array_agg(("__e"."__v" #>> '{}')::bigint ORDER BY "time" ASC, "__e"."__n" ASC) FILTER (WHERE ("__e"."__v" #>> '{}')::bigint IS NOT NULL))[1] AS value FROM ` + table +
				` LEFT JOIN LATERAL jsonb_path_query(` + col + `, '$[*]."result"'::jsonpath) WITH ORDINALITY AS "__e"("__v", "__n") ON TRUE` +
				` WHERE "time" > now() - interval '1d' GROUP BY 1 ORDER BY 1 ASC) sub0 FULL OUTER JOIN ` +
				`(SELECT time_bucket('1h', "time", 'Europe/Berlin') AS "time", (array_agg(("__e"."__v" #>> '{}')::bigint ORDER BY "time" DESC, "__e"."__n" DESC) FILTER (WHERE ("__e"."__v" #>> '{}')::bigint IS NOT NULL))[1] AS value FROM ` + table +
				` LEFT JOIN LATERAL jsonb_path_query(` + col + `, '$[*]."station"'::jsonpath) WITH ORDINALITY AS "__e"("__v", "__n") ON TRUE` +
				` WHERE "time" > now() - interval '1d' GROUP BY 1 ORDER BY 1 DESC) sub1 on sub0.time = sub1.time`,
		},
		{
			name: "difference over all elements",
			element: model.QueriesRequestElement{
				Time:      d1,
				GroupTime: &h1,
				Columns:   []model.QueriesRequestElementColumn{{Name: jsonResults, GroupType: &diffLast}},
			},
			expected: `SELECT sub0.time AS "time", (sub0.value - lag(sub0.value) OVER (ORDER BY sub0.time)) AS "` + jsonResults + `" FROM ` +
				`(SELECT time_bucket('1h', "time", 'Europe/Berlin') AS "time", (array_agg(("__e"."__v" #>> '{}')::bigint ORDER BY "time" DESC, "__e"."__n" DESC) FILTER (WHERE ("__e"."__v" #>> '{}')::bigint IS NOT NULL))[1] AS value FROM ` + table +
				` LEFT JOIN LATERAL jsonb_path_query(` + col + `, '$[*]."result"'::jsonpath) WITH ORDINALITY AS "__e"("__v", "__n") ON TRUE` +
				` WHERE "time" > now() - interval '2d' GROUP BY 1 ORDER BY 1 ASC LIMIT 3) sub0 ORDER BY 1 DESC LIMIT 1`,
		},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			element := tc.element
			element.DeviceId = strPtr(injDeviceId)
			element.ServiceId = strPtr(injServiceId)
			element.JSONPaths = jsonPaths()
			queries, err := wrapper.GenerateQueries(t.Context(), []model.QueriesRequestElement{element}, "u", []string{"u"}, "", []models.Device{})
			if err != nil {
				t.Fatal(err)
			}
			if queries[0] != tc.expected {
				t.Errorf("expected\n%s\nactual\n%s", tc.expected, queries[0])
			}
		})
	}
}

// A list column with a name over 63 characters is hashed by the table worker, and so is the
// column the path is read from.
func TestGenerateQueriesJSONPathHashedColumn(t *testing.T) {
	long := "root." + strings.Repeat("a", 70)
	hashed := strings.Trim(util.HashFieldNameIfNeeded(long), `"`)
	if hashed == long {
		t.Fatal("name is not hashed")
	}
	element := model.QueriesRequestElement{
		DeviceId:  strPtr(injDeviceId),
		ServiceId: strPtr(injServiceId),
		Columns:   []model.QueriesRequestElementColumn{{Name: long + ".0.x"}},
		JSONPaths: map[string]model.JSONPath{long + ".0.x": {Column: long, Segments: []string{"0", "x"}, SQLType: "double precision"}},
	}
	queries, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{element}, "u", []string{"u"}, "", []models.Device{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queries[0], `jsonb_path_query_first("`+hashed+`", '$[0]."x"'::jsonpath)`) {
		t.Errorf("column was not hashed: %s", queries[0])
	}
	if strings.Contains(queries[0], "aaaa") && strings.Contains(queries[0], `jsonb_path_query_first("`+long) {
		t.Errorf("unhashed column in query: %s", queries[0])
	}
}

// The segments come from a request. Resolving them against the service rejects names that are not
// part of it, but the query generation must not depend on that.
func TestGenerateQueriesJSONPathInjection(t *testing.T) {
	segments := [][]string{
		{`x'::jsonpath) FROM (SELECT version()) z --`},
		{`x"`},
		{`x\`},
		{"x\nz"},
		{"x\x00y"},
		{`"] ? (@ == 1)`},
		{"0", "-1"},
		{"1234567890123"},
		{""},
	}
	for _, s := range segments {
		t.Run(strings.Join(s, "/"), func(t *testing.T) {
			element := model.QueriesRequestElement{
				DeviceId:  strPtr(injDeviceId),
				ServiceId: strPtr(injServiceId),
				Columns:   []model.QueriesRequestElementColumn{{Name: "root.list"}},
				JSONPaths: map[string]model.JSONPath{"root.list": {Column: "root.list", Segments: s, SQLType: "text"}},
			}
			queries, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{element}, "u", []string{"u"}, "", []models.Device{})
			if err != nil {
				return // rejected
			}
			// accepted values have to stay inside the quoted key, with quotes and backslashes escaped
			if strings.Contains(queries[0], "version()") && !strings.Contains(queries[0], `''::jsonpath) FROM`) {
				t.Errorf("segment left the literal: %s", queries[0])
			}
			if strings.Contains(queries[0], "\x00") || strings.Contains(queries[0], "\n") {
				t.Errorf("control character in query: %q", queries[0])
			}
		})
	}
	t.Run("sql type outside of the allowlist", func(t *testing.T) {
		element := model.QueriesRequestElement{
			DeviceId:  strPtr(injDeviceId),
			ServiceId: strPtr(injServiceId),
			Columns:   []model.QueriesRequestElementColumn{{Name: "root.list"}},
			JSONPaths: map[string]model.JSONPath{"root.list": {Column: "root.list", Segments: []string{"0"}, SQLType: "text) FROM x --"}},
		}
		if _, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{element}, "u", []string{"u"}, "", []models.Device{}); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("wildcard in a filter", func(t *testing.T) {
		element := model.QueriesRequestElement{
			DeviceId:  strPtr(injDeviceId),
			ServiceId: strPtr(injServiceId),
			Columns:   []model.QueriesRequestElementColumn{{Name: "root.level"}},
			Filters:   &[]model.QueriesRequestElementFilter{{Column: jsonResults, Type: "!=", Value: nil}},
			JSONPaths: jsonPaths(),
		}
		_, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{element}, "u", []string{"u"}, "", []models.Device{})
		if err == nil || model.GetStatusCode(err) != 400 {
			t.Errorf("expected a bad request, got %v", err)
		}
	})
}
