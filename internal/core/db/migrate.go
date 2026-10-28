package db

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

const (
	coreSchema    = "core"
	bootstrapName = "db"
)

var (
	setNameRE  = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)
	fileNameRE = regexp.MustCompile(`^([0-9]{4})_([a-z0-9_]+)\.sql$`)
)

// migrateLockKey is the advisory lock key of one Migrate run, derived from "bms.migrate".
var migrateLockKey = func() int64 {
	sum := sha256.Sum256([]byte("bms.migrate"))
	return int64(binary.BigEndian.Uint64(sum[:8])) //nolint:gosec // any 64 bits make a valid key
}()

// Set is one sequence of migrations: the core package's or the module's
// postgres/migrations directory. Module sets are built by app from Manifest.Name and
// Manifest.Migrations; core sets are exported by each core package with tables.
type Set struct {
	// Name identifies the sequence in core.schema_migration: a module name, or a core
	// package's name (people, identity, audit, ...). "[a-z][a-z0-9]{1,31}".
	Name string
	// Schema is the PostgreSQL schema the set owns, created with CREATE SCHEMA IF NOT
	// EXISTS before its first file runs: "core" for a core set, Name for a module (D10).
	// Nothing else is accepted.
	Schema string
	// Files holds NNNN_name.sql at its root (db.md "Migration files").
	Files fs.FS
}

// Validate fails with errs.Invalid joining a validate.FieldError per problem: Name or
// Schema fails its grammar, Schema is neither "core" nor Name, Files is nil (F32). It does
// not read Files; Version does.
func (s Set) Validate() error {
	v := validate.Errors{Op: "db.Set.Validate"}
	if !setNameRE.MatchString(s.Name) {
		v.Addf("name", validate.CodeMalformed, "must match %s", setNameRE.String())
	}
	switch {
	case !setNameRE.MatchString(s.Schema):
		v.Addf("schema", validate.CodeMalformed, "must match %s", setNameRE.String())
	case s.Schema != coreSchema && s.Schema != s.Name:
		v.Addf("schema", validate.CodeOneOf, "must be %q or the set's name", coreSchema)
	}
	if s.Files == nil {
		v.Addf("files", validate.CodeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // validate.Errors.Err already wraps with errs.E
}

// migFile is one parsed migration file.
type migFile struct {
	number   int
	name     string // without number and suffix
	sql      []byte
	checksum string // lower-case hex SHA-256 of the bytes
}

func (f migFile) label() string { return fmt.Sprintf("%04d_%s", f.number, f.name) }

// Version parses files and returns the set's version: the highest file number. It fails
// with errs.Invalid when a name is not NNNN_name.sql, numbers repeat, have a gap or do
// not start at 0001, a file cannot be read, or is empty (F33-F35). Consumers: Migrator,
// `bms check`, tests.
func Version(files fs.FS) (int, error) {
	parsed, err := parseFiles("db.Version", files)
	if err != nil {
		return 0, err
	}
	return parsed[len(parsed)-1].number, nil
}

// parseFiles reads and checks a set's files. On success it returns at least one file,
// numbered 1..n in order.
func parseFiles(op string, files fs.FS) ([]migFile, error) {
	if files == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("no migration files given"))
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, errs.E(op, errs.Invalid, fmt.Errorf("cannot list the migration files: %w", err))
	}
	var problems []error
	var out []migFile
	for _, e := range entries { // fs.ReadDir sorts by file name
		m := fileNameRE.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			problems = append(problems, fmt.Errorf("%q is not a migration file named NNNN_name.sql", e.Name()))
			continue
		}
		n, _ := strconv.Atoi(m[1]) //nolint:errcheck // four digits, matched by the grammar
		data, rerr := fs.ReadFile(files, e.Name())
		switch {
		case rerr != nil:
			problems = append(problems, fmt.Errorf("%s cannot be read: %w", e.Name(), rerr))
			continue
		case len(strings.TrimSpace(string(data))) == 0:
			problems = append(problems, fmt.Errorf("%s is empty", e.Name()))
			continue
		}
		sum := sha256.Sum256(data)
		out = append(out, migFile{number: n, name: m[2], sql: data, checksum: hex.EncodeToString(sum[:])})
	}
	if len(problems) == 0 && len(out) == 0 {
		problems = append(problems, errors.New("the set has no migration files"))
	}
	for i, f := range out {
		switch {
		case f.number == i+1:
		case i > 0 && f.number == out[i-1].number:
			problems = append(problems, fmt.Errorf("number %04d is used by more than one file", f.number))
		default:
			problems = append(problems, fmt.Errorf("%s: numbers must start at 0001 and have no gaps (expected %04d)", f.label(), i+1))
		}
	}
	if len(problems) > 0 {
		return nil, errs.E(op, errs.Invalid, errors.Join(problems...))
	}
	return out, nil
}

