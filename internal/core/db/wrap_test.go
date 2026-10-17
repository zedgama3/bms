package db_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// pgErr builds a synthetic server error whose free-text fields all carry the marker.
func pgErr(code string) *pgconn.PgError {
	return &pgconn.PgError{
		Severity:       "ERROR",
		Code:           code,
		Message:        "message " + marker,
		Detail:         "Key (email)=(" + marker + ") already exists.",
		Hint:           "hint " + marker,
		Where:          "PL/pgSQL function f(text) line 3: " + marker,
		InternalQuery:  "SELECT " + marker,
		SchemaName:     "volunteers",
		TableName:      "shift",
		ColumnName:     "slot",
		ConstraintName: "shift_slot_key",
	}
}

type retryableError struct{}

func (retryableError) Error() string     { return "retryable" }
func (retryableError) SafeToRetry() bool { return true }

func TestWrap_MappingTable(t *testing.T) {
	sentinel := errors.New("sentinel")
	tests := []struct {
		name     string
		in       error
		kind     errs.Kind
		readOnly bool
		pg       bool // result must carry a *db.Error with the same code
	}{
		{"ErrNoRows", pgx.ErrNoRows, errs.NotFound, false, false},
		{"ErrNoRows wrapped", fmt.Errorf("scan: %w", pgx.ErrNoRows), errs.NotFound, false, false},
		{"ErrTooManyRows", pgx.ErrTooManyRows, errs.Internal, false, false},
		{"ErrTxClosed", pgx.ErrTxClosed, errs.Internal, false, false},
		{"ErrTxCommitRollback", pgx.ErrTxCommitRollback, errs.Internal, false, false},
		{"context canceled", context.Canceled, errs.Canceled, false, false},
		{"context canceled wrapped", fmt.Errorf("x: %w", context.Canceled), errs.Canceled, false, false},
		{"deadline exceeded", context.DeadlineExceeded, errs.Timeout, false, false},
		{"23505 unique", pgErr("23505"), errs.Conflict, false, true},
		{"23503 foreign key", pgErr("23503"), errs.Invalid, false, true},
		{"23502 not null", pgErr("23502"), errs.Invalid, false, true},
		{"23514 check", pgErr("23514"), errs.Invalid, false, true},
		{"22P02 invalid text", pgErr("22P02"), errs.Invalid, false, true},
		{"22001 too long", pgErr("22001"), errs.Invalid, false, true},
		{"25006 read only", pgErr("25006"), errs.Unavailable, true, true},
		{"40001 serialization", pgErr("40001"), errs.Unavailable, false, true},
		{"40P01 deadlock", pgErr("40P01"), errs.Unavailable, false, true},
		{"55P03 lock not available", pgErr("55P03"), errs.Unavailable, false, true},
		{"08006 connection failure", pgErr("08006"), errs.Unavailable, false, true},
		{"08003 connection does not exist", pgErr("08003"), errs.Unavailable, false, true},
		{"53300 too many connections", pgErr("53300"), errs.Unavailable, false, true},
		{"57P01 admin shutdown", pgErr("57P01"), errs.Unavailable, false, true},
		{"57P02 crash shutdown", pgErr("57P02"), errs.Unavailable, false, true},
		{"57P03 cannot connect now", pgErr("57P03"), errs.Unavailable, false, true},
		{"57014 query canceled", pgErr("57014"), errs.Timeout, false, true},
		{"28P01 bad password", pgErr("28P01"), errs.Invalid, false, true},
		{"28000 bad authorization", pgErr("28000"), errs.Invalid, false, true},
		{"3D000 missing database", pgErr("3D000"), errs.Invalid, false, true},
		{"42601 syntax", pgErr("42601"), errs.Internal, false, true},
		{"42P01 undefined table", pgErr("42P01"), errs.Internal, false, true},
		{"42501 privilege", pgErr("42501"), errs.Internal, false, true},
		{"other server error", pgErr("P0001"), errs.Internal, false, true},
		{"server error wrapped", fmt.Errorf("exec: %w", pgErr("23505")), errs.Conflict, false, true},
		{"net op error", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, errs.Unavailable, false, false},
		{"io.EOF", io.EOF, errs.Unavailable, false, false},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, errs.Unavailable, false, false},
		{"SafeToRetry", retryableError{}, errs.Unavailable, false, false},
		{"unknown", errors.New("boom"), errs.Internal, false, false},
		{"sentinel", sentinel, errs.Internal, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := db.Wrap("test.op", tc.in)
			wantKind(t, got, tc.kind)
			noMarker(t, got)
			if gotRO := errors.Is(got, db.ErrReadOnly); gotRO != tc.readOnly {
				t.Errorf("errors.Is(ErrReadOnly) = %v, want %v", gotRO, tc.readOnly)
			}
			var pe *db.Error
			if tc.pg {
				if !errors.As(got, &pe) {
					t.Fatalf("result carries no *db.Error")
				}
				var drv *pgconn.PgError
				if errors.As(got, &drv) {
					t.Errorf("the driver's *pgconn.PgError must not stay in the chain")
				}
			}
			var e *errs.Error
			if !errors.As(got, &e) || e.Op != "test.op" {
				t.Errorf("outermost errs.Error must carry op test.op, got %+v", e)
			}
		})
	}
}

