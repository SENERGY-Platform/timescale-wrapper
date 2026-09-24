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

package locate

import (
	"encoding/json"
	"testing"
)

func TestLocationsQuery(t *testing.T) {
	actual, err := LocationsQuery("mytable", "id", "lat", "lon", "")
	if err != nil {
		t.Fatal(err)
	}
	expected := `SELECT DISTINCT "id", "lat", "lon" FROM "mytable";`
	if actual != expected {
		t.Error("Expected/Actual\n", expected, "\n", actual)
	}
}

func TestLocationsQueryWithSince(t *testing.T) {
	actual, err := LocationsQuery("mytable", "id", "lat", "lon", "24h")
	if err != nil {
		t.Fatal(err)
	}
	expected := `SELECT DISTINCT "id", "lat", "lon" FROM "mytable" WHERE "time" >= now() - interval '24h';`
	if actual != expected {
		t.Error("Expected/Actual\n", expected, "\n", actual)
	}
}

func TestLocationsQueryRejectsEmptyColumn(t *testing.T) {
	if _, err := LocationsQuery("mytable", "", "lat", "lon", ""); err == nil {
		t.Error("expected an error for an empty id column")
	}
	if _, err := LocationsQuery("mytable", "id", "", "lon", ""); err == nil {
		t.Error("expected an error for an empty lat column")
	}
	if _, err := LocationsQuery("mytable", "id", "lat", "", ""); err == nil {
		t.Error("expected an error for an empty lon column")
	}
}

func TestLocationsQueryRejectsEmptyRelation(t *testing.T) {
	if _, err := LocationsQuery("", "id", "lat", "lon", ""); err == nil {
		t.Error("expected an error for an empty relation")
	}
}

// TestLocationsQueryEscapesQuotedColumn makes sure a column name that itself contains a double
// quote is escaped instead of letting it break out of the identifier it is quoted into.
func TestLocationsQueryEscapesQuotedColumn(t *testing.T) {
	actual, err := LocationsQuery("mytable", `id"1`, "lat", "lon", "")
	if err != nil {
		t.Fatal(err)
	}
	expected := `SELECT DISTINCT "id""1", "lat", "lon" FROM "mytable";`
	if actual != expected {
		t.Error("Expected/Actual\n", expected, "\n", actual)
	}
}

func TestNearestPicksTheClosestOfThree(t *testing.T) {
	candidates := []Candidate{
		{Id: "far", Lat: 2, Lon: 0},
		{Id: "near", Lat: 0.1, Lon: 0},
		{Id: "farther", Lat: 4, Lon: 0},
	}
	c, ok := Nearest(candidates, 0, 0)
	if !ok {
		t.Fatal("expected ok")
	}
	if c.Id != "near" {
		t.Error("expected near, got", c.Id)
	}
}

// TestNearestBreaksTiesDeterministically sets up two candidates that are exactly equidistant from
// the target (symmetric around it, so the haversine distance is identical) and calls Nearest
// twice with the candidate order reversed. Without a deterministic tie-break, the previous
// sort.Slice-based implementation could return either one depending on scan order; this pins the
// choice to the lower fmt.Sprint(Id).
func TestNearestBreaksTiesDeterministically(t *testing.T) {
	a := Candidate{Id: "A", Lat: 1, Lon: 0}
	b := Candidate{Id: "B", Lat: -1, Lon: 0}

	c1, ok := Nearest([]Candidate{a, b}, 0, 0)
	if !ok {
		t.Fatal("expected ok")
	}
	c2, ok := Nearest([]Candidate{b, a}, 0, 0)
	if !ok {
		t.Fatal("expected ok")
	}
	if c1.Id != c2.Id {
		t.Error("expected the same candidate regardless of input order, got", c1.Id, "and", c2.Id)
	}
	if c1.Id != "A" {
		t.Error("expected the tie to be broken towards the lower fmt.Sprint(Id), got", c1.Id)
	}
}

func TestNearestEmptyOrOnlyNilIds(t *testing.T) {
	if _, ok := Nearest(nil, 0, 0); ok {
		t.Error("expected ok == false for an empty candidate list")
	}
	candidates := []Candidate{{Id: nil, Lat: 1, Lon: 1}, {Id: nil, Lat: 2, Lon: 2}}
	if _, ok := Nearest(candidates, 0, 0); ok {
		t.Error("expected ok == false when every candidate has a nil Id")
	}
}

// TestNearestUsesLatLonInTheRightOrder guards against a swapped lat/lon assignment, which a
// distance formula alone would not catch since swapping both coordinates of both points can
// still yield a symmetric (and therefore misleadingly plausible) result. Berlin (52.52, 13.405)
// to Hamburg (53.55, 9.993) is a real, nearby pair (roughly 255km); a decoy candidate is placed
// at Hamburg's lat and lon swapped, which is a real but distant point (in the Gulf of Aden). Only
// a correct, unswapped calculation picks the true Hamburg candidate over the decoy.
func TestNearestUsesLatLonInTheRightOrder(t *testing.T) {
	const berlinLat, berlinLon = 52.52, 13.405
	hamburg := Candidate{Id: "hamburg", Lat: 53.55, Lon: 9.993}
	decoy := Candidate{Id: "decoy-swapped", Lat: 9.993, Lon: 53.55}

	c, ok := Nearest([]Candidate{hamburg, decoy}, berlinLat, berlinLon)
	if !ok {
		t.Fatal("expected ok")
	}
	if c.Id != "hamburg" {
		t.Error("expected hamburg to be nearest to berlin, got", c.Id)
	}
}

func TestValueLiteral(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		expected string
	}{
		{"string", "abc", "'abc'"},
		{"string with embedded quote", "a'bc", "'a''bc'"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"float64", float64(1.5), "1.5"},
		{"float32", float32(1.5), "1.5"},
		{"int", int(42), "42"},
		{"int16", int16(42), "42"},
		{"int32", int32(42), "42"},
		{"int64", int64(42), "42"},
		{"json.Number", json.Number("42.5"), "42.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual, err := ValueLiteral(c.value)
			if err != nil {
				t.Fatal(err)
			}
			if actual != c.expected {
				t.Error("Expected/Actual\n", c.expected, "\n", actual)
			}
		})
	}
}

func TestValueLiteralRejectsInvalidJsonNumber(t *testing.T) {
	if _, err := ValueLiteral(json.Number("not-a-number")); err == nil {
		t.Error("expected an error for an invalid json.Number")
	}
}

func TestValueLiteralRejectsUnknownType(t *testing.T) {
	if _, err := ValueLiteral(struct{}{}); err == nil {
		t.Error("expected an error for an unsupported type")
	}
}
