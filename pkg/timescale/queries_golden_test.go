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

package timescale

// Characterization ("golden") tests for GenerateQueries. Each case freezes the exact SQL text that
// GenerateQueries produces, so that a refactoring can be checked for byte-identical output. A
// golden file is rewritten with `go test ./pkg/timescale/ -run TestGenerateQueriesGolden -update`;
// review the diff of testdata/golden before committing such a change.
//
// Known gap: the wrapper is built without a readPool, so tableName never looks up a continuous
// aggregate. The CA substitution in tableName is not covered by these goldens.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

const (
	goldenDeviceId  = "urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4"
	goldenServiceId = "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050"
	goldenColumn    = "sensor.ENERGY.Total"
	goldenStart     = "2026-01-01T00:00:00Z"
	goldenEnd       = "2026-01-08T00:00:00Z"
)

func ptr[T any](v T) *T { return &v }

func timeLast(interval string) *model.QueriesRequestElementTime {
	return &model.QueriesRequestElementTime{Last: ptr(interval)}
}

func timeAhead(interval string) *model.QueriesRequestElementTime {
	return &model.QueriesRequestElementTime{Ahead: ptr(interval)}
}

func timeRange() *model.QueriesRequestElementTime {
	return &model.QueriesRequestElementTime{Start: ptr(goldenStart), End: ptr(goldenEnd)}
}

func plainColumn(name string) model.QueriesRequestElementColumn {
	return model.QueriesRequestElementColumn{Name: name}
}

func plainColumnMath(name string, math string) model.QueriesRequestElementColumn {
	return model.QueriesRequestElementColumn{Name: name, Math: ptr(math)}
}

func groupedColumn(name string, groupType string) model.QueriesRequestElementColumn {
	return model.QueriesRequestElementColumn{Name: name, GroupType: ptr(groupType)}
}

func filters(fs ...model.QueriesRequestElementFilter) *[]model.QueriesRequestElementFilter {
	return &fs
}

func deviceElement(time *model.QueriesRequestElementTime, columns ...model.QueriesRequestElementColumn) model.QueriesRequestElement {
	return model.QueriesRequestElement{
		DeviceId:  ptr(goldenDeviceId),
		ServiceId: ptr(goldenServiceId),
		Time:      time,
		Columns:   columns,
	}
}

func goldenJSONPaths(name string, path model.JSONPath) map[string]model.JSONPath {
	return map[string]model.JSONPath{name: path}
}

type goldenCase struct {
	name         string
	elements     []model.QueriesRequestElement
	ownerUserIds []string // one per element; nil means empty strings
	forceTz      string
	devices      []models.Device
}