// Applied is one migration file Migrate ran.
type Applied struct {
	Set      string
	Version  int
	Name     string
	Duration time.Duration
}

// Status is one set's state.
type Status struct {
	Set     string
	Built   int   // Version(files)
	Applied int   // highest version recorded; 0 when none
	Err     error // nil when the set is current; else what CheckSchema returns
}

// Migrator applies and checks migration sets on one Pool. Safe for concurrent use; Migrate
// serialises itself across processes with an advisory lock.
type Migrator struct {
	pool   *Pool
	logger *slog.Logger
	boot   Set
}

// NewMigrator fails with errs.Invalid if pool or logger is nil (F51).
func NewMigrator(pool *Pool, logger *slog.Logger) (*Migrator, error) {
	if pool == nil || logger == nil {
		return nil, errs.E("db.NewMigrator", errs.Invalid, errors.New("nil pool or logger"))
	}
	return &Migrator{
		pool:   pool,
		logger: logger,
		boot:   Set{Name: bootstrapName, Schema: coreSchema, Files: bootstrapFiles},
	}, nil
}

// preparedSet is a validated set with its parsed files.
type preparedSet struct {
	Set
	files []migFile
}

// prepare validates the bootstrap set and sets, and parses their files; every problem is
// reported together (F32-F35, F41).
func (m *Migrator) prepare(op string, sets []Set) ([]preparedSet, error) {
	all := make([]Set, 0, len(sets)+1)
	all = append(all, m.boot)
	all = append(all, sets...)
	seen := map[string]bool{}
	var problems []error
	out := make([]preparedSet, 0, len(all))
	for _, s := range all {
		if err := s.Validate(); err != nil {
			problems = append(problems, fmt.Errorf("set %q: %w", s.Name, err))
			continue
		}
		if seen[s.Name] {
			problems = append(problems, fmt.Errorf("set %q is given more than once (or is the reserved bootstrap set)", s.Name))
			continue
		}
		seen[s.Name] = true
		files, err := parseFiles(op, s.Files)
		if err != nil {
			problems = append(problems, fmt.Errorf("set %q: %w", s.Name, err))
			continue
		}
		out = append(out, preparedSet{Set: s, files: files})
	}
	if len(problems) > 0 {
		return nil, errs.E(op, errs.Invalid, errors.Join(problems...))
	}
	return out, nil
}

// appliedRow is one core.schema_migration row.
type appliedRow struct {
	set      string
	version  int
	name     string
	checksum string
}

