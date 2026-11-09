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
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestNumericValue pins how numeric results reach the response: integral values as int64, all
// others as float64. Before, every numeric was cut to an int64, so the mean of 2 and 3 over a
// bigint column came back as 2.
func TestNumericValue(t *testing.T) {
	huge, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	cases := []struct {
		name     string
		in       pgtype.Numeric
		expected interface{}
	}{
		{"fraction", pgtype.Numeric{Int: big.NewInt(25), Exp: -1, Valid: true}, 2.5},
		{"integral with scale", pgtype.Numeric{Int: big.NewInt(20000), Exp: -4, Valid: true}, int64(2)},
		{"positive exponent", pgtype.Numeric{Int: big.NewInt(5), Exp: 2, Valid: true}, int64(500)},
		{"negative fraction", pgtype.Numeric{Int: big.NewInt(-125), Exp: -2, Valid: true}, -1.25},
		{"zero without int", pgtype.Numeric{Valid: true}, int64(0)},
		{"beyond int64", pgtype.Numeric{Int: huge, Exp: 0, Valid: true}, 1.2345678901234568e29},
		{"beyond float64", pgtype.Numeric{Int: big.NewInt(1), Exp: 400, Valid: true}, nil},
		{"null", pgtype.Numeric{}, nil},
		{"NaN", pgtype.Numeric{NaN: true, Valid: true}, nil},
		{"infinity", pgtype.Numeric{InfinityModifier: pgtype.Infinity, Valid: true}, nil},
		{"negative infinity", pgtype.Numeric{InfinityModifier: pgtype.NegativeInfinity, Valid: true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := numericValue(c.in)
			if !reflect.DeepEqual(actual, c.expected) {
				t.Errorf("expected %#v, got %#v", c.expected, actual)
			}
		})
	}
}

// TestNumericResultsAgainstDatabase runs the aggregates that return numeric over a bigint column
// through executeQuery. Skipped when TEST_POSTGRES_DSN is unset.
func TestNumericResultsAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := t.Context()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	res, err := executeQuery(ctx, pool, `SELECT avg(x), sum(x), avg(y), max(x) * 1.5, 'NaN'::numeric, NULL::numeric
		FROM (VALUES (2::bigint, 2::bigint), (3::bigint, 4::bigint)) AS t(x, y)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("expected one row, got %d", len(res))
	}
	expected := []interface{}{2.5, int64(5), int64(3), 4.5, nil, nil}
	if !reflect.DeepEqual(res[0], expected) {
		t.Errorf("expected %#v, got %#v", expected, res[0])
	}
}
