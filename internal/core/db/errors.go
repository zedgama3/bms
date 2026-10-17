package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/puddle/v2"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// ErrReadOnly is wrapped by every error for a write refused because this copy is
// read-only. Its kind is errs.Unavailable; callers that must tell the two apart check
// errors.Is(err, db.ErrReadOnly) before the kind (the api does, to answer "this copy is
// read-only"; a retry loop around a write must, because retry.Retryable would retry it).
var ErrReadOnly = errors.New("this copy of the database is read-only")

// ErrNoDatabase is wrapped by every error from the DB that None returns. Its kind is
// errs.Unavailable. A module factory that reads the database at construction fails the
// build step of `bms openapi` and `bms check` with it (app.md decision 7).
var ErrNoDatabase = errors.New("no database behind this DB")

// Error is a PostgreSQL server error as db reports it: the SQLSTATE and the names the
// server attached, never the message's detail, hint or context, which can carry row and
// parameter values (decision 4). It is the leaf of the chain: the driver's error is not
// kept. Find it with errors.As to act on a constraint name.
type Error struct {
	Code       string // SQLSTATE, e.g. "23505"
	Schema     string // schema name, if the server gave one
	Table      string // table name, if given
	Column     string // column name, if given
	Constraint string // constraint name, if given
}

// Error returns "pg <Code> <condition name> [on <Schema>.<Table>[.<Column>]] [constraint
// <Constraint>]", e.g. "pg 23505 unique_violation on volunteers.shift constraint
// shift_slot_key". Cannot fail; never contains a value from the statement or a row.
func (e *Error) Error() string {
	if e == nil {
		return "pg <nil>"
	}
	var b strings.Builder
	b.WriteString("pg ")
	b.WriteString(e.Code)
	if name := conditionName(e.Code); name != "" {
		b.WriteByte(' ')
		b.WriteString(name)
	}
	var where []string
	for _, p := range []string{e.Schema, e.Table, e.Column} {
		if p != "" {
			where = append(where, p)
		}
	}
	if len(where) > 0 {
		b.WriteString(" on ")
		b.WriteString(strings.Join(where, "."))
	}
	if e.Constraint != "" {
		b.WriteString(" constraint ")
		b.WriteString(e.Constraint)
	}
	return b.String()
}

// Wrap classifies err and wraps it with op: errs.E(op, kind, scrubbed), where the kind
// follows the table in the db.md "Error mapping" and a server error is replaced by an
// *Error. It returns nil for a nil err. An err that already carries an *errs.Error keeps
// its kind (the storage package only adds its op). Every error that leaves a store method
// goes through Wrap, including those from pgx helpers that never passed through Querier
// (pgx.CollectOneRow returning pgx.ErrNoRows). Cannot fail.
func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	var ee *errs.Error
	if errors.As(err, &ee) {
		return errs.E(op, errs.KindOf(err), err)
	}
	kind, cause := classify(err)
	return errs.E(op, kind, cause)
}

// classify returns the kind for a driver error and the error to keep in the chain: a
// server error is replaced by an *Error; anything that could echo a statement argument is
// replaced by a fixed text.
func classify(err error) (errs.Kind, error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return errs.NotFound, err
	case errors.Is(err, pgx.ErrTooManyRows), errors.Is(err, pgx.ErrTxClosed),
		errors.Is(err, pgx.ErrTxCommitRollback):
		return errs.Internal, err
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return classifyPg(pg)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return errs.Canceled, err
	case errors.Is(err, context.DeadlineExceeded):
		return errs.Timeout, err
	}
	var ce *pgconn.ConnectError
	var ne net.Error
	switch {
	case errors.As(err, &ce), errors.As(err, &ne),
		errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errs.Unavailable, err
	case errors.Is(err, puddle.ErrClosedPool):
		return errs.Unavailable, errors.New("the database pool is closed")
	case pgconn.SafeToRetry(err):
		// Driver text can carry statement fragments; keep the type only.
		return errs.Unavailable, fmt.Errorf("connection error (%T)", err)
	}
	// pgx prints the offending value in an encode error; the value may be personal data.
	if strings.Contains(err.Error(), "failed to encode args[") {
		return errs.Invalid, errors.New("a statement argument could not be encoded")
	}
	// pgx's scan error text can quote the value it failed to convert: keep the column only.
	var sae pgx.ScanArgError
	if errors.As(err, &sae) {
		return errs.Internal, fmt.Errorf("cannot scan column %d (%s) into its destination", sae.ColumnIndex, sae.FieldName)
	}
	// An error db does not know could echo a value: keep its Go type, not its text.
	return errs.Internal, fmt.Errorf("unclassified error (%T)", err)
}

