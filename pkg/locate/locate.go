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

// Package locate picks the station nearest to a target location out of several candidates that
// share one multivalued import's export, and builds the query that reads those candidates'
// locations. pkg/timescale's CreateFiltersForImport delegates here instead of keeping the only
// copy of this logic.
//
// This is a leaf package on purpose: it imports nothing beyond the standard library,
// github.com/SENERGY-Platform/timescale-wrapper/pkg/tablenames and github.com/jackc/pgx/v5 (only
// for identifier quoting), so a caller outside this module - the measures service - can pick the
// same nearest station without also pulling in the wrapper's database driver, tracing,
// import-repository and analytics-serving clients.
package locate

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/tablenames"
	"github.com/jackc/pgx/v5"
)

// MaterializedViewPrefix marks the materialized views a caller creates to deduplicate and cache
// the location lookup for a hypertable that otherwise holds one row per measurement rather than
// one row per station.
const MaterializedViewPrefix = "_wmv_"

// ViewName returns the materialized view name for the hypertable table.
func ViewName(table string) string {
	return MaterializedViewPrefix + table
}

// quoteIdentifier renders name as a quoted SQL identifier, escaping any embedded quotes. This
// mirrors pkg/timescale's own quoteIdentifier; it is kept as a second copy instead of an exported
// function because pgx.Identifier{}.Sanitize() is the only piece of pgx this package needs, and
// exporting it would widen this leaf package's API beyond what it is for.
func quoteIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// LocationsQuery builds the query that reads the distinct (idColumn, latColumn, lonColumn)
// combinations out of relation. relation is typically the materialized view of
// already-deduplicated locations kept for a hypertable (see ViewName); with since set, relation
// can instead be the raw hypertable itself, restricted to a recent time window so the DISTINCT
// has a bounded number of rows to work through instead of the hypertable's full history. since is
// rendered as a PostgreSQL interval literal (e.g. "24h", "7d") and is not otherwise validated - a
// malformed interval fails at the database, not here. The time column of the raw hypertable is
// fixed to "time" on this platform. An empty relation or an empty column name is an error.
func LocationsQuery(relation, idColumn, latColumn, lonColumn, since string) (string, error) {
	if relation == "" {
		return "", errors.New("locate: relation must not be empty")
	}
	if idColumn == "" || latColumn == "" || lonColumn == "" {
		return "", errors.New("locate: idColumn, latColumn and lonColumn must not be empty")
	}
	query := "SELECT DISTINCT " + quoteIdentifier(idColumn) + ", " + quoteIdentifier(latColumn) + ", " + quoteIdentifier(lonColumn) +
		" FROM " + quoteIdentifier(relation)
	if since != "" {
		query += ` WHERE "time" >= now() - interval ` + tablenames.QuoteSQLString(since)
	}
	query += ";"
	return query, nil
}

// Candidate is one station location to weigh against a target coordinate: an opaque identifier
// plus its latitude and longitude.
type Candidate struct {
	Id       any
	Lat, Lon float64
}

// earthRadiusKm is the mean Earth radius used by the haversine distance below. It matches
// github.com/umahmood/haversine's constant, which this function replaces so that this
// dependency-free leaf package does not have to pull in a library for one formula.
const earthRadiusKm = 6371.0

// Nearest picks the candidate with the smallest great-circle distance to (lat, lon). Candidates
// with a nil Id are skipped, since they cannot be turned into a usable filter value afterwards. A
// tie is broken deterministically by comparing fmt.Sprint(Id), so the result does not depend on
// the order candidates were passed in - a plain sort.Slice, which is what this replaced, is not
// stable across equal keys and could flip the choice between otherwise identical calls. ok is
// false when no candidate remains after skipping nil Ids.
func Nearest(candidates []Candidate, lat, lon float64) (c Candidate, ok bool) {
	best := -1
	var bestKm float64
	for i, cand := range candidates {
		if cand.Id == nil {
			continue
		}
		km := haversineKm(lat, lon, cand.Lat, cand.Lon)
		if best == -1 || km < bestKm || (km == bestKm && fmt.Sprint(cand.Id) < fmt.Sprint(candidates[best].Id)) {
			best, bestKm = i, km
		}
	}
	if best == -1 {
		return Candidate{}, false
	}
	return candidates[best], true
}

// haversineKm returns the great-circle distance in kilometers between (lat1, lon1) and
// (lat2, lon2), both given in degrees.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }
	rLat1, rLon1, rLat2, rLon2 := toRad(lat1), toRad(lon1), toRad(lat2), toRad(lon2)
	dLat := rLat2 - rLat1
	dLon := rLon2 - rLon1
	a := math.Pow(math.Sin(dLat/2), 2) + math.Cos(rLat1)*math.Cos(rLat2)*math.Pow(math.Sin(dLon/2), 2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return c * earthRadiusKm
}

// ValueLiteral renders v as a SQL literal. Values may originate from a request body or from a
// previous query (see Candidate.Id as scanned from LocationsQuery's result), so unknown types are
// rejected rather than printed with %v.
func ValueLiteral(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return tablenames.QuoteSQLString(val), nil
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
		return "", fmt.Errorf("unsupported filter value type %T", v)
	}
}
