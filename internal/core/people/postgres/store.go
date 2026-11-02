// Package postgres is people's storage: the only package with SQL and the driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed migrations/*.sql
var embedded embed.FS

//go:embed sql/*.sql
var sqlFiles embed.FS

// Migrations is the migration set of people, rooted at the files (db.md: NNNN_name.sql at
// the set root). It is fs.Sub(embedded, "migrations") resolved in init; that Sub failing on
// an embedded directory is impossible, so init panics (go-conventions).
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic("people/postgres: embedded migrations missing: " + err.Error())
	}
	Migrations = sub
}

// The statements, loaded once. A missing file is impossible: the embed directive fails the
// build.
var (
	sqlGetPerson         = mustSQL("get_person.sql")
	sqlGetForUpdate      = mustSQL("get_person_for_update.sql")
	sqlInsertPerson      = mustSQL("insert_person.sql")
	sqlUpsertPerson      = mustSQL("upsert_person.sql")
	sqlUpdatePerson      = mustSQL("update_person.sql")
	sqlPlayaName         = mustSQL("get_playa_name.sql")
	sqlNamesOf           = mustSQL("names_of.sql")
	sqlLegalNamesOf      = mustSQL("legal_names_of.sql")
	sqlSearch            = mustSQL("search_people.sql")
	sqlLockDeletion      = mustSQL("lock_person_deletion.sql")
	sqlDeleteOrphan      = mustSQL("delete_orphan_person.sql")
	sqlDeletePerson      = mustSQL("delete_person.sql")
	sqlLockPerson        = mustSQL("lock_person.sql")
	sqlListContacts      = mustSQL("list_contacts_by_person.sql")
	sqlGetContact        = mustSQL("get_contact.sql")
	sqlInsertContact     = mustSQL("insert_contact.sql")
	sqlUpdateContact     = mustSQL("update_contact.sql")
	sqlDeleteContact     = mustSQL("delete_contact.sql")
	sqlRenumber          = mustSQL("renumber_contacts.sql")
	sqlDeleteContacts    = mustSQL("delete_contacts_by_person.sql")
	sqlGetConsent        = mustSQL("get_consent.sql")
	sqlListConsents      = mustSQL("list_consents_by_person.sql")
	sqlUpsertConsent     = mustSQL("upsert_consent.sql")
	sqlDeleteConsents    = mustSQL("delete_consents_by_person.sql")
	sqlExportPerson      = mustSQL("export_person.sql")
	sqlMarkNamePrompted  = mustSQL("mark_name_prompted.sql")
	sqlExportContacts    = mustSQL("export_contacts_by_person.sql")
	sqlExportConsents    = mustSQL("export_consents_by_person.sql")
	consentPersonFKeyKey = "person_consent_person_id_fkey"
)

// playaNameKeyIdx is the unique index that decides whether a playa name is taken
// (decision 29).
const playaNameKeyIdx = "person_playa_name_key_idx"

func mustSQL(name string) string {
	b, err := sqlFiles.ReadFile("sql/" + name)
	if err != nil {
		panic("people/postgres: embedded SQL missing: " + name)
	}
	return stripComments(string(b))
}

// stripComments drops the whole-line "--" comments of a statement file, so the documentation
// that sits above each statement is not sent to the server with every call.
func stripComments(sql string) string {
	var out strings.Builder
	for line := range strings.Lines(sql) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			out.WriteString(line)
		}
	}
	return strings.TrimSpace(out.String())
}

// Store keeps core.person, core.person_emergency_contact, core.person_consent and
// core.data_request in PostgreSQL. It holds a db.DB and no state; safe for concurrent use.
// Every method runs inside the caller's transaction when ctx carries one. Every error is
// already classified by db.Wrap.
type Store struct {
	db db.DB
}

// NewStore returns the store over d. Fails with errs.Invalid when d is nil.
func NewStore(d db.DB) (*Store, error) {
	if d == nil {
		return nil, errs.E("people.postgres.NewStore", errs.Invalid, errors.New("nil database"))
	}
	return &Store{db: d}, nil
}