func classifyPg(pg *pgconn.PgError) (errs.Kind, error) {
	leaf := &Error{
		Code:       pg.Code,
		Schema:     pg.SchemaName,
		Table:      pg.TableName,
		Column:     pg.ColumnName,
		Constraint: pg.ConstraintName,
	}
	code := pg.Code
	switch {
	case code == "23505":
		return errs.Conflict, leaf
	case code == "23503", code == "23502", code == "23514", strings.HasPrefix(code, "22"):
		return errs.Invalid, leaf
	case code == "25006":
		return errs.Unavailable, fmt.Errorf("%w (%w)", ErrReadOnly, leaf)
	case code == "40001", code == "40P01", code == "55P03":
		return errs.Unavailable, leaf
	case strings.HasPrefix(code, "08"), strings.HasPrefix(code, "53"),
		code == "57P01", code == "57P02", code == "57P03":
		return errs.Unavailable, leaf
	case code == "57014":
		return errs.Timeout, leaf
	case strings.HasPrefix(code, "28"), code == "3D000":
		return errs.Invalid, leaf
	default:
		return errs.Internal, leaf
	}
}

// scrub returns err with a server error replaced by its *Error, for places that join an
// error into another without classifying it.
func scrub(err error) error {
	if err == nil {
		return nil
	}
	_, cause := classify(err)
	return cause
}

// conditionName returns the PostgreSQL condition name of a SQLSTATE, or "" when this
// table does not know it. The table lists the codes a store meets; it is names only.
func conditionName(code string) string { return conditionNames[code] }

var conditionNames = map[string]string{
	"00000": "successful_completion",
	"01000": "warning",
	"02000": "no_data",
	"08000": "connection_exception",
	"08001": "sqlclient_unable_to_establish_sqlconnection",
	"08003": "connection_does_not_exist",
	"08004": "sqlserver_rejected_establishment_of_sqlconnection",
	"08006": "connection_failure",
	"08P01": "protocol_violation",
	"0A000": "feature_not_supported",
	"21000": "cardinality_violation",
	"22000": "data_exception",
	"22001": "string_data_right_truncation",
	"22003": "numeric_value_out_of_range",
	"22004": "null_value_not_allowed",
	"22007": "invalid_datetime_format",
	"22008": "datetime_field_overflow",
	"2200B": "escape_character_conflict",
	"22012": "division_by_zero",
	"22021": "character_not_in_repertoire",
	"22023": "invalid_parameter_value",
	"22P02": "invalid_text_representation",
	"22P03": "invalid_binary_representation",
	"23000": "integrity_constraint_violation",
	"23001": "restrict_violation",
	"23502": "not_null_violation",
	"23503": "foreign_key_violation",
	"23505": "unique_violation",
	"23514": "check_violation",
	"23P01": "exclusion_violation",
	"25000": "invalid_transaction_state",
	"25001": "active_sql_transaction",
	"25006": "read_only_sql_transaction",
	"25P01": "no_active_sql_transaction",
	"25P02": "in_failed_sql_transaction",
	"26000": "invalid_sql_statement_name",
	"28000": "invalid_authorization_specification",
	"28P01": "invalid_password",
	"2BP01": "dependent_objects_still_exist",
	"3D000": "invalid_catalog_name",
	"3F000": "invalid_schema_name",
	"40000": "transaction_rollback",
	"40001": "serialization_failure",
	"40002": "transaction_integrity_constraint_violation",
	"40003": "statement_completion_unknown",
	"40P01": "deadlock_detected",
	"42000": "syntax_error_or_access_rule_violation",
	"42501": "insufficient_privilege",
	"42601": "syntax_error",
	"42602": "invalid_name",
	"42703": "undefined_column",
	"42883": "undefined_function",
	"42P01": "undefined_table",
	"42P02": "undefined_parameter",
	"42P06": "duplicate_schema",
	"42P07": "duplicate_table",
	"42701": "duplicate_column",
	"42710": "duplicate_object",
	"42804": "datatype_mismatch",
	"42809": "wrong_object_type",
	"42846": "cannot_coerce",
	"42P10": "invalid_column_reference",
	"42P18": "indeterminate_datatype",
	"53000": "insufficient_resources",
	"53100": "disk_full",
	"53200": "out_of_memory",
	"53300": "too_many_connections",
	"54000": "program_limit_exceeded",
	"55000": "object_not_in_prerequisite_state",
	"55006": "object_in_use",
	"55P03": "lock_not_available",
	"57000": "operator_intervention",
	"57014": "query_canceled",
	"57P01": "admin_shutdown",
	"57P02": "crash_shutdown",
	"57P03": "cannot_connect_now",
	"58000": "system_error",
	"58030": "io_error",
	"XX000": "internal_error",
	"XX001": "data_corrupted",
	"XX002": "index_corrupted",
}
