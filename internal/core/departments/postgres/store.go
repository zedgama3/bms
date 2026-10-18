// Package postgres is departments' storage: the only package with SQL and the driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of departments, rooted at the files (db.md: NNNN_name.sql
// at the set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing
// on an embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("departments/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlList         = mustSQL("list_departments.sql")
	sqlGet          = mustSQL("get_department.sql")
	sqlGetForUpdate = mustSQL("get_department_for_update.sql")
	sqlInsert       = mustSQL("insert_department.sql")
	sqlUpdate       = mustSQL("update_department.sql")
	sqlArchive      = mustSQL("archive_department.sql")
	sqlRestore      = mustSQL("restore_department.sql")
	sqlNamesOf      = mustSQL("names_of.sql")
	sqlIsActive     = mustSQL("is_active.sql")
)

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("departments/postgres: embedded SQL missing: " + name)
	}
	return stripComments(string(b))
}

// stripComments drops the whole-line "--" comments of a statement file, so the
// documentation above each statement is not sent to the server with every call.
func stripComments(sql string) string {
	var out strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Store keeps core.department in PostgreSQL. It holds a db.DB and no state; safe for
// concurrent use. Every method runs inside the caller's transaction when ctx carries one.
// Every error is already classified by db.Wrap.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("departments.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// scanner is the Scan of a pgx.Row and of a pgx.Rows.
type scanner interface{ Scan(dest ...any) error }

// scanDepartment reads id, name, description, contact_email, archived_at, created_at,
// updated_at.
func scanDepartment(sc scanner) (rows.Department, error) {
	var (
		d        rows.Department
		id       [16]byte
		archived *time.Time
	)
	if err := sc.Scan(&id, &d.Name, &d.Description, &d.ContactEmail, &archived, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return rows.Department{}, err //nolint:wrapcheck // callers wrap with db.Wrap
	}
	d.ID = model.DepartmentID(id)
	if archived != nil {
		d.ArchivedAt = archived.UTC()
	}
	d.CreatedAt, d.UpdatedAt = d.CreatedAt.UTC(), d.UpdatedAt.UTC()
	return d, nil
}

// queryAll runs query and scans every row.
func (s *Store) queryAll(ctx context.Context, op, query string, args ...any) ([]rows.Department, error) {
	rs, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []rows.Department{}
	for rs.Next() {
		d, err := scanDepartment(rs)
		if err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, d)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// queryOne runs query and returns its first row; none is errs.NotFound.
func (s *Store) queryOne(ctx context.Context, op, query string, args ...any) (rows.Department, error) {
	all, err := s.queryAll(ctx, op, query, args...)
	if err != nil {
		return rows.Department{}, err
	}
	if len(all) == 0 {
		return rows.Department{}, errs.E(op, errs.NotFound, errors.New("no such department"))
	}
	return all[0], nil
}

// List returns the departments by lower(name); the archived ones only when includeArchived.
func (s *Store) List(ctx context.Context, includeArchived bool) ([]rows.Department, error) {
	return s.queryAll(ctx, "departments.postgres.List", sqlList, includeArchived)
}

// Get returns one department, archived or not. Fails errs.NotFound when there is none.
func (s *Store) Get(ctx context.Context, id model.DepartmentID) (rows.Department, error) {
	return s.queryOne(ctx, "departments.postgres.Get", sqlGet, [16]byte(id))
}

// GetForUpdate is Get with a row lock, for a Write transaction.
func (s *Store) GetForUpdate(ctx context.Context, id model.DepartmentID) (rows.Department, error) {
	return s.queryOne(ctx, "departments.postgres.GetForUpdate", sqlGetForUpdate, [16]byte(id))
}

// Insert inserts d with its CreatedAt as UpdatedAt too. Fails errs.Conflict when the name
// belongs to another department ignoring case (constraint department_name_key), and
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Store) Insert(ctx context.Context, d rows.Department) (rows.Department, error) {
	return s.queryOne(ctx, "departments.postgres.Insert", sqlInsert,
		[16]byte(d.ID), d.Name, d.Description, d.ContactEmail, d.CreatedAt.UTC())
}

// ptrArg is the argument for a patch field: NULL leaves the column unchanged, a value
// (possibly "") sets it.
func ptrArg(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// Update applies the patch. Fails errs.NotFound when there is no such department and
// errs.Conflict for a name that is taken.
func (s *Store) Update(ctx context.Context, p rows.Patch, now time.Time) (rows.Department, error) {
	return s.queryOne(ctx, "departments.postgres.Update", sqlUpdate,
		[16]byte(p.ID), ptrArg(p.Name), ptrArg(p.Description), ptrArg(p.ContactEmail), now.UTC())
}

// Archive archives an active department. Fails errs.Conflict when it is not active or
// unknown (the caller has locked the row and checked, so this is a safety net).
func (s *Store) Archive(ctx context.Context, id model.DepartmentID, now time.Time) (rows.Department, error) {
	return s.flip(ctx, "departments.postgres.Archive", sqlArchive, id, now)
}

// Restore restores an archived department. Fails errs.Conflict when it is not archived or
// unknown.
func (s *Store) Restore(ctx context.Context, id model.DepartmentID, now time.Time) (rows.Department, error) {
	return s.flip(ctx, "departments.postgres.Restore", sqlRestore, id, now)
}

func (s *Store) flip(ctx context.Context, op, query string, id model.DepartmentID, now time.Time) (rows.Department, error) {
	d, err := s.queryOne(ctx, op, query, [16]byte(id), now.UTC())
	if errs.Is(err, errs.NotFound) {
		return rows.Department{}, errs.E(op, errs.Conflict, errors.New("the department is not in the state the change needs"))
	}
	return d, err
}

// NamesOf maps each known ID, archived included, to its name. Unknown IDs are omitted.
func (s *Store) NamesOf(ctx context.Context, ids []model.DepartmentID) (map[model.DepartmentID]string, error) {
	const op = "departments.postgres.NamesOf"
	raw := make([][16]byte, len(ids))
	for i, id := range ids {
		raw[i] = [16]byte(id)
	}
	rs, err := s.db.Query(ctx, sqlNamesOf, raw)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := make(map[model.DepartmentID]string, len(ids))
	for rs.Next() {
		var (
			id   [16]byte
			name string
		)
		if err := rs.Scan(&id, &name); err != nil {
			return nil, db.Wrap(op, err)
		}
		out[model.DepartmentID(id)] = name
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// Active reports whether the department exists and is not archived.
func (s *Store) Active(ctx context.Context, id model.DepartmentID) (bool, error) {
	const op = "departments.postgres.Active"
	var active bool
	err := s.db.QueryRow(ctx, sqlIsActive, [16]byte(id)).Scan(&active)
	if errs.Is(db.Wrap(op, err), errs.NotFound) {
		return false, nil // the statement always answers a row; no row still means not active
	}
	if err != nil {
		return false, db.Wrap(op, err)
	}
	return active, nil
}
