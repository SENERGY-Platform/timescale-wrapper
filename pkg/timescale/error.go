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

import (
	"errors"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx"
	"net/http"
)

func GetHTTPErrorCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var pgErr pgx.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgerrcode.IsWarning(pgErr.Code):
			return http.StatusOK
		case pgerrcode.IsNoData(pgErr.Code):
			return http.StatusOK
		case pgerrcode.IsSQLStatementNotYetComplete(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsConnectionException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsTriggeredActionException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsFeatureNotSupported(pgErr.Code):
			return http.StatusBadRequest // unsupported operation not available on api
		case pgerrcode.IsInvalidTransactionInitiation(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsLocatorException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidGrantor(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidRoleSpecification(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsDiagnosticsException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsCaseNotFound(pgErr.Code):
			return http.StatusNotFound
		case pgerrcode.IsCardinalityViolation(pgErr.Code):
			return http.StatusBadRequest
		case pgerrcode.IsDataException(pgErr.Code):
			return http.StatusBadRequest
		case pgerrcode.IsIntegrityConstraintViolation(pgErr.Code):
			return http.StatusBadRequest
		case pgerrcode.IsInvalidCursorState(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidTransactionState(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidSQLStatementName(pgErr.Code):
			return http.StatusBadRequest
		case pgerrcode.IsTriggeredDataChangeViolation(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidAuthorizationSpecification(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsDependentPrivilegeDescriptorsStillExist(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidTransactionTermination(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsSQLRoutineException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidCursorName(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsExternalRoutineException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsExternalRoutineInvocationException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsSavepointException(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidCatalogName(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInvalidSchemaName(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsTransactionRollback(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsSyntaxErrororAccessRuleViolation(pgErr.Code):
			return http.StatusBadRequest
		case pgerrcode.IsWithCheckOptionViolation(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInsufficientResources(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsProgramLimitExceeded(pgErr.Code):
			return http.StatusBadRequest
		case pgerrcode.IsObjectNotInPrerequisiteState(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsOperatorIntervention(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsSystemError(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsSnapshotFailure(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsConfigurationFileError(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsForeignDataWrapperError(pgErr.Code):
			return http.StatusBadRequest
		case pgerrcode.IsPLpgSQLError(pgErr.Code):
			return http.StatusBadGateway
		case pgerrcode.IsInternalError(pgErr.Code):
			return http.StatusBadGateway
		}
	}
	return http.StatusInternalServerError
}