// ---- argument and column helpers --------------------------------------------------------

// uuidOrNil is the statement argument for an optional ID: NULL for the zero ID.
func uuidOrNil[T ~[16]byte](id T) any {
	if id == (T{}) {
		return nil
	}
	return [16]byte(id)
}

// ptrArg is the argument for a patch field: NULL leaves the column unchanged, a value
// (possibly "") sets it.
func ptrArg(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// idOf converts a scanned optional uuid; NULL is the zero ID.
func idOf[T ~[16]byte](raw *[16]byte) T {
	if raw == nil {
		return T{}
	}
	return T(*raw)
}

func uuids[T ~[16]byte](ids []T) [][16]byte {
	out := make([][16]byte, len(ids))
	for i, id := range ids {
		out[i] = [16]byte(id)
	}
	return out
}

// takenOr re-kinds a unique violation on person_playa_name_key_idx as errs.Conflict with
// field playa_name code taken (decision 29). The error names nobody. Any other error,
// including another unique violation, is returned as it is.
func takenOr(op string, err error) error {
	var pg *db.Error
	if !errors.As(err, &pg) || pg.Code != "23505" || pg.Constraint != playaNameKeyIdx {
		return err
	}
	return errs.E(op, errs.Conflict, &validate.FieldError{
		Field:  "playa_name",
		Code:   "taken",
		Reason: "another person already uses this playa name",
	})
}

func notFound(op, what string) error {
	return errs.E(op, errs.NotFound, errors.New("no such "+what))
}

// scanner is the Scan of a pgx.Row and of a pgx.Rows.
type scanner interface{ Scan(dest ...any) error }

// queryAll runs query and scans every row.
func queryAll[T any](ctx context.Context, d db.DB, op, query string, scan func(scanner) (T, error), args ...any) ([]T, error) {
	rs, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, db.Wrap(op, err)
	}
	defer rs.Close()
	out := []T{}
	for rs.Next() {
		v, err := scan(rs)
		if err != nil {
			return nil, db.Wrap(op, err)
		}
		out = append(out, v)
	}
	if err := rs.Err(); err != nil {
		return nil, db.Wrap(op, err)
	}
	return out, nil
}

// queryOne runs query and scans its first row; ok is false when it returned none.
func queryOne[T any](ctx context.Context, d db.DB, op, query string, scan func(scanner) (T, error), args ...any) (v T, ok bool, err error) {
	all, err := queryAll(ctx, d, op, query, scan, args...)
	if err != nil || len(all) == 0 {
		return v, false, err
	}
	return all[0], true, nil
}

// ---- row scanners -----------------------------------------------------------------------

