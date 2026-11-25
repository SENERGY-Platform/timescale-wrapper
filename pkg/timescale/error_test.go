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
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestGetHTTPErrorCode(t *testing.T) {
	pgErr := func(code string) error { return &pgconn.PgError{Code: code} }

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusOK},
		{"no pg error", errors.New("boom"), http.StatusInternalServerError},
		{"wrapped pg error is found", fmt.Errorf("query failed: %w", pgErr(pgerrcode.DivisionByZero)), http.StatusBadRequest},
		{"unknown sqlstate class", pgErr("99999"), http.StatusInternalServerError},
		{"empty sqlstate", pgErr(""), http.StatusInternalServerError},

		{"class 01 warning", pgErr(pgerrcode.Warning), http.StatusOK},
		{"class 02 no data", pgErr(pgerrcode.NoData), http.StatusOK},
		{"class 03 statement not yet complete", pgErr(pgerrcode.SQLStatementNotYetComplete), http.StatusBadGateway},
		{"class 08 connection exception", pgErr(pgerrcode.ConnectionFailure), http.StatusBadGateway},
		{"class 09 triggered action exception", pgErr(pgerrcode.TriggeredActionException), http.StatusBadGateway},
		{"class 0A feature not supported", pgErr(pgerrcode.FeatureNotSupported), http.StatusBadRequest},
		{"class 0B invalid transaction initiation", pgErr(pgerrcode.InvalidTransactionInitiation), http.StatusBadGateway},
		{"class 0F locator exception", pgErr(pgerrcode.LocatorException), http.StatusBadGateway},
		{"class 0L invalid grantor", pgErr(pgerrcode.InvalidGrantor), http.StatusBadGateway},
		{"class 0P invalid role specification", pgErr(pgerrcode.InvalidRoleSpecification), http.StatusBadGateway},
		{"class 0Z diagnostics exception", pgErr(pgerrcode.DiagnosticsException), http.StatusBadGateway},
		{"class 20 case not found", pgErr(pgerrcode.CaseNotFound), http.StatusNotFound},
		{"class 21 cardinality violation", pgErr(pgerrcode.CardinalityViolation), http.StatusBadRequest},
		{"class 22 data exception", pgErr(pgerrcode.DivisionByZero), http.StatusBadRequest},
		{"class 23 integrity constraint violation", pgErr(pgerrcode.UniqueViolation), http.StatusBadRequest},
		{"class 24 invalid cursor state", pgErr(pgerrcode.InvalidCursorState), http.StatusBadGateway},
		{"class 25 invalid transaction state", pgErr(pgerrcode.InvalidTransactionState), http.StatusBadGateway},
		{"class 26 invalid sql statement name", pgErr(pgerrcode.InvalidSQLStatementName), http.StatusBadRequest},
		{"class 27 triggered data change violation", pgErr(pgerrcode.TriggeredDataChangeViolation), http.StatusBadGateway},
		{"class 28 invalid authorization specification", pgErr(pgerrcode.InvalidAuthorizationSpecification), http.StatusBadGateway},
		{"class 2B dependent privilege descriptors", pgErr(pgerrcode.DependentPrivilegeDescriptorsStillExist), http.StatusBadGateway},
		{"class 2D invalid transaction termination", pgErr(pgerrcode.InvalidTransactionTermination), http.StatusBadGateway},
		{"class 2F sql routine exception", pgErr(pgerrcode.SQLRoutineException), http.StatusBadGateway},
		{"class 34 invalid cursor name", pgErr(pgerrcode.InvalidCursorName), http.StatusBadGateway},
		{"class 38 external routine exception", pgErr(pgerrcode.ExternalRoutineException), http.StatusBadGateway},
		{"class 39 external routine invocation exception", pgErr(pgerrcode.ExternalRoutineInvocationException), http.StatusBadGateway},
		{"class 3B savepoint exception", pgErr(pgerrcode.SavepointException), http.StatusBadGateway},
		{"class 3D invalid catalog name", pgErr(pgerrcode.InvalidCatalogName), http.StatusBadGateway},
		{"class 3F invalid schema name", pgErr(pgerrcode.InvalidSchemaName), http.StatusBadGateway},
		{"class 40 transaction rollback", pgErr(pgerrcode.TransactionRollback), http.StatusBadGateway},
		{"class 42 syntax error or access rule violation", pgErr(pgerrcode.SyntaxErrorOrAccessRuleViolation), http.StatusBadRequest},
		{"class 44 with check option violation", pgErr(pgerrcode.WithCheckOptionViolation), http.StatusBadGateway},
		{"class 53 insufficient resources", pgErr(pgerrcode.InsufficientResources), http.StatusBadGateway},
		{"class 54 program limit exceeded", pgErr(pgerrcode.ProgramLimitExceeded), http.StatusBadRequest},
		{"class 55 object not in prerequisite state", pgErr(pgerrcode.ObjectNotInPrerequisiteState), http.StatusBadGateway},
		{"class 57 operator intervention", pgErr(pgerrcode.OperatorIntervention), http.StatusBadGateway},
		{"class 58 system error", pgErr(pgerrcode.SystemError), http.StatusBadGateway},
		{"class 72 snapshot failure", pgErr(pgerrcode.SnapshotTooOld), http.StatusBadGateway},
		{"class F0 configuration file error", pgErr(pgerrcode.ConfigFileError), http.StatusBadGateway},
		{"class HV foreign data wrapper error", pgErr(pgerrcode.FDWError), http.StatusBadRequest},
		{"class P0 plpgsql error", pgErr(pgerrcode.PLpgSQLError), http.StatusBadGateway},
		{"class XX internal error", pgErr(pgerrcode.InternalError), http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetHTTPErrorCode(tt.err); got != tt.want {
				t.Errorf("GetHTTPErrorCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
