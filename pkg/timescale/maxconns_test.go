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
	"math"
	"testing"

	"github.com/SENERGY-Platform/timescale-wrapper/pkg/configuration"
)

// TestMaxConnsFallsBackRatherThanPanicking is the case every deployment is in
// on the day this ships: a configuration file written before the field
// existed.
//
// pgxpool's own pool panics at a size below one, so passing the zero value
// through would take the process down at startup - and it would do it on
// every replica at once, which is the failure mode a default is for.
func TestMaxConnsFallsBackRatherThanPanicking(t *testing.T) {
	for name, held := range map[string]int64{
		"absent from the file": 0,
		"negative":             -1,
	} {
		t.Run(name, func(t *testing.T) {
			got := MaxConns(&configuration.ConfigStruct{PostgresMaxConns: held})
			if got != DefaultMaxConns {
				t.Errorf("got %v, want the default of %v", got, DefaultMaxConns)
			}
			if got < 1 {
				t.Errorf("got %v, which pgxpool panics on", got)
			}
		})
	}
}

// TestMaxConnsTakesTheConfiguredSize, which is the point of the field.
func TestMaxConnsTakesTheConfiguredSize(t *testing.T) {
	if got := MaxConns(&configuration.ConfigStruct{PostgresMaxConns: 50}); got != 50 {
		t.Errorf("got %v, want 50", got)
	}
	// The field is int64 because the environment reader handles Int64 and not
	// Int32; the narrowing may not wrap into a negative pool size.
	if got := MaxConns(&configuration.ConfigStruct{PostgresMaxConns: math.MaxInt64}); got != math.MaxInt32 {
		t.Errorf("got %v for an absurd size, want it clamped to %v", got, int32(math.MaxInt32))
	}
}

// TestTheShippedConfigurationNamesAPoolSize keeps the default in the file as
// well as in the code: the constant is the fallback for an old file, not the
// documentation of what this service runs with.
func TestTheShippedConfigurationNamesAPoolSize(t *testing.T) {
	// Read the file and nothing else: Load applies the environment on top,
	// and a POSTGRES_MAX_CONNS in the shell running the tests would make this
	// assert about that shell instead of about the file.
	t.Setenv("POSTGRES_MAX_CONNS", "")

	config, err := configuration.Load("../../config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.PostgresMaxConns <= 0 {
		t.Fatalf("config.json names no postgres_max_conns")
	}
	if got := MaxConns(config); got != DefaultMaxConns {
		t.Errorf("config.json says %v, the code's default says %v - one of the two moved", got, DefaultMaxConns)
	}
}