// rowSource is what loadApplied reads through: a *Pool or a *pgxpool.Conn.
type rowSource interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// loadApplied reads the recorded files: every set's when set is "", else one set's. A
// missing core.schema_migration means nothing is applied (decision 18).
func loadApplied(ctx context.Context, op string, q rowSource, set string) ([]appliedRow, error) {
	var present bool
	if err := q.QueryRow(ctx, sqlTableExists).Scan(&present); err != nil {
		return nil, Wrap(op, err)
	}
	if !present {
		return nil, nil
	}
	var rows pgx.Rows
	var err error
	if set == "" {
		rows, err = q.Query(ctx, sqlAppliedAll)
	} else {
		rows, err = q.Query(ctx, sqlAppliedForSet, set)
	}
	if err != nil {
		return nil, Wrap(op, err)
	}
	defer rows.Close()
	var out []appliedRow
	for rows.Next() {
		r := appliedRow{set: set}
		var err error
		if set == "" {
			err = rows.Scan(&r.set, &r.version, &r.name, &r.checksum)
		} else {
			err = rows.Scan(&r.version, &r.name, &r.checksum)
		}
		if err != nil {
			return nil, Wrap(op, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, Wrap(op, err)
	}
	return out, nil
}

// highest returns the highest recorded version of set, 0 when none.
func highest(rows []appliedRow, set string) int {
	h := 0
	for _, r := range rows {
		if r.set == set && r.version > h {
			h = r.version
		}
	}
	return h
}

// diagnose compares the recorded files of ps with its embedded files. A changed file
// (F38) or a recorded version above the binary's (F39) is always a problem; with
// wantCurrent, so is a recorded version below it or none at all (F45, F46).
func diagnose(ps preparedSet, rows []appliedRow, wantCurrent bool) []error {
	built := ps.files[len(ps.files)-1].number
	have := highest(rows, ps.Name)
	var problems []error
	if have > built {
		problems = append(problems, fmt.Errorf(
			"module %s: database schema is at version %04d but this binary is built for %04d: upgrade the binary",
			ps.Name, have, built))
	}
	for _, r := range rows {
		if r.set != ps.Name || r.version > built {
			continue
		}
		f := ps.files[r.version-1]
		if r.name != f.name {
			problems = append(problems, fmt.Errorf(
				"module %s: applied file %04d was %q but the binary has %q: a released migration must never change",
				ps.Name, r.version, r.name, f.name))
		} else if r.checksum != f.checksum {
			problems = append(problems, fmt.Errorf(
				"module %s: applied file %s was changed after it ran (checksum differs): restore it and write a new numbered file",
				ps.Name, f.label()))
		}
	}
	if wantCurrent {
		switch {
		case have == 0:
			problems = append(problems, fmt.Errorf(
				"module %s: schema version %04d expected, none applied: run bms migrate", ps.Name, built))
		case have < built:
			problems = append(problems, fmt.Errorf(
				"module %s: schema version %04d expected, %04d applied: run bms migrate", ps.Name, built, have))
		}
	}
	return problems
}

func conflict(op string, problems []error) error {
	return errs.E(op, errs.Conflict, errors.Join(problems...))
}

// Migrate applies, in order, every pending file of its own bootstrap set and then of each
// set in sets, one transaction per file with the core.schema_migration row written in the
// same transaction (decision 10). It first validates everything (F32-F35, F41), refuses a
// read-only database (F36) and a concurrent run (F37), and compares every applied row with
// its file (F38, F39) so a problem anywhere stops the run before anything is applied.
// Each applied file is logged at info with set, version, name and duration, and returned.
// Nothing pending returns an empty slice and nil. A failing file is rolled back and
// reported with its set, version and name; earlier files stay applied (F40), and the
// files applied so far are returned with the error. ctx bounds the whole run and every
// statement in it; no QueryTimeout applies (F42).
func (m *Migrator) Migrate(ctx context.Context, sets []Set) ([]Applied, error) {
	const op = "db.Migrator.Migrate"
	prepared, err := m.prepare(op, sets)
	if err != nil {
		return nil, err
	}
	p := m.pool
	if err := p.usable(op); err != nil {
		return nil, err
	}
	conn, err := p.acquire(ctx)
	if err != nil {
		return nil, p.fail(ctx, op, err)
	}
	defer conn.Release()

	var res probeResult
	if err := conn.QueryRow(ctx, sqlReadonlyProbe).Scan(&res.version, &res.readOnly); err != nil {
		return nil, p.fail(ctx, op, err)
	}
	p.setReadOnly(ctx, res.readOnly)
	if res.readOnly {
		return nil, errs.E(op, errs.Unavailable, ErrReadOnly)
	}

	var got bool
	if err := conn.QueryRow(ctx, sqlAdvisoryTryLock, migrateLockKey).Scan(&got); err != nil {
		return nil, p.fail(ctx, op, err)
	}
	if !got {
		return nil, errs.E(op, errs.Conflict, errors.New("another bms migrate is running"))
	}
	defer func() {
		// The lock belongs to the session; give it back before the connection returns to
		// the pool, or drop the connection when that fails.
		uctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), probeTimeout)
		defer cancel()
		var released bool
		if uerr := conn.QueryRow(uctx, sqlAdvisoryUnlock, migrateLockKey).Scan(&released); uerr != nil || !released {
			m.logger.WarnContext(ctx, "could not release the migration lock; closing its connection")
			if cerr := conn.Hijack().Close(uctx); cerr != nil {
				m.logger.WarnContext(ctx, "closing the connection failed", slog.Any("error", Wrap(op, cerr)))
			}
		}
	}()

	rows, err := loadApplied(ctx, op, conn, "")
	if err != nil {
		return nil, p.fail(ctx, op, err)
	}
	var problems []error
	for _, ps := range prepared {
		problems = append(problems, diagnose(ps, rows, false)...)
	}
	if len(problems) > 0 {
		return nil, conflict(op, problems)
	}

	out := []Applied{}
	for _, ps := range prepared {
		if _, err := conn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{ps.Schema}.Sanitize()); err != nil {
			return out, fmt.Errorf("%s: create schema: %w", ps.Name, p.fail(ctx, op, err))
		}
		have := highest(rows, ps.Name)
		for _, f := range ps.files {
			if f.number <= have {
				continue
			}
			a, err := m.apply(ctx, conn, ps.Name, f)
			if err != nil {
				return out, err
			}
			out = append(out, a)
		}
	}
	return out, nil
}

