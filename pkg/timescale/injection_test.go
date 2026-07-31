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
	"regexp"
	"strings"
	"testing"

	"github.com/SENERGY-Platform/models/go/models"
	util "github.com/SENERGY-Platform/timescale-tableworker/pkg/lib/handler"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
	"github.com/SENERGY-Platform/timescale-wrapper/pkg/model"
)

const injDeviceId = "urn:infai:ses:device:ade1fba6-fa5f-4704-9997-81dc168f62f4"
const injServiceId = "urn:infai:ses:service:97805820-ca0a-46c5-9dcf-16c2e386b050"
const injTable = "device:reH7pvpfRwSZl4HcFo9i9A_service:l4BYIMoKRsWdzxbC44awUA"

func injWrapper() *Wrapper {
	return &Wrapper{config: &configuration.ConfigStruct{DefaultTimezone: "Europe/Berlin"}}
}

func strPtr(s string) *string { return &s }

// groupedElement builds a request that reaches the time_bucket branch, where the timezone is
// placed into the query.
func groupedElement() model.QueriesRequestElement {
	return model.QueriesRequestElement{
		DeviceId:  strPtr(injDeviceId),
		ServiceId: strPtr(injServiceId),
		GroupTime: strPtr("1h"),
		Time:      &model.QueriesRequestElementTime{Last: strPtr("1d")},
		Columns: []model.QueriesRequestElementColumn{{
			Name:      "sensor.ENERGY.Total",
			GroupType: strPtr("mean"),
		}},
	}
}

// A force_tz that closes the string literal used to be able to append arbitrary SQL, including
// a FROM against a table the caller has no access to.
func TestForceTzInjectionRejected(t *testing.T) {
	payloads := []string{
		`UTC') AS "time", version() AS value FROM (SELECT now() AS "time") z) sub0 --`,
		`UTC'`,
		`UTC''`,
		`UTC') --`,
		"UTC\x00",
		`UTC" `,
		strings.Repeat("a", 65),
	}
	for _, payload := range payloads {
		t.Run(payload, func(t *testing.T) {
			if model.TimezoneValid(payload) {
				t.Fatalf("TimezoneValid accepted %q", payload)
			}
			_, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{groupedElement()},
				"u", []string{"u"}, payload, []models.Device{})
			if err == nil {
				t.Fatalf("GenerateQueries accepted force_tz %q", payload)
			}
		})
	}
}

func TestForceTzValidAccepted(t *testing.T) {
	for _, tz := range []string{"UTC", "Europe/Berlin", "America/Argentina/Buenos_Aires", "Etc/GMT+5"} {
		t.Run(tz, func(t *testing.T) {
			if !model.TimezoneValid(tz) {
				t.Fatalf("TimezoneValid rejected %q", tz)
			}
			queries, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{groupedElement()},
				"u", []string{"u"}, tz, []models.Device{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(queries[0], "'"+tz+"'") {
				t.Errorf("timezone missing from query: %s", queries[0])
			}
		})
	}
}

// Device attributes come from the device repository and are not trusted either. An unusable
// value falls back to the configured default instead of reaching the query.
func TestDeviceTimezoneAttributeInjectionRejected(t *testing.T) {
	device := models.Device{
		Id: injDeviceId,
		Attributes: []models.Attribute{{
			Key:   "timezone",
			Value: `UTC') AS "time", version() AS value FROM (SELECT now() AS "time") z) sub0 --`,
		}},
	}
	queries, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{groupedElement()},
		"u", []string{"u"}, "", []models.Device{device})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queries[0], "'Europe/Berlin'") {
		t.Errorf("expected fallback to default timezone, got: %s", queries[0])
	}
	if strings.Contains(queries[0], "version()") {
		t.Errorf("injected sql reached the query: %s", queries[0])
	}
}

var quotedIdentifier = regexp.MustCompile(`"(?:[^"]|"")*"`)
var quotedLiteral = regexp.MustCompile(`'(?:[^']|'')*'`)