func TestWrap_NilIsNil(t *testing.T) {
	if err := db.Wrap("test.op", nil); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
}

func TestWrap_KeepsExistingKindAndPrependsOp(t *testing.T) {
	cause := errors.New("denied")
	in := errs.E("inner.op", errs.Permission, cause)
	got := db.Wrap("outer.op", in)
	wantKind(t, got, errs.Permission)
	if !errors.Is(got, cause) {
		t.Errorf("cause lost")
	}
	var e *errs.Error
	if !errors.As(got, &e) || e.Op != "outer.op" {
		t.Errorf("want outer op first, got %+v", e)
	}
}

func TestWrap_Idempotent(t *testing.T) {
	once := db.Wrap("a.op", pgErr("23505"))
	twice := db.Wrap("b.op", once)
	wantKind(t, twice, errs.Conflict)
	var pe *db.Error
	if !errors.As(twice, &pe) || pe.Code != "23505" {
		t.Fatalf("*db.Error lost after a second Wrap: %+v", pe)
	}
	noMarker(t, twice)
	ro := db.Wrap("b.op", db.Wrap("a.op", pgErr("25006")))
	wantKind(t, ro, errs.Unavailable)
	if !errors.Is(ro, db.ErrReadOnly) {
		t.Errorf("ErrReadOnly lost after a second Wrap")
	}
}

// F12
func TestWrap_UniqueViolation_ReturnsConflictWithConstraint(t *testing.T) {
	got := db.Wrap("volunteers.postgres.CreateShift", pgErr("23505"))
	wantKind(t, got, errs.Conflict)
	var pe *db.Error
	if !errors.As(got, &pe) {
		t.Fatalf("no *db.Error")
	}
	want := db.Error{Code: "23505", Schema: "volunteers", Table: "shift", Column: "slot", Constraint: "shift_slot_key"}
	if *pe != want {
		t.Fatalf("got %+v, want %+v", *pe, want)
	}
	noMarker(t, got)
}

// F13
func TestWrap_ForeignKeyViolation_ReturnsInvalid(t *testing.T) {
	wantKind(t, db.Wrap("op", pgErr("23503")), errs.Invalid)
}

// F14
func TestWrap_DataAndCheckViolations_ReturnInvalid(t *testing.T) {
	for _, c := range []string{"23502", "23514", "22000", "22003", "22007", "22P02", "22012"} {
		wantKind(t, db.Wrap("op", pgErr(c)), errs.Invalid)
	}
}