// apply runs one file and records it in one transaction.
func (m *Migrator) apply(ctx context.Context, conn rowTx, set string, f migFile) (Applied, error) {
	const op = "db.Migrator.Migrate"
	p := m.pool
	fail := func(err error) (Applied, error) {
		return Applied{}, fmt.Errorf("%s %s: %w", set, f.label(), p.fail(ctx, op, err))
	}
	start := p.clock.Now()
	// Plain BEGIN, like Mode Write: a database switched to default_transaction_read_only refuses it.
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fail(err)
	}
	finished := false
	defer func() {
		if finished {
			return
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), probeTimeout)
		defer cancel()
		if rerr := tx.Rollback(rctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			m.logger.WarnContext(ctx, "rollback of a migration failed", slog.Any("error", Wrap(op, rerr)))
		}
	}()
	if _, err := tx.Exec(ctx, string(f.sql)); err != nil { // no args: simple protocol, several statements
		out, ferr := fail(err)
		ferr = withRaised(ferr, err)
		if raised(err) { // the author's text is for the operator: it goes to the log too (F58)
			m.logger.ErrorContext(ctx, "a migration raised an error and was rolled back",
				slog.String("set", set), slog.Int("version", f.number), slog.String("name", f.name),
				slog.Any("error", ferr))
		}
		return out, ferr
	}
	took := p.clock.Since(start)
	insert := sqlInsertApplied
	if set == bootstrapName {
		insert = sqlInsertBootstrap // the bootstrap file is idempotent and may have run before
	}
	if _, err := tx.Exec(ctx, insert, set, f.number, f.name, f.checksum, int(took.Milliseconds())); err != nil {
		return fail(err)
	}
	finished = true
	if err := tx.Commit(ctx); err != nil {
		return fail(err)
	}
	m.logger.InfoContext(ctx, "migration applied",
		slog.String("set", set), slog.Int("version", f.number), slog.String("name", f.name),
		slog.Duration("duration", took))
	return Applied{Set: set, Version: f.number, Name: f.name, Duration: took}, nil
}

// sqlstateRaise is the SQLSTATE of a RAISE EXCEPTION with no explicit ERRCODE.
const sqlstateRaise = "P0001"

