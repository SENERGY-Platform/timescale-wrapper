/*
 * Copyright 2026 InfAI (CC SES)
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
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

// handledSQLStates is the switch of GetHTTPErrorCode before it became a lookup
// table: one group per pgerrcode predicate, listing every code the predicate
// accepts and the status the switch returned for it. The codes are copied from
// the predicates in pgerrcode, not from the lookup table, so the test checks
// the table against the old behaviour and not against itself.
var handledSQLStates = []struct {
	predicate string
	status    int
	codes     []string
}{
	{
		predicate: "IsWarning",
		status:    http.StatusOK,
		codes: []string{
			pgerrcode.Warning,
			pgerrcode.DynamicResultSetsReturned,
			pgerrcode.ImplicitZeroBitPadding,
			pgerrcode.NullValueEliminatedInSetFunction,
			pgerrcode.PrivilegeNotGranted,
			pgerrcode.PrivilegeNotRevoked,
			pgerrcode.StringDataRightTruncationWarning,
			pgerrcode.DeprecatedFeature,
		},
	},
	{
		predicate: "IsNoData",
		status:    http.StatusOK,
		codes: []string{
			pgerrcode.NoData,
			pgerrcode.NoAdditionalDynamicResultSetsReturned,
		},
	},
	{
		predicate: "IsSQLStatementNotYetComplete",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.SQLStatementNotYetComplete,
		},
	},
	{
		predicate: "IsConnectionException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.ConnectionException,
			pgerrcode.ConnectionDoesNotExist,
			pgerrcode.ConnectionFailure,
			pgerrcode.SQLClientUnableToEstablishSQLConnection,
			pgerrcode.SQLServerRejectedEstablishmentOfSQLConnection,
			pgerrcode.TransactionResolutionUnknown,
			pgerrcode.ProtocolViolation,
		},
	},
	{
		predicate: "IsTriggeredActionException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.TriggeredActionException,
		},
	},
	{
		predicate: "IsFeatureNotSupported",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.FeatureNotSupported,
		},
	},
	{
		predicate: "IsInvalidTransactionInitiation",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidTransactionInitiation,
		},
	},
	{
		predicate: "IsLocatorException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.LocatorException,
			pgerrcode.InvalidLocatorSpecification,
		},
	},
	{
		predicate: "IsInvalidGrantor",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidGrantor,
			pgerrcode.InvalidGrantOperation,
		},
	},
	{
		predicate: "IsInvalidRoleSpecification",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidRoleSpecification,
		},
	},
	{
		predicate: "IsDiagnosticsException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.DiagnosticsException,
			pgerrcode.StackedDiagnosticsAccessedWithoutActiveHandler,
		},
	},
	{
		predicate: "IsCaseNotFound",
		status:    http.StatusNotFound,
		codes: []string{
			pgerrcode.CaseNotFound,
		},
	},
	{
		predicate: "IsCardinalityViolation",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.CardinalityViolation,
		},
	},
	{
		predicate: "IsDataException",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.DataException,
			pgerrcode.ArraySubscriptError,
			pgerrcode.CharacterNotInRepertoire,
			pgerrcode.DatetimeFieldOverflow,
			pgerrcode.DivisionByZero,
			pgerrcode.ErrorInAssignment,
			pgerrcode.EscapeCharacterConflict,
			pgerrcode.IndicatorOverflow,
			pgerrcode.IntervalFieldOverflow,
			pgerrcode.InvalidArgumentForLogarithm,
			pgerrcode.InvalidArgumentForNtileFunction,
			pgerrcode.InvalidArgumentForNthValueFunction,
			pgerrcode.InvalidArgumentForPowerFunction,
			pgerrcode.InvalidArgumentForWidthBucketFunction,
			pgerrcode.InvalidCharacterValueForCast,
			pgerrcode.InvalidDatetimeFormat,
			pgerrcode.InvalidEscapeCharacter,
			pgerrcode.InvalidEscapeOctet,
			pgerrcode.InvalidEscapeSequence,
			pgerrcode.NonstandardUseOfEscapeCharacter,
			pgerrcode.InvalidIndicatorParameterValue,
			pgerrcode.InvalidParameterValue,
			pgerrcode.InvalidPrecedingOrFollowingSize,
			pgerrcode.InvalidRegularExpression,
			pgerrcode.InvalidRowCountInLimitClause,
			pgerrcode.InvalidRowCountInResultOffsetClause,
			pgerrcode.InvalidTablesampleArgument,
			pgerrcode.InvalidTablesampleRepeat,
			pgerrcode.InvalidTimeZoneDisplacementValue,
			pgerrcode.InvalidUseOfEscapeCharacter,
			pgerrcode.MostSpecificTypeMismatch,
			pgerrcode.NullValueNotAllowedDataException,
			pgerrcode.NullValueNoIndicatorParameter,
			pgerrcode.NumericValueOutOfRange,
			pgerrcode.SequenceGeneratorLimitExceeded,
			pgerrcode.StringDataLengthMismatch,
			pgerrcode.StringDataRightTruncationDataException,
			pgerrcode.SubstringError,
			pgerrcode.TrimError,
			pgerrcode.UnterminatedCString,
			pgerrcode.ZeroLengthCharacterString,
			pgerrcode.FloatingPointException,
			pgerrcode.InvalidTextRepresentation,
			pgerrcode.InvalidBinaryRepresentation,
			pgerrcode.BadCopyFileFormat,
			pgerrcode.UntranslatableCharacter,
			pgerrcode.NotAnXMLDocument,
			pgerrcode.InvalidXMLDocument,
			pgerrcode.InvalidXMLContent,
			pgerrcode.InvalidXMLComment,
			pgerrcode.InvalidXMLProcessingInstruction,
			pgerrcode.DuplicateJSONObjectKeyValue,
			pgerrcode.InvalidArgumentForSQLJSONDatetimeFunction,
			pgerrcode.InvalidJSONText,
			pgerrcode.InvalidSQLJSONSubscript,
			pgerrcode.MoreThanOneSQLJSONItem,
			pgerrcode.NoSQLJSONItem,
			pgerrcode.NonNumericSQLJSONItem,
			pgerrcode.NonUniqueKeysInAJSONObject,
			pgerrcode.SingletonSQLJSONItemRequired,
			pgerrcode.SQLJSONArrayNotFound,
			pgerrcode.SQLJSONMemberNotFound,
			pgerrcode.SQLJSONNumberNotFound,
			pgerrcode.SQLJSONObjectNotFound,
			pgerrcode.TooManyJSONArrayElements,
			pgerrcode.TooManyJSONObjectMembers,
			pgerrcode.SQLJSONScalarRequired,
			pgerrcode.SQLJSONItemCannotBeCastToTargetType,
		},
	},
	{
		predicate: "IsIntegrityConstraintViolation",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.IntegrityConstraintViolation,
			pgerrcode.RestrictViolation,
			pgerrcode.NotNullViolation,
			pgerrcode.ForeignKeyViolation,
			pgerrcode.UniqueViolation,
			pgerrcode.CheckViolation,
			pgerrcode.ExclusionViolation,
		},
	},
	{
		predicate: "IsInvalidCursorState",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidCursorState,
		},
	},
	{
		predicate: "IsInvalidTransactionState",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidTransactionState,
			pgerrcode.ActiveSQLTransaction,
			pgerrcode.BranchTransactionAlreadyActive,
			pgerrcode.HeldCursorRequiresSameIsolationLevel,
			pgerrcode.InappropriateAccessModeForBranchTransaction,
			pgerrcode.InappropriateIsolationLevelForBranchTransaction,
			pgerrcode.NoActiveSQLTransactionForBranchTransaction,
			pgerrcode.ReadOnlySQLTransaction,
			pgerrcode.SchemaAndDataStatementMixingNotSupported,
			pgerrcode.NoActiveSQLTransaction,
			pgerrcode.InFailedSQLTransaction,
			pgerrcode.IdleInTransactionSessionTimeout,
		},
	},
	{
		predicate: "IsInvalidSQLStatementName",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.InvalidSQLStatementName,
		},
	},
	{
		predicate: "IsTriggeredDataChangeViolation",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.TriggeredDataChangeViolation,
		},
	},
	{
		predicate: "IsInvalidAuthorizationSpecification",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidAuthorizationSpecification,
			pgerrcode.InvalidPassword,
		},
	},
	{
		predicate: "IsDependentPrivilegeDescriptorsStillExist",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.DependentPrivilegeDescriptorsStillExist,
			pgerrcode.DependentObjectsStillExist,
		},
	},
	{
		predicate: "IsInvalidTransactionTermination",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidTransactionTermination,
		},
	},
	{
		predicate: "IsSQLRoutineException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.SQLRoutineException,
			pgerrcode.FunctionExecutedNoReturnStatement,
			pgerrcode.ModifyingSQLDataNotPermittedSQLRoutineException,
			pgerrcode.ProhibitedSQLStatementAttemptedSQLRoutineException,
			pgerrcode.ReadingSQLDataNotPermittedSQLRoutineException,
		},
	},
	{
		predicate: "IsInvalidCursorName",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidCursorName,
		},
	},
	{
		predicate: "IsExternalRoutineException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.ExternalRoutineException,
			pgerrcode.ContainingSQLNotPermitted,
			pgerrcode.ModifyingSQLDataNotPermittedExternalRoutineException,
			pgerrcode.ProhibitedSQLStatementAttemptedExternalRoutineException,
			pgerrcode.ReadingSQLDataNotPermittedExternalRoutineException,
		},
	},
	{
		predicate: "IsExternalRoutineInvocationException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.ExternalRoutineInvocationException,
			pgerrcode.InvalidSQLstateReturned,
			pgerrcode.NullValueNotAllowedExternalRoutineInvocationException,
			pgerrcode.TriggerProtocolViolated,
			pgerrcode.SRFProtocolViolated,
			pgerrcode.EventTriggerProtocolViolated,
		},
	},
	{
		predicate: "IsSavepointException",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.SavepointException,
			pgerrcode.InvalidSavepointSpecification,
		},
	},
	{
		predicate: "IsInvalidCatalogName",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidCatalogName,
		},
	},
	{
		predicate: "IsInvalidSchemaName",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InvalidSchemaName,
		},
	},
	{
		predicate: "IsTransactionRollback",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.TransactionRollback,
			pgerrcode.TransactionIntegrityConstraintViolation,
			pgerrcode.SerializationFailure,
			pgerrcode.StatementCompletionUnknown,
			pgerrcode.DeadlockDetected,
		},
	},
	{
		predicate: "IsSyntaxErrororAccessRuleViolation",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.SyntaxErrorOrAccessRuleViolation,
			pgerrcode.SyntaxError,
			pgerrcode.InsufficientPrivilege,
			pgerrcode.CannotCoerce,
			pgerrcode.GroupingError,
			pgerrcode.WindowingError,
			pgerrcode.InvalidRecursion,
			pgerrcode.InvalidForeignKey,
			pgerrcode.InvalidName,
			pgerrcode.NameTooLong,
			pgerrcode.ReservedName,
			pgerrcode.DatatypeMismatch,
			pgerrcode.IndeterminateDatatype,
			pgerrcode.CollationMismatch,
			pgerrcode.IndeterminateCollation,
			pgerrcode.WrongObjectType,
			pgerrcode.GeneratedAlways,
			pgerrcode.UndefinedColumn,
			pgerrcode.UndefinedFunction,
			pgerrcode.UndefinedTable,
			pgerrcode.UndefinedParameter,
			pgerrcode.UndefinedObject,
			pgerrcode.DuplicateColumn,
			pgerrcode.DuplicateCursor,
			pgerrcode.DuplicateDatabase,
			pgerrcode.DuplicateFunction,
			pgerrcode.DuplicatePreparedStatement,
			pgerrcode.DuplicateSchema,
			pgerrcode.DuplicateTable,
			pgerrcode.DuplicateAlias,
			pgerrcode.DuplicateObject,
			pgerrcode.AmbiguousColumn,
			pgerrcode.AmbiguousFunction,
			pgerrcode.AmbiguousParameter,
			pgerrcode.AmbiguousAlias,
			pgerrcode.InvalidColumnReference,
			pgerrcode.InvalidColumnDefinition,
			pgerrcode.InvalidCursorDefinition,
			pgerrcode.InvalidDatabaseDefinition,
			pgerrcode.InvalidFunctionDefinition,
			pgerrcode.InvalidPreparedStatementDefinition,
			pgerrcode.InvalidSchemaDefinition,
			pgerrcode.InvalidTableDefinition,
			pgerrcode.InvalidObjectDefinition,
		},
	},
	{
		predicate: "IsWithCheckOptionViolation",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.WithCheckOptionViolation,
		},
	},
	{
		predicate: "IsInsufficientResources",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InsufficientResources,
			pgerrcode.DiskFull,
			pgerrcode.OutOfMemory,
			pgerrcode.TooManyConnections,
			pgerrcode.ConfigurationLimitExceeded,
		},
	},
	{
		predicate: "IsProgramLimitExceeded",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.ProgramLimitExceeded,
			pgerrcode.StatementTooComplex,
			pgerrcode.TooManyColumns,
			pgerrcode.TooManyArguments,
		},
	},
	{
		predicate: "IsObjectNotInPrerequisiteState",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.ObjectNotInPrerequisiteState,
			pgerrcode.ObjectInUse,
			pgerrcode.CantChangeRuntimeParam,
			pgerrcode.LockNotAvailable,
			pgerrcode.UnsafeNewEnumValueUsage,
		},
	},
	{
		predicate: "IsOperatorIntervention",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.OperatorIntervention,
			pgerrcode.QueryCanceled,
			pgerrcode.AdminShutdown,
			pgerrcode.CrashShutdown,
			pgerrcode.CannotConnectNow,
			pgerrcode.DatabaseDropped,
			pgerrcode.IdleSessionTimeout,
		},
	},
	{
		predicate: "IsSystemError",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.SystemError,
			pgerrcode.IOError,
			pgerrcode.UndefinedFile,
			pgerrcode.DuplicateFile,
		},
	},
	{
		predicate: "IsSnapshotFailure",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.SnapshotTooOld,
		},
	},
	{
		predicate: "IsConfigurationFileError",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.ConfigFileError,
			pgerrcode.LockFileExists,
		},
	},
	{
		predicate: "IsForeignDataWrapperError",
		status:    http.StatusBadRequest,
		codes: []string{
			pgerrcode.FDWError,
			pgerrcode.FDWColumnNameNotFound,
			pgerrcode.FDWDynamicParameterValueNeeded,
			pgerrcode.FDWFunctionSequenceError,
			pgerrcode.FDWInconsistentDescriptorInformation,
			pgerrcode.FDWInvalidAttributeValue,
			pgerrcode.FDWInvalidColumnName,
			pgerrcode.FDWInvalidColumnNumber,
			pgerrcode.FDWInvalidDataType,
			pgerrcode.FDWInvalidDataTypeDescriptors,
			pgerrcode.FDWInvalidDescriptorFieldIdentifier,
			pgerrcode.FDWInvalidHandle,
			pgerrcode.FDWInvalidOptionIndex,
			pgerrcode.FDWInvalidOptionName,
			pgerrcode.FDWInvalidStringLengthOrBufferLength,
			pgerrcode.FDWInvalidStringFormat,
			pgerrcode.FDWInvalidUseOfNullPointer,
			pgerrcode.FDWTooManyHandles,
			pgerrcode.FDWOutOfMemory,
			pgerrcode.FDWNoSchemas,
			pgerrcode.FDWOptionNameNotFound,
			pgerrcode.FDWReplyHandle,
			pgerrcode.FDWSchemaNotFound,
			pgerrcode.FDWTableNotFound,
			pgerrcode.FDWUnableToCreateExecution,
			pgerrcode.FDWUnableToCreateReply,
			pgerrcode.FDWUnableToEstablishConnection,
		},
	},
	{
		predicate: "IsPLpgSQLError",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.PLpgSQLError,
			pgerrcode.RaiseException,
			pgerrcode.NoDataFound,
			pgerrcode.TooManyRows,
			pgerrcode.AssertFailure,
		},
	},
	{
		predicate: "IsInternalError",
		status:    http.StatusBadGateway,
		codes: []string{
			pgerrcode.InternalError,
			pgerrcode.DataCorrupted,
			pgerrcode.IndexCorrupted,
		},
	},
}

func TestGetHTTPErrorCodeHandledSQLStates(t *testing.T) {
	for _, group := range handledSQLStates {
		for _, code := range group.codes {
			pgErr := &pgconn.PgError{Code: code}
			if got := GetHTTPErrorCode(pgErr); got != group.status {
				t.Errorf("%s: GetHTTPErrorCode(%s) = %d, want %d", group.predicate, code, got, group.status)
			}
			if got := GetHTTPErrorCode(fmt.Errorf("query: %w", pgErr)); got != group.status {
				t.Errorf("%s: wrapped GetHTTPErrorCode(%s) = %d, want %d", group.predicate, code, got, group.status)
			}
		}
	}
}

func TestGetHTTPErrorCodeUnhandled(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusOK},
		{"not a pg error", errors.New("boom"), http.StatusInternalServerError},
		{"empty code", &pgconn.PgError{Code: ""}, http.StatusInternalServerError},
		{"class prefix alone is not a code", &pgconn.PgError{Code: pgerrcode.DataException[:2]}, http.StatusInternalServerError},
		{"unknown code", &pgconn.PgError{Code: "99999"}, http.StatusInternalServerError},
		{"codes match exactly, not case-insensitively", &pgconn.PgError{Code: "2b000"}, http.StatusInternalServerError},
		{"code with a suffix does not match", &pgconn.PgError{Code: pgerrcode.DivisionByZero + "x"}, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetHTTPErrorCode(tt.err); got != tt.want {
				t.Errorf("GetHTTPErrorCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