// scanPerson reads the columns P: id, playa_name, pronouns, language, legal_name, phone,
// contact_email, created_at, updated_at.
func scanPerson(sc scanner) (rows.Person, error) {
	var (
		p     rows.Person
		id    [16]byte
		email *string
	)
	if err := sc.Scan(&id, &p.PlayaName, &p.Pronouns, &p.Language, &p.LegalName, &p.Phone, &email,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return rows.Person{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	p.ID = model.PersonID(id)
	if email != nil {
		p.ContactEmail = *email
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, nil
}

// scanPersonExport reads P and the trailing name_prompted_at of export_person.sql (NULL is
// the zero time).
func scanPersonExport(sc scanner) (rows.Person, error) {
	var (
		p        rows.Person
		id       [16]byte
		email    *string
		prompted *time.Time
	)
	if err := sc.Scan(&id, &p.PlayaName, &p.Pronouns, &p.Language, &p.LegalName, &p.Phone, &email,
		&p.CreatedAt, &p.UpdatedAt, &prompted); err != nil {
		return rows.Person{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	p.ID = model.PersonID(id)
	if email != nil {
		p.ContactEmail = *email
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	if prompted != nil {
		p.NamePromptedAt = prompted.UTC()
	}
	return p, nil
}

// scanPersonKeyed reads P and the trailing playa_name_key of get_person_for_update.sql.
func scanPersonKeyed(sc scanner) (rows.Person, error) {
	var (
		p     rows.Person
		id    [16]byte
		email *string
	)
	if err := sc.Scan(&id, &p.PlayaName, &p.Pronouns, &p.Language, &p.LegalName, &p.Phone, &email,
		&p.CreatedAt, &p.UpdatedAt, &p.PlayaNameKey); err != nil {
		return rows.Person{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	p.ID = model.PersonID(id)
	if email != nil {
		p.ContactEmail = *email
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, nil
}

// scanPersonInserted reads P and the trailing "inserted" flag of upsert_person.sql.
func scanPersonInserted(sc scanner) (personInserted, error) {
	var (
		p        rows.Person
		id       [16]byte
		email    *string
		inserted bool
	)
	if err := sc.Scan(&id, &p.PlayaName, &p.Pronouns, &p.Language, &p.LegalName, &p.Phone, &email,
		&p.CreatedAt, &p.UpdatedAt, &inserted); err != nil {
		return personInserted{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	p.ID = model.PersonID(id)
	if email != nil {
		p.ContactEmail = *email
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return personInserted{p, inserted}, nil
}

type personInserted struct {
	row      rows.Person
	inserted bool
}

// scanContact reads the columns C: id, person_id, name, phone, relationship, position,
// created_at, updated_at.
func scanContact(sc scanner) (rows.Contact, error) {
	var (
		c        rows.Contact
		id, pid  [16]byte
		position int32
	)
	if err := sc.Scan(&id, &pid, &c.Name, &c.Phone, &c.Relationship, &position, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return rows.Contact{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	c.ID, c.Person, c.Position = rows.ContactID(id), model.PersonID(pid), int(position)
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return c, nil
}

// scanConsent reads the columns K: person_id, purpose, granted, recorded_by, changed_at.
func scanConsent(sc scanner) (rows.Consent, error) {
	var (
		c       rows.Consent
		pid     [16]byte
		by      *[16]byte
		changed time.Time
	)
	if err := sc.Scan(&pid, &c.Purpose, &c.Granted, &by, &changed); err != nil {
		return rows.Consent{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	c.Person, c.RecordedBy, c.ChangedAt = model.PersonID(pid), idOf[model.PersonID](by), changed.UTC()
	return c, nil
}

// ---- person -----------------------------------------------------------------------------

// GetPerson returns the profile row. Fails errs.NotFound when there is none.
func (s *Store) GetPerson(ctx context.Context, id model.PersonID) (rows.Person, error) {
	return s.person(ctx, "people.postgres.GetPerson", sqlGetPerson, [16]byte(id))
}

// GetForUpdate is GetPerson with a row lock, for a Write transaction. The row also carries
// PlayaNameKey as stored (the permanence check compares against it). Fails errs.NotFound
// when there is none.
func (s *Store) GetForUpdate(ctx context.Context, id model.PersonID) (rows.Person, error) {
	const op = "people.postgres.GetForUpdate"
	p, ok, err := queryOne(ctx, s.db, op, sqlGetForUpdate, scanPersonKeyed, [16]byte(id))
	if err != nil {
		return rows.Person{}, err
	}
	if !ok {
		return rows.Person{}, notFound(op, "person")
	}
	return p, nil
}

// ExportPerson returns the profile row for the export document. Fails errs.NotFound when
// there is none.
func (s *Store) ExportPerson(ctx context.Context, id model.PersonID) (rows.Person, error) {
	const op = "people.postgres.ExportPerson"
	p, ok, err := queryOne(ctx, s.db, op, sqlExportPerson, scanPersonExport, [16]byte(id))
	if err != nil {
		return rows.Person{}, err
	}
	if !ok {
		return rows.Person{}, notFound(op, "person")
	}
	return p, nil
}

// MarkNamePrompted records that person was asked for a playa name in the session issued at
// issued, when that is due: the person has no playa name and name_prompted_at is NULL or
// before issued (decisions 34, 35). It writes GREATEST(now, issued) and leaves updated_at
// alone. It returns true when the row was updated; false means not due (named, or already
// prompted this session) or no such row. The one statement decides under the row lock, so
// concurrent calls of one session get one true. It must run in the Write transaction ctx
// carries; on a read-only copy it fails errs.Unavailable wrapping db.ErrReadOnly, and with
// db's kinds otherwise.
func (s *Store) MarkNamePrompted(ctx context.Context, person model.PersonID, issued, now time.Time) (bool, error) {
	const op = "people.postgres.MarkNamePrompted"
	_, ok, err := queryOne(ctx, s.db, op, sqlMarkNamePrompted, idRow, [16]byte(person), issued.UTC(), now.UTC())
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (s *Store) person(ctx context.Context, op, query string, args ...any) (rows.Person, error) {
	p, ok, err := queryOne(ctx, s.db, op, query, scanPerson, args...)
	if err != nil {
		return rows.Person{}, err
	}
	if !ok {
		return rows.Person{}, notFound(op, "person")
	}
	return p, nil
}

// InsertPerson inserts the row (the text fields and ContactEmail as given, "" for none;
// PlayaNameKey is written with the name). inserted is false, and nothing was written, when
// the ID already has a row. Fails errs.Conflict with field playa_name code taken when
// another person holds the key, errs.Unavailable wrapping db.ErrReadOnly on a read-only
// copy, and with db's kinds.
func (s *Store) InsertPerson(ctx context.Context, p rows.Person, now time.Time) (rows.Person, bool, error) {
	const op = "people.postgres.InsertPerson"
	row, ok, err := queryOne(ctx, s.db, op, sqlInsertPerson, scanPerson,
		[16]byte(p.ID), p.PlayaName, p.Pronouns, p.Language, p.LegalName, p.Phone, p.ContactEmail,
		uuidOrNil(p.CreatedBy), now.UTC(), p.PlayaNameKey)
	if err != nil {
		return rows.Person{}, false, takenOr(op, err)
	}
	return row, ok, nil
}

// Upsert applies the patch to the person's row, or creates the row from it; inserted
// reports which. A patch that sets PlayaName sets PlayaNameKey with it. Fails as
// InsertPerson does.
func (s *Store) Upsert(ctx context.Context, p rows.PersonPatch, now time.Time) (rows.Person, bool, error) {
	const op = "people.postgres.Upsert"
	r, ok, err := queryOne(ctx, s.db, op, sqlUpsertPerson, scanPersonInserted,
		[16]byte(p.ID), ptrArg(p.PlayaName), ptrArg(p.Pronouns), ptrArg(p.Language), ptrArg(p.LegalName),
		ptrArg(p.Phone), ptrArg(p.ContactEmail), now.UTC(), ptrArg(p.PlayaNameKey))
	if err != nil {
		return rows.Person{}, false, takenOr(op, err)
	}
	if !ok {
		return rows.Person{}, false, errs.E(op, errs.Internal, errors.New("upsert returned no row"))
	}
	return r.row, r.inserted, nil
}

// UpdatePerson applies the patch to an existing row (PlayaNameKey travels with PlayaName).
// Fails errs.NotFound when there is none, and as InsertPerson does.
func (s *Store) UpdatePerson(ctx context.Context, p rows.PersonPatch, now time.Time) (rows.Person, error) {
	const op = "people.postgres.UpdatePerson"
	row, ok, err := queryOne(ctx, s.db, op, sqlUpdatePerson, scanPerson,
		[16]byte(p.ID), ptrArg(p.PlayaName), ptrArg(p.Pronouns), ptrArg(p.Language), ptrArg(p.LegalName),
		ptrArg(p.Phone), ptrArg(p.ContactEmail), now.UTC(), ptrArg(p.PlayaNameKey))
	if err != nil {
		return rows.Person{}, takenOr(op, err)
	}
	if !ok {
		return rows.Person{}, notFound(op, "person")
	}
	return row, nil
}

// PlayaNameOf returns the playa name ("" until filled in). Fails errs.NotFound when there
// is no row.
func (s *Store) PlayaNameOf(ctx context.Context, id model.PersonID) (string, error) {
	const op = "people.postgres.PlayaNameOf"
	var name string
	if err := s.db.QueryRow(ctx, sqlPlayaName, [16]byte(id)).Scan(&name); err != nil {
		return "", db.Wrap(op, err)
	}
	return name, nil
}

// NamesOf returns the playa name of each known person. Unknown IDs are omitted.
func (s *Store) NamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	const op = "people.postgres.NamesOf"
	type pair struct {
		id   model.PersonID
		name string
	}
	pairs, err := queryAll(ctx, s.db, op, sqlNamesOf, func(sc scanner) (pair, error) {
		var (
			p   pair
			raw [16]byte
		)
		if err := sc.Scan(&raw, &p.name); err != nil {
			return pair{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		p.id = model.PersonID(raw)
		return p, nil
	}, uuids(ids))
	if err != nil {
		return nil, err
	}
	out := make(map[model.PersonID]string, len(pairs))
	for _, p := range pairs {
		out[p.id] = p.name
	}
	return out, nil
}

// LegalNamesOf returns the legal name of each known person who has one. Unknown IDs and
// people with no legal name are omitted. The caller has checked people.contacts.read and
// audits what it gets back.
func (s *Store) LegalNamesOf(ctx context.Context, ids []model.PersonID) (map[model.PersonID]string, error) {
	const op = "people.postgres.LegalNamesOf"
	type pair struct {
		id   model.PersonID
		name string
	}
	pairs, err := queryAll(ctx, s.db, op, sqlLegalNamesOf, func(sc scanner) (pair, error) {
		var (
			p   pair
			raw [16]byte
		)
		if err := sc.Scan(&raw, &p.name); err != nil {
			return pair{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		p.id = model.PersonID(raw)
		return p, nil
	}, uuids(ids))
	if err != nil {
		return nil, err
	}
	out := make(map[model.PersonID]string, len(pairs))
	for _, p := range pairs {
		out[p.id] = p.name
	}
	return out, nil
}

// Search returns up to q.Limit matches ordered by (lower(playa_name), id). The matched
// flags say which selected personal column matched.
func (s *Store) Search(ctx context.Context, q rows.SearchQuery) ([]rows.Match, error) {
	const op = "people.postgres.Search"
	return queryAll(ctx, s.db, op, sqlSearch, func(sc scanner) (rows.Match, error) {
		var (
			m       rows.Match
			id      [16]byte
			sortKey string
		)
		if err := sc.Scan(&id, &m.PlayaName, &m.Pronouns, &m.Legal, &m.Phone, &m.Email, &sortKey); err != nil {
			return rows.Match{}, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		m.ID = model.PersonID(id)
		return m, nil
	}, q.Q, q.LegalName, q.Phone, q.ContactEmail, nil, nil, int32(q.Limit)) //nolint:gosec // Limit is at most people.MaxSearchResults
}

// LockDeletion takes the transaction-scoped advisory lock two deletions of one person
// serialise on. Fails errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Store) LockDeletion(ctx context.Context, id model.PersonID) error {
	const op = "people.postgres.LockDeletion"
	_, err := s.db.Exec(ctx, sqlLockDeletion, [16]byte(id))
	return db.Wrap(op, err)
}

// DeleteOrphan deletes the row only when it is still empty and nothing points at it. false
// means it was kept, also when another table's foreign key refused the delete.
func (s *Store) DeleteOrphan(ctx context.Context, id model.PersonID) (bool, error) {
	const op = "people.postgres.DeleteOrphan"
	_, ok, err := queryOne(ctx, s.db, op, sqlDeleteOrphan, idRow, [16]byte(id))
	if err != nil {
		if inUse(err) {
			return false, nil
		}
		return false, err
	}
	return ok, nil
}

// DeletePerson deletes the row and reports how many rows went (1). Fails errs.NotFound when
// there was none and errs.Conflict (with *db.Error.Constraint set) when a foreign key still
// references the person.
func (s *Store) DeletePerson(ctx context.Context, id model.PersonID) (int, error) {
	const op = "people.postgres.DeletePerson"
	_, ok, err := queryOne(ctx, s.db, op, sqlDeletePerson, idRow, [16]byte(id))
	if err != nil {
		if inUse(err) {
			return 0, errs.E(op, errs.Conflict, err)
		}
		return 0, err
	}
	if !ok {
		return 0, notFound(op, "person")
	}
	return 1, nil
}

// inUse reports a foreign key violation: a row elsewhere still references the person.
func inUse(err error) bool {
	var pe *db.Error
	return errors.As(err, &pe) && pe.Code == "23503"
}

func idRow(sc scanner) ([16]byte, error) {
	var id [16]byte
	if err := sc.Scan(&id); err != nil {
		return id, err //nolint:wrapcheck // queryAll wraps with db.Wrap
	}
	return id, nil
}

// count runs a statement that returns one row per deleted record and counts them.
func (s *Store) count(ctx context.Context, op, query string, args ...any) (int, error) {
	rs, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return 0, db.Wrap(op, err)
	}
	defer rs.Close()
	n := 0
	for rs.Next() {
		n++
	}
	if err := rs.Err(); err != nil {
		return 0, db.Wrap(op, err)
	}
	return n, nil
}

// ---- emergency contacts -----------------------------------------------------------------

// LockPerson locks the person's row until the transaction ends, serialising writers of the
// contact list. Fails errs.NotFound when there is no such person.
func (s *Store) LockPerson(ctx context.Context, id model.PersonID) error {
	const op = "people.postgres.LockPerson"
	_, ok, err := queryOne(ctx, s.db, op, sqlLockPerson, idRow, [16]byte(id))
	if err != nil {
		return err
	}
	if !ok {
		return notFound(op, "person")
	}
	return nil
}

// Contacts returns the person's contacts in display order.
func (s *Store) Contacts(ctx context.Context, person model.PersonID) ([]rows.Contact, error) {
	return queryAll(ctx, s.db, "people.postgres.Contacts", sqlListContacts, scanContact, [16]byte(person))
}

// ExportContacts is Contacts for the export document.
func (s *Store) ExportContacts(ctx context.Context, person model.PersonID) ([]rows.Contact, error) {
	return queryAll(ctx, s.db, "people.postgres.ExportContacts", sqlExportContacts, scanContact, [16]byte(person))
}

// GetContact returns one contact of the person. Fails errs.NotFound when there is none,
// also when it is another person's.
func (s *Store) GetContact(ctx context.Context, person model.PersonID, id rows.ContactID) (rows.Contact, error) {
	const op = "people.postgres.GetContact"
	c, ok, err := queryOne(ctx, s.db, op, sqlGetContact, scanContact, [16]byte(person), [16]byte(id))
	if err != nil {
		return rows.Contact{}, err
	}
	if !ok {
		return rows.Contact{}, notFound(op, "contact")
	}
	return c, nil
}

// AddContact appends c at the next position; added is false when the person already has
// limit contacts. Call LockPerson first in the same Write transaction.
func (s *Store) AddContact(ctx context.Context, c rows.Contact, limit int) (rows.Contact, bool, error) {
	const op = "people.postgres.AddContact"
	row, ok, err := queryOne(ctx, s.db, op, sqlInsertContact, scanContact,
		[16]byte(c.ID), [16]byte(c.Person), c.Name, c.Phone, c.Relationship, c.CreatedAt.UTC(), int32(limit)) //nolint:gosec // limit is people.MaxEmergencyContacts
	if err != nil {
		return rows.Contact{}, false, err
	}
	return row, ok, nil
}

// UpdateContact changes the text of a contact (c.UpdatedAt is the time). Fails
// errs.NotFound when the contact is not the person's.
func (s *Store) UpdateContact(ctx context.Context, c rows.Contact) (rows.Contact, error) {
	const op = "people.postgres.UpdateContact"
	row, ok, err := queryOne(ctx, s.db, op, sqlUpdateContact, scanContact,
		[16]byte(c.ID), [16]byte(c.Person), c.Name, c.Phone, c.Relationship, c.UpdatedAt.UTC())
	if err != nil {
		return rows.Contact{}, err
	}
	if !ok {
		return rows.Contact{}, notFound(op, "contact")
	}
	return row, nil
}

// DeleteContact deletes one contact. Fails errs.NotFound when it is not the person's.
// Follow with Renumber in the same transaction.
func (s *Store) DeleteContact(ctx context.Context, person model.PersonID, id rows.ContactID) error {
	const op = "people.postgres.DeleteContact"
	_, ok, err := queryOne(ctx, s.db, op, sqlDeleteContact, idRow, [16]byte(id), [16]byte(person))
	if err != nil {
		return err
	}
	if !ok {
		return notFound(op, "contact")
	}
	return nil
}

// Renumber closes the gap a delete left, so positions are 1..n.
func (s *Store) Renumber(ctx context.Context, person model.PersonID) error {
	const op = "people.postgres.Renumber"
	_, err := s.db.Exec(ctx, sqlRenumber, [16]byte(person))
	return db.Wrap(op, err)
}

// DeleteContactsOf deletes every contact of the person and reports how many.
func (s *Store) DeleteContactsOf(ctx context.Context, person model.PersonID) (int, error) {
	return s.count(ctx, "people.postgres.DeleteContactsOf", sqlDeleteContacts, [16]byte(person))
}

// ---- consents ---------------------------------------------------------------------------

// Consent reports whether the person currently grants purpose; false when there is no row.
func (s *Store) Consent(ctx context.Context, person model.PersonID, purpose string) (bool, error) {
	const op = "people.postgres.Consent"
	granted, ok, err := queryOne(ctx, s.db, op, sqlGetConsent, func(sc scanner) (bool, error) {
		var g bool
		if err := sc.Scan(&g); err != nil {
			return false, err //nolint:wrapcheck // queryAll wraps with db.Wrap
		}
		return g, nil
	}, [16]byte(person), purpose)
	if err != nil {
		return false, err
	}
	return ok && granted, nil
}

// Consents returns the person's consents ordered by purpose.
func (s *Store) Consents(ctx context.Context, person model.PersonID) ([]rows.Consent, error) {
	return queryAll(ctx, s.db, "people.postgres.Consents", sqlListConsents, scanConsent, [16]byte(person))
}

// ExportConsents is Consents for the export document.
func (s *Store) ExportConsents(ctx context.Context, person model.PersonID) ([]rows.Consent, error) {
	return queryAll(ctx, s.db, "people.postgres.ExportConsents", sqlExportConsents, scanConsent, [16]byte(person))
}

// SetConsent writes the current state of one purpose. Fails errs.NotFound when the person
// has no row.
func (s *Store) SetConsent(ctx context.Context, c rows.Consent) (rows.Consent, error) {
	const op = "people.postgres.SetConsent"
	row, ok, err := queryOne(ctx, s.db, op, sqlUpsertConsent, scanConsent,
		[16]byte(c.Person), c.Purpose, c.Granted, uuidOrNil(c.RecordedBy), c.ChangedAt.UTC())
	if err != nil {
		var pe *db.Error
		if errors.As(err, &pe) && pe.Constraint == consentPersonFKeyKey {
			return rows.Consent{}, errs.E(op, errs.NotFound, err)
		}
		return rows.Consent{}, err
	}
	if !ok {
		return rows.Consent{}, errs.E(op, errs.Internal, errors.New("upsert returned no row"))
	}
	return row, nil
}

// DeleteConsentsOf deletes every consent of the person and reports how many.
func (s *Store) DeleteConsentsOf(ctx context.Context, person model.PersonID) (int, error) {
	return s.count(ctx, "people.postgres.DeleteConsentsOf", sqlDeleteConsents, [16]byte(person))
}