func goldenCases() []goldenCase {
	const total = goldenColumn
	const listScalar = "root.List.0.Value"
	const listWildcard = "root.List.*.Value"
	return []goldenCase{
		{
			name:     "raw_single_column",
			elements: []model.QueriesRequestElement{deviceElement(timeLast("1d"), plainColumn(total))},
		},
		{
			name: "raw_filter_math",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn(total))
				e.Filters = filters(model.QueriesRequestElementFilter{Column: total, Math: ptr("+5"), Type: ">", Value: 10})
				return e
			}()},
		},
		{
			name: "raw_filter_null_eq",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn(total))
				e.Filters = filters(model.QueriesRequestElementFilter{Column: total, Type: "="})
				return e
			}()},
		},
		{
			name: "raw_filter_null_ne",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn(total))
				e.Filters = filters(model.QueriesRequestElementFilter{Column: total, Type: "!="})
				return e
			}()},
		},
		{
			name: "raw_string_filter",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn(total))
				e.Filters = filters(model.QueriesRequestElementFilter{Column: total, Type: "=", Value: "on"})
				return e
			}()},
		},
		{
			name: "raw_order_limit",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn(total), plainColumn("sensor.POWER.Current"))
				e.OrderColumnIndex = ptr(1)
				e.OrderDirection = ptr(model.Asc)
				e.Limit = ptr(10)
				return e
			}()},
		},
		{
			name:     "raw_column_math",
			elements: []model.QueriesRequestElement{deviceElement(timeLast("1d"), plainColumnMath(total, "*2"))},
		},
		{
			name:     "grouped_mean",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "mean")), "1h")},
		},
		{
			name:     "grouped_first",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "first")), "1h")},
		},
		{
			name:     "grouped_last",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "last")), "1h")},
		},
		{
			name: "grouped_mean_and_last_three_columns",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("1d"),
				groupedColumn(total, "mean"), groupedColumn(total, "last"), groupedColumn(total, "max")), "1h")},
		},
		{
			name: "grouped_with_filter",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "mean")), "1h")
				e.Filters = filters(model.QueriesRequestElementFilter{Column: total, Math: ptr("+5"), Type: ">", Value: 10})
				return e
			}()},
		},
		{
			name:     "difference_last_with_last",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("7d"), groupedColumn(total, "difference-last")), "1d")},
		},
		{
			name:     "difference_mean_with_ahead",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeAhead("3d"), groupedColumn(total, "difference-mean")), "1d")},
		},
		{
			name:     "difference_first_start_end",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeRange(), groupedColumn(total, "difference-first")), "1h")},
		},
		{
			name: "difference_and_plain_columns",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("7d"),
				groupedColumn(total, "difference-last"), groupedColumn(total, "mean")), "1d")},
		},
		{
			name:     "difference_group_6h",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeRange(), groupedColumn(total, "difference-mean")), "6h")},
		},
		{
			name:     "time_weighted_locf",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeRange(), groupedColumn(total, "time-weighted-mean-locf")), "1h")},
		},
		{
			name:     "time_weighted_linear",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeRange(), groupedColumn(total, "time-weighted-mean-linear")), "1h")},
		},
		{
			name: "time_weighted_with_mean_column",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeRange(),
				groupedColumn(total, "time-weighted-mean-locf"), groupedColumn(total, "mean")), "1h")},
		},
		{
			name: "json_scalar_raw",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn("root.List.0.Value"))
				e.JSONPaths = goldenJSONPaths("root.List.0.Value", model.JSONPath{Column: "root.List", Segments: []string{"0", "Value"}, SQLType: "double precision"})
				return e
			}()},
		},
		{
			name: "json_wildcard_raw",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn(listWildcard))
				e.JSONPaths = goldenJSONPaths(listWildcard, model.JSONPath{Column: "root.List", Segments: []string{"*", "Value"}})
				return e
			}()},
		},
		{
			name: "json_wildcard_grouped_last",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := withGroup(deviceElement(timeLast("1d"), groupedColumn(listWildcard, "last")), "1h")
				e.JSONPaths = goldenJSONPaths(listWildcard, model.JSONPath{Column: "root.List", Segments: []string{"*", "Value"}, SQLType: "double precision"})
				return e
			}()},
		},
		{
			name: "json_wildcard_grouped_mean",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := withGroup(deviceElement(timeLast("1d"), groupedColumn(listWildcard, "mean")), "1h")
				e.JSONPaths = goldenJSONPaths(listWildcard, model.JSONPath{Column: "root.List", Segments: []string{"*", "Value"}, SQLType: "double precision"})
				return e
			}()},
		},
		{
			name: "json_filter_scalar",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("1d"), plainColumn(listScalar))
				e.Filters = filters(model.QueriesRequestElementFilter{Column: listScalar, Type: ">", Value: 10})
				e.JSONPaths = goldenJSONPaths(listScalar, model.JSONPath{Column: "root.List", Segments: []string{"0", "Value"}, SQLType: "double precision"})
				return e
			}()},
		},
		{
			name:     "force_tz",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "mean")), "1h")},
			forceTz:  "America/New_York",
		},
		{
			name:     "device_timezone_attribute",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "mean")), "1h")},
			devices: []models.Device{{
				Id:         goldenDeviceId,
				Attributes: []models.Attribute{{Key: "Timezone", Value: "Asia/Tokyo"}},
			}},
		},
		{
			name: "export_element",
			elements: []model.QueriesRequestElement{withGroup(model.QueriesRequestElement{
				ExportId: ptr("1b6a3e1c-0d8f-4a51-9e43-2f6b5c7d8e90"),
				Time:     timeLast("1d"),
				Columns:  []model.QueriesRequestElementColumn{groupedColumn(total, "mean")},
			}, "1h")},
			ownerUserIds: []string{"97805820-ca0a-46c5-9dcf-16c2e386b050"},
		},
		{
			name: "multiple_elements",
			elements: []model.QueriesRequestElement{
				deviceElement(timeLast("1d"), plainColumn(total)),
				withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "mean")), "1h"),
			},
		},
		{
			// the first column aggregates, the second does not: GenerateQueries rejects the mix
			name:     "error_mixed_aggregate",
			elements: []model.QueriesRequestElement{withGroup(deviceElement(timeLast("1d"), groupedColumn(total, "mean"), plainColumn(total)), "1h")},
		},
		{
			name: "long_field_name_hashing",
			elements: []model.QueriesRequestElement{func() model.QueriesRequestElement {
				e := deviceElement(timeLast("7d"), plainColumn(total),
					plainColumn("thisisatestforveryveryveryveryveryverylongfieldnameswhichneedtobehashed"))
				e.Limit = ptr(10)
				e.OrderColumnIndex = ptr(0)
				e.OrderDirection = ptr(model.Desc)
				return e
			}()},
		},
	}
}

func withGroup(element model.QueriesRequestElement, groupTime string) model.QueriesRequestElement {
	element.GroupTime = ptr(groupTime)
	return element
}

// goldenOutput renders the result of GenerateQueries as one string: the queries joined by a
// separator line, or the error prefixed with "ERROR: ".
func goldenOutput(queries []string, err error) string {
	if err != nil {
		return "ERROR: " + err.Error()
	}
	return strings.Join(queries, "\n-- next query --\n")
}

func TestGenerateQueriesGolden(t *testing.T) {
	// readPool is nil, so the continuous-aggregate lookup in tableName is skipped (see the comment
	// at the top of this file).
	wrapper := &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: "Europe/Berlin"}}
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			owners := tc.ownerUserIds
			if owners == nil {
				owners = make([]string, len(tc.elements))
			}
			queries, err := wrapper.GenerateQueries(t.Context(), tc.elements, "user1", owners, tc.forceTz, tc.devices)
			got := goldenOutput(queries, err)
			path := filepath.Join("testdata", "golden", tc.name+".sql")

			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			wantBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("case %s: %v (run with -update to create the golden file)", tc.name, err)
			}
			if want := string(wantBytes); got != want {
				t.Errorf("case %s: output differs from %s\nwant:\n%s\n\ngot:\n%s", tc.name, path, want, got)
			}
		})
	}
}