// whereOwnBlock is how the server's Where starts (CONTEXT) for a RAISE in the migration's own
// DO block, which PostgreSQL runs as an anonymous function. A RAISE from a trigger or function
// the file fires has its own function name there, and is scrubbed like any server error (D52).
const whereOwnBlock = "PL/pgSQL function inline_code_block"

// withRaised appends the message (and DETAIL, when the file sets one) of a RAISE EXCEPTION
// raised by a migration file to failed, which is the already-classified error for driver. It is
// the one place a server error's text leaves the database layer (decision 4, D51): the author
// of the file wrote it to tell the operator what to fix, and a migration never puts personal
// data in it. It applies only to a RAISE in the file's own DO block (the error's Where starts
// with "PL/pgSQL function inline_code_block", D52): a RAISE from a trigger the file fires, and
// any other error, and a failed that is nil, come back unchanged.
func withRaised(failed, driver error) error {
	var pg *pgconn.PgError
	if failed == nil || !errors.As(driver, &pg) || !ownRaise(pg) {
		return failed
	}
	text := pg.Message
	if pg.Detail != "" {
		text += " (" + pg.Detail + ")"
	}
	return fmt.Errorf("%w: raised: %s", failed, text)
}

// raised reports whether err is a RAISE EXCEPTION (SQLSTATE P0001) from the migration's own
// DO block (D52).
func raised(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && ownRaise(pg)
}

// ownRaise reports whether pg is a P0001 raised in the migration's own DO block.
func ownRaise(pg *pgconn.PgError) bool {
	return pg.Code == sqlstateRaise && strings.HasPrefix(pg.Where, whereOwnBlock)
}

// rowTx is the part of a pooled connection apply needs.
type rowTx interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// Status reports, for the bootstrap set and each set in sets, the version the binary was
// built with, the version the database holds (0 when none, including when the table does
// not exist yet) and the problem CheckSchema would report, if any. It never changes the
// database. Fails with errs.Invalid for an invalid set (F41) and errs.Unavailable,
// errs.Timeout or errs.Canceled when the database cannot be read (F44). Consumers: app's
// `bms check`; api's GET /api/admin/modules (versions per module, module-contract Node).
func (m *Migrator) Status(ctx context.Context, sets []Set) ([]Status, error) {
	const op = "db.Migrator.Status"
	prepared, err := m.prepare(op, sets)
	if err != nil {
		return nil, err
	}
	rows, err := loadApplied(ctx, op, m.pool, "")
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(prepared))
	for _, ps := range prepared {
		st := Status{Set: ps.Name, Built: ps.files[len(ps.files)-1].number, Applied: highest(rows, ps.Name)}
		if problems := diagnose(ps, rows, true); len(problems) > 0 {
			st.Err = conflict("db.Migrator.CheckSchema", problems)
		}
		out = append(out, st)
	}
	return out, nil
}

// CheckSchema implements module.SchemaChecker (structurally; app asserts it). It fails
// with errs.Conflict when no version is recorded for module (including on an empty
// database), when the recorded version differs from Version(migrations) in either
// direction, or when an applied file's checksum or name differs from the file in
// migrations; the message names the module and both versions and says to run
// `bms migrate` (recorded lower) or to upgrade the binary (recorded higher) (F45-F47).
// It fails with errs.Invalid when module or migrations is malformed (F48, F50) and with
// errs.Unavailable, errs.Timeout or errs.Canceled when the database cannot be read
// (F49). It never writes.
func (m *Migrator) CheckSchema(ctx context.Context, module string, migrations fs.FS) error {
	const op = "db.Migrator.CheckSchema"
	if !setNameRE.MatchString(module) {
		return errs.E(op, errs.Invalid, errors.New("module name must match "+setNameRE.String()))
	}
	files, err := parseFiles(op, migrations)
	if err != nil {
		return err
	}
	rows, err := loadApplied(ctx, op, m.pool, module)
	if err != nil {
		return err
	}
	ps := preparedSet{Set: Set{Name: module}, files: files}
	if problems := diagnose(ps, rows, true); len(problems) > 0 {
		return conflict(op, problems)
	}
	return nil
}