// F15
func TestWrap_SerializationAndDeadlock_ReturnUnavailable(t *testing.T) {
	for _, c := range []string{"40001", "40P01", "55P03"} {
		got := db.Wrap("op", pgErr(c))
		wantKind(t, got, errs.Unavailable)
		if errors.Is(got, db.ErrReadOnly) {
			t.Errorf("%s must not be read-only", c)
		}
	}
}

// F16
func TestWrap_ConnectionErrors_ReturnUnavailable(t *testing.T) {
	for _, in := range []error{
		io.EOF, io.ErrUnexpectedEOF, retryableError{},
		&net.OpError{Op: "read", Err: errors.New("connection reset by peer")},
		pgErr("08006"), pgErr("57P01"),
	} {
		wantKind(t, db.Wrap("op", in), errs.Unavailable)
	}
}

// F19
func TestWrap_SyntaxAndUndefinedTable_ReturnInternal(t *testing.T) {
	for _, c := range []string{"42601", "42P01", "42703", "42883"} {
		got := db.Wrap("op", pgErr(c))
		wantKind(t, got, errs.Internal)
		var pe *db.Error
		if !errors.As(got, &pe) || pe.Code != c {
			t.Errorf("%s: *db.Error must carry the code, got %+v", c, pe)
		}
	}
}

// F31
func TestWrap_UnknownError_ReturnsInternal(t *testing.T) {
	wantKind(t, db.Wrap("op", errors.New("who knows")), errs.Internal)
}

func TestWrap_ErrReadOnlyIsErrorsIsThroughWrapping(t *testing.T) {
	got := fmt.Errorf("store: %w", db.Wrap("op", pgErr("25006")))
	if !errors.Is(got, db.ErrReadOnly) {
		t.Fatalf("errors.Is(ErrReadOnly) must see through fmt wrapping")
	}
	wantKind(t, got, errs.Unavailable)
	// A plain unavailable error is not read-only.
	if errors.Is(db.Wrap("op", io.EOF), db.ErrReadOnly) {
		t.Fatalf("an EOF is not read-only")
	}
}

func TestSentinels_AreDistinctNonNil(t *testing.T) {
	if db.ErrReadOnly == nil || db.ErrNoDatabase == nil || errors.Is(db.ErrReadOnly, db.ErrNoDatabase) {
		t.Fatalf("sentinels must be distinct and non-nil")
	}
}

func TestError_String_NamesOnly(t *testing.T) {
	tests := []struct {
		name string
		e    db.Error
		want string // exact, where the spec fixes the format
		has  []string
	}{
		{"spec example", db.Error{Code: "23505", Schema: "volunteers", Table: "shift", Constraint: "shift_slot_key"},
			"pg 23505 unique_violation on volunteers.shift constraint shift_slot_key", nil},
		{"with column", db.Error{Code: "23502", Schema: "volunteers", Table: "shift", Column: "slot"},
			"", []string{"pg 23502", "volunteers.shift.slot"}},
		{"code only", db.Error{Code: "40001"}, "", []string{"pg 40001"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.e.Error()
			if tc.want != "" && got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for _, h := range tc.has {
				if !strings.Contains(got, h) {
					t.Errorf("%q lacks %q", got, h)
				}
			}
		})
	}
	// Built from a server error carrying marker values everywhere, the text is names only.
	var pe *db.Error
	if !errors.As(db.Wrap("op", pgErr("23505")), &pe) {
		t.Fatalf("no *db.Error")
	}
	if strings.Contains(pe.Error(), marker) {
		t.Fatalf("Error() leaks a value")
	}
}

func TestMode_String(t *testing.T) {
	tests := []struct {
		m    db.Mode
		want string
	}{
		{db.Read, "read"}, {db.Write, "write"}, {db.Mode(0), "Mode(0)"}, {db.Mode(7), "Mode(7)"},
	}
	for _, tc := range tests {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("Mode(%d).String() = %q, want %q", uint8(tc.m), got, tc.want)
		}
	}
	if db.Read == 0 || db.Write == 0 || db.Read == db.Write {
		t.Errorf("the zero Mode must be invalid and the two modes distinct")
	}
}