// sqlSkeleton removes every quoted identifier and string literal from a query, leaving the part
// that postgres reads as SQL. Attacker controlled input must never show up in it.
func sqlSkeleton(query string) string {
	query = quotedIdentifier.ReplaceAllString(query, `"?"`)
	return quotedLiteral.ReplaceAllString(query, "'?'")
}

func assertContained(t *testing.T, query string, fragments ...string) {
	t.Helper()
	skeleton := sqlSkeleton(query)
	for _, fragment := range fragments {
		if strings.Contains(skeleton, fragment) {
			t.Errorf("%q escaped quoting\nquery:    %s\nskeleton: %s", fragment, query, skeleton)
		}
	}
}

// Column names are resolved from device selection on the device group path and are therefore
// not covered by request validation. A quote in a name must not escape the identifier.
func TestColumnNameQuoteEscaped(t *testing.T) {
	element := model.QueriesRequestElement{
		DeviceId:  strPtr(injDeviceId),
		ServiceId: strPtr(injServiceId),
		Columns: []model.QueriesRequestElementColumn{{
			Name: `x" FROM (SELECT now() AS "time", version() AS "x") z --`,
		}},
	}
	queries, err := injWrapper().GenerateQueries(t.Context(), []model.QueriesRequestElement{element},
		"u", []string{"u"}, "", []models.Device{})
	if err != nil {
		t.Fatal(err)
	}
	assertContained(t, queries[0], "SELECT now()", "version()", "--")
	if !strings.HasSuffix(queries[0], " FROM "+quoteIdentifier(injTable)) {
		t.Errorf("query does not end with the authorized table: %s", queries[0])
	}
}

func TestFilterValueLiteral(t *testing.T) {
	t.Run("string quotes are doubled", func(t *testing.T) {
		got, err := filterValueLiteral(`a' OR 1=1 --`)
		if err != nil {
			t.Fatal(err)
		}
		if got != `'a'' OR 1=1 --'` {
			t.Errorf("unexpected literal %s", got)
		}
	})
	t.Run("unsupported types are rejected", func(t *testing.T) {
		for _, v := range []interface{}{
			map[string]interface{}{"1 OR 1=1 --": 1},
			[]interface{}{"1 OR 1=1 --"},
			struct{}{},
		} {
			if _, err := filterValueLiteral(v); err == nil {
				t.Errorf("accepted unsupported filter value %#v", v)
			}
		}
	})
	t.Run("numbers keep their rendering", func(t *testing.T) {
		for value, expected := range map[interface{}]string{
			10:          "10",
			float64(10): "10",
			10.5:        "10.5",
			int64(-3):   "-3",
			true:        "true",
		} {
			got, err := filterValueLiteral(value)
			if err != nil {
				t.Fatal(err)
			}
			if got != expected {
				t.Errorf("filterValueLiteral(%v) = %s, want %s", value, got, expected)
			}
		}
	})
}

// The continuous aggregate lookup embeds the timezone, table and column names into string
// literals of a regex, so quotes have to be doubled there as well.
func TestGetCAQueryEscaping(t *testing.T) {
	element := model.QueriesRequestElement{
		GroupTime: strPtr("1h"),
		Columns: []model.QueriesRequestElementColumn{{
			Name:      `a' OR 1=1 --`,
			GroupType: strPtr("sum"),
		}},
	}
	query, err := getCAQuery(element, `tbl' OR 1=1 --`, `UTC' OR 1=1 --`)
	if err != nil {
		t.Fatal(err)
	}
	assertContained(t, query, "OR 1=1", "--")
}

func TestQuoteColumnNameMatchesUpstreamForValidNames(t *testing.T) {
	// legitimate names must be rendered exactly as before, including the hash for long names
	for _, name := range []string{
		"value",
		"sensor.ENERGY.Total",
		strings.Repeat("a", 63),
		strings.Repeat("a", 64),
	} {
		if got, want := quoteColumnName(name), util.HashFieldNameIfNeeded(name); got != want {
			t.Errorf("quoteColumnName(%q) = %s, want %s", name, got, want)
		}
	}
}
