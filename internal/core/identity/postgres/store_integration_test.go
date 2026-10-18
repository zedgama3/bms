//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/core/identity/postgres"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

var t0 = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

const (
	hashA = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRB$aGFzaGhhc2hB"
	hashB = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRC$aGFzaGhhc2hC"
	hashC = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRD$aGFzaGhhc2hD"
)

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

type fixture struct {
	pool  *db.Pool
	store *postgres.Store
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := dbtest.TempDB(t)
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mig.Migrate(ctxT(t), []db.Set{identity.Set}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	s, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{pool: pool, store: s}
}

func rec(email string, created time.Time) account.Record {
	return account.Record{
		ID:                model.NewID[model.AccountID](),
		Person:            model.NewID[model.PersonID](),
		Email:             model.Email(email),
		PasswordHash:      secret.New(hashA),
		PasswordChangedAt: created,
		CreatedAt:         created,
		UpdatedAt:         created,
	}
}

func (f *fixture) insert(t *testing.T, r account.Record) account.Record {
	t.Helper()
	if err := f.store.Insert(ctxT(t), r); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return r
}

func wantKind(t *testing.T, err error, k errs.Kind) {
	t.Helper()
	if !errs.Is(err, k) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, k)
	}
}

func wantConstraint(t *testing.T, err error, name string) {
	t.Helper()
	wantKind(t, err, errs.Conflict)
	var de *db.Error
	if !errors.As(err, &de) || de.Constraint != name {
		t.Fatalf("constraint = %+v, want %s", de, name)
	}
}

// F63
func TestStore_Integration_Table(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("River.Stone@Example.org", t0))
	other := rec("other.person@example.org", t0.Add(time.Minute))

	t.Run("round trip by every key", func(t *testing.T) {
		for name, get := range map[string]func() (account.Record, error){
			"ByID":             func() (account.Record, error) { return f.store.ByID(ctxT(t), r.ID) },
			"ByCanonicalEmail": func() (account.Record, error) { return f.store.ByCanonicalEmail(ctxT(t), "RIVER.stone@EXAMPLE.org") },
			"ByPerson":         func() (account.Record, error) { return f.store.ByPerson(ctxT(t), r.Person) },
		} {
			got, err := get()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got.ID != r.ID || got.Person != r.Person || got.Email != "River.Stone@Example.org" {
				t.Errorf("%s: %+v (the address is stored as entered)", name, got)
			}
			if got.PasswordHash.Value() != hashA {
				t.Errorf("%s: hash not returned for verification", name)
			}
			if !got.DisabledAt.IsZero() || !got.LastLoginAt.IsZero() || got.Superuser || got.PasswordChangeRequired || got.Generation != 0 {
				t.Errorf("%s: defaults wrong: %+v", name, got)
			}
			if !got.CreatedAt.Equal(t0) || !got.PasswordChangedAt.Equal(t0) || !got.UpdatedAt.Equal(t0) {
				t.Errorf("%s: times %v %v %v", name, got.CreatedAt, got.PasswordChangedAt, got.UpdatedAt)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("%s: stored record invalid: %v", name, err)
			}
		}
	})
	t.Run("state", func(t *testing.T) {
		st, err := f.store.StateByID(ctxT(t), r.ID)
		if err != nil || st.Person != r.Person || st.Disabled || st.Superuser || st.PasswordChangeRequired || st.Generation != 0 {
			t.Fatalf("state = %+v, %v", st, err)
		}
	})
	t.Run("unknown keys are NotFound", func(t *testing.T) {
		_, err := f.store.ByID(ctxT(t), model.NewID[model.AccountID]())
		wantKind(t, err, errs.NotFound)
		_, err = f.store.ByCanonicalEmail(ctxT(t), "nobody@example.org")
		wantKind(t, err, errs.NotFound)
		_, err = f.store.ByPerson(ctxT(t), model.NewID[model.PersonID]())
		wantKind(t, err, errs.NotFound)
		_, err = f.store.StateByID(ctxT(t), model.NewID[model.AccountID]())
		wantKind(t, err, errs.NotFound)
		_, err = f.store.ExportByPerson(ctxT(t), model.NewID[model.PersonID]())
		wantKind(t, err, errs.NotFound)
	})
	t.Run("duplicate address ignoring case", func(t *testing.T) {
		d := rec("RIVER.STONE@example.org", t0)
		wantConstraint(t, f.store.Insert(ctxT(t), d), "account_email_canonical_key")
	})
	t.Run("second account for one person", func(t *testing.T) {
		d := rec("another.address@example.org", t0)
		d.Person = r.Person
		wantConstraint(t, f.store.Insert(ctxT(t), d), "account_person_id_key")
	})
	t.Run("duplicate id", func(t *testing.T) {
		d := rec("third.address@example.org", t0)
		d.ID = r.ID
		wantConstraint(t, f.store.Insert(ctxT(t), d), "account_pkey")
	})
	t.Run("generated canonical column", func(t *testing.T) {
		var canon string
		if err := f.pool.QueryRow(ctxT(t), "SELECT email_canonical FROM core.account WHERE id = $1", [16]byte(r.ID)).Scan(&canon); err != nil {
			t.Fatal(err)
		}
		if canon != "river.stone@example.org" {
			t.Errorf("email_canonical = %q", canon)
		}
		if _, err := f.pool.Exec(ctxT(t), "UPDATE core.account SET email_canonical = 'x' WHERE id = $1", [16]byte(r.ID)); err == nil {
			t.Error("a generated column accepted a write")
		}
	})
	t.Run("row checks refuse bad data", func(t *testing.T) {
		short := rec("a@", t0) // 2 bytes
		if err := f.store.Insert(ctxT(t), short); err == nil {
			t.Error("a 2-byte address was stored")
		}
		nilID := rec("nil.id@example.org", t0)
		nilID.ID = model.AccountID{}
		if err := f.store.Insert(ctxT(t), nilID); err == nil {
			t.Error("the all-zero ID was stored")
		}
		neg := rec("neg.gen@example.org", t0)
		neg.Generation = -1
		if err := f.store.Insert(ctxT(t), neg); err == nil {
			t.Error("a negative generation was stored")
		}
	})
	t.Run("a cancelled context is Canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctxT(t))
		cancel()
		_, err := f.store.ByID(ctx, r.ID)
		wantKind(t, err, errs.Canceled)
	})
	_ = other
}

func TestStore_ListKeysetOrder(t *testing.T) {
	f := newFixture(t)
	var all []account.Record
	for i := range 3 { // three at the same instant: the id breaks the tie
		all = append(all, f.insert(t, rec(fmt.Sprintf("tie%d@example.org", i), t0)))
	}
	for i := range 3 {
		all = append(all, f.insert(t, rec(fmt.Sprintf("later%d@example.org", i), t0.Add(time.Duration(i+1)*time.Minute))))
	}
	var got []account.Record
	var after account.Key
	for pages := 0; ; pages++ {
		page, err := f.store.List(ctxT(t), after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		got = append(got, page...)
		last := page[len(page)-1]
		after = account.Key{CreatedAt: last.CreatedAt, ID: last.ID}
		if pages > 10 {
			t.Fatal("paging does not end")
		}
	}
	if len(got) != len(all) {
		t.Fatalf("got %d, want %d", len(got), len(all))
	}
	seen := map[model.AccountID]bool{}
	for i, g := range got {
		if seen[g.ID] {
			t.Fatalf("repeat at %d", i)
		}
		seen[g.ID] = true
		if g.PasswordHash.IsSet() {
			t.Error("a list read the password hash")
		}
		if i > 0 {
			p := got[i-1]
			if g.CreatedAt.After(p.CreatedAt) || (g.CreatedAt.Equal(p.CreatedAt) && string(g.ID.String()) > string(p.ID.String())) {
				t.Errorf("order broken between %d and %d", i-1, i)
			}
		}
	}
	if !got[0].CreatedAt.Equal(t0.Add(3 * time.Minute)) {
		t.Error("not newest first")
	}
	// a limit larger than the table returns everything
	page, err := f.store.List(ctxT(t), account.Key{}, 100)
	if err != nil || len(page) != len(all) {
		t.Errorf("List(100) = %d, %v", len(page), err)
	}
}

func TestStore_ListByEmailAndExport_NeverReadTheHash(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("Rae.Finch@example.org", t0))
	f.insert(t, rec("someone.else@example.org", t0))
	recs, err := f.store.ListByEmail(ctxT(t), "rae.FINCH@example.org")
	if err != nil || len(recs) != 1 || recs[0].ID != r.ID || recs[0].PasswordHash.IsSet() {
		t.Fatalf("ListByEmail = %+v, %v", recs, err)
	}
	recs, err = f.store.ListByEmail(ctxT(t), "rae@example.org")
	if err != nil || len(recs) != 0 {
		t.Fatalf("partial match: %+v, %v", recs, err)
	}
	exp, err := f.store.ExportByPerson(ctxT(t), r.Person)
	if err != nil || exp.ID != r.ID || exp.PasswordHash.IsSet() {
		t.Fatalf("ExportByPerson = %+v, %v", exp, err)
	}
}

func TestStore_PasswordMethods_AtomicGenerationBumps(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("river.stone@example.org", t0))
	at := t0.Add(time.Hour)

	gen, err := f.store.SetTemporaryPassword(ctxT(t), r.ID, secret.New(hashB), at)
	if err != nil || gen != 1 {
		t.Fatalf("SetTemporaryPassword = %d, %v", gen, err)
	}
	got := f.byID(t, r.ID)
	if !got.PasswordChangeRequired || got.PasswordHash.Value() != hashB || got.Generation != 1 {
		t.Errorf("after temporary: %+v", got)
	}
	gen, err = f.store.SetPassword(ctxT(t), r.ID, secret.New(hashC), at.Add(time.Minute))
	if err != nil || gen != 2 {
		t.Fatalf("SetPassword = %d, %v", gen, err)
	}
	got = f.byID(t, r.ID)
	if got.PasswordChangeRequired || got.PasswordHash.Value() != hashC || !got.PasswordChangedAt.Equal(at.Add(time.Minute)) {
		t.Errorf("after set: %+v", got)
	}
	gen, err = f.store.BumpGeneration(ctxT(t), r.ID, at)
	if err != nil || gen != 3 {
		t.Fatalf("BumpGeneration = %d, %v", gen, err)
	}
	for name, run := range map[string]func() error{
		"SetPassword": func() error {
			_, e := f.store.SetPassword(ctxT(t), model.NewID[model.AccountID](), secret.New(hashA), at)
			return keep(e)
		},
		"SetTemporaryPassword": func() error {
			_, e := f.store.SetTemporaryPassword(ctxT(t), model.NewID[model.AccountID](), secret.New(hashA), at)
			return keep(e)
		},
		"BumpGeneration": func() error {
			_, e := f.store.BumpGeneration(ctxT(t), model.NewID[model.AccountID](), at)
			return keep(e)
		},
	} {
		wantKind(t, run(), errs.NotFound)
		_ = name
	}
}

func TestStore_ConcurrentBumps_NoLostUpdate(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("river.stone@example.org", t0))
	const n = 10
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			if _, err := f.store.BumpGeneration(ctxT(t), r.ID, t0); err != nil {
				t.Errorf("BumpGeneration: %v", err)
			}
		})
	}
	wg.Wait()
	st, err := f.store.StateByID(ctxT(t), r.ID)
	if err != nil || st.Generation != n {
		t.Fatalf("generation = %d, %v, want %d", st.Generation, err, n)
	}
}

func TestStore_DisableEnable_ReportChange(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("river.stone@example.org", t0))
	at := t0.Add(time.Hour)
	if ch, err := f.store.Enable(ctxT(t), r.ID, at); err != nil || ch {
		t.Fatalf("Enable of enabled = %v, %v, want false", ch, err)
	}
	if ch, err := f.store.Disable(ctxT(t), r.ID, at); err != nil || !ch {
		t.Fatalf("Disable = %v, %v, want true", ch, err)
	}
	st := f.state(t, r.ID)
	if !st.Disabled || st.Generation != 1 {
		t.Errorf("state = %+v", st)
	}
	if ch, err := f.store.Disable(ctxT(t), r.ID, at.Add(time.Minute)); err != nil || ch {
		t.Fatalf("second Disable = %v, %v, want false", ch, err)
	}
	if st = f.state(t, r.ID); st.Generation != 1 {
		t.Error("second Disable bumped the generation")
	}
	got := f.byID(t, r.ID)
	if !got.DisabledAt.Equal(at) {
		t.Errorf("disabled_at = %v, want the first disable's time", got.DisabledAt)
	}
	if ch, err := f.store.Enable(ctxT(t), r.ID, at); err != nil || !ch {
		t.Fatalf("Enable = %v, %v, want true", ch, err)
	}
	if st = f.state(t, r.ID); st.Disabled {
		t.Error("still disabled")
	}
}

func TestStore_TouchLogin_OnlyLastLogin(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("river.stone@example.org", t0))
	at := t0.Add(2 * time.Hour)
	if err := f.store.TouchLogin(ctxT(t), r.ID, at); err != nil {
		t.Fatal(err)
	}
	got := f.byID(t, r.ID)
	if !got.LastLoginAt.Equal(at) || !got.UpdatedAt.Equal(t0) || got.Generation != 0 || got.PasswordHash.Value() != hashA {
		t.Errorf("after touch: %+v", got)
	}
	if err := f.store.TouchLogin(ctxT(t), model.NewID[model.AccountID](), at); err != nil {
		t.Errorf("a vanished account is not an error: %v", err)
	}
}

func TestStore_Rehash_CompareAndSwap(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("river.stone@example.org", t0))
	if err := f.store.Rehash(ctxT(t), r.ID, secret.New(hashB), secret.New(hashC)); err != nil { // stale old hash
		t.Fatal(err)
	}
	if got := f.byID(t, r.ID); got.PasswordHash.Value() != hashA {
		t.Error("rehash overwrote a password it had not verified")
	}
	if err := f.store.Rehash(ctxT(t), r.ID, secret.New(hashB), secret.New(hashA)); err != nil {
		t.Fatal(err)
	}
	got := f.byID(t, r.ID)
	if got.PasswordHash.Value() != hashB || got.Generation != 0 || !got.PasswordChangedAt.Equal(t0) {
		t.Errorf("after rehash: %+v", got)
	}
}

func TestStore_Superuser_CreateResetConflict(t *testing.T) {
	f := newFixture(t)
	id, person := model.NewID[model.AccountID](), model.NewID[model.PersonID]()
	su, err := f.store.Superuser(ctxT(t), id, person, "Root.Operator@example.org", secret.New(hashA), t0)
	if err != nil || !su.Created || su.ID != id || su.Person != person {
		t.Fatalf("create = %+v, %v", su, err)
	}
	if _, err := f.pool.Exec(ctxT(t), "UPDATE core.account SET disabled_at = $1, password_change_required = true WHERE id = $2", t0, [16]byte(id)); err != nil {
		t.Fatal(err)
	}
	su2, err := f.store.Superuser(ctxT(t), model.NewID[model.AccountID](), model.NewID[model.PersonID](), "root.operator@EXAMPLE.org", secret.New(hashB), t0.Add(time.Hour))
	if err != nil || su2.Created || su2.ID != id || su2.Person != person {
		t.Fatalf("reset = %+v, %v (the existing IDs must be kept)", su2, err)
	}
	got := f.byID(t, id)
	if !got.Superuser || got.Generation != 1 || !got.DisabledAt.IsZero() || got.PasswordChangeRequired || got.PasswordHash.Value() != hashB || got.Email != "Root.Operator@example.org" {
		t.Errorf("after reset: %+v", got)
	}
	p := f.insert(t, rec("participant@example.org", t0))
	_, err = f.store.Superuser(ctxT(t), model.NewID[model.AccountID](), model.NewID[model.PersonID](), "Participant@example.org", secret.New(hashC), t0)
	wantKind(t, err, errs.Conflict)
	if got := f.byID(t, p.ID); got.Superuser || got.PasswordHash.Value() != hashA || got.Generation != 0 {
		t.Errorf("participant changed: %+v", got)
	}
}

func TestStore_DeleteByPerson(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("river.stone@example.org", t0))
	keep := f.insert(t, rec("keep.me@example.org", t0))
	id, n, err := f.store.DeleteByPerson(ctxT(t), r.Person)
	if err != nil || n != 1 || id != r.ID {
		t.Fatalf("DeleteByPerson = %v, %d, %v", id, n, err)
	}
	id, n, err = f.store.DeleteByPerson(ctxT(t), r.Person)
	if err != nil || n != 0 || !id.IsZero() {
		t.Fatalf("second DeleteByPerson = %v, %d, %v", id, n, err)
	}
	if _, err := f.store.ByID(ctxT(t), keep.ID); err != nil {
		t.Errorf("another account was deleted: %v", err)
	}
}

func TestStore_ConcurrentInsertSameAddress_OneWins(t *testing.T) {
	f := newFixture(t)
	const n = 8
	res := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { res <- f.store.Insert(ctxT(t), rec("race.runner@example.org", t0)) })
	}
	wg.Wait()
	close(res)
	ok, conflict := 0, 0
	for err := range res {
		switch {
		case err == nil:
			ok++
		case errs.Is(err, errs.Conflict):
			conflict++
		default:
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 || conflict != n-1 {
		t.Errorf("ok=%d conflict=%d", ok, conflict)
	}
}

// F63: writes inside a read-only transaction are refused as db.Wrap classifies them.
func TestStore_ReadOnlyTransaction_WritesUnavailable(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t, rec("river.stone@example.org", t0))
	err := f.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, err := f.pool.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
			return err //nolint:wrapcheck // test helper
		}
		return f.store.TouchLogin(ctx, r.ID, t0.Add(time.Hour))
	})
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Errorf("error does not wrap db.ErrReadOnly: %v", err)
	}
	err = f.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, err := f.pool.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
			return err //nolint:wrapcheck // test helper
		}
		_, err := f.store.BumpGeneration(ctx, r.ID, t0)
		return keep(err)
	})
	wantKind(t, err, errs.Unavailable)
	// reads still work inside it
	if err := f.pool.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		_, err := f.store.StateByID(ctx, r.ID)
		return keep(err)
	}); err != nil {
		t.Errorf("read in a read transaction: %v", err)
	}
}

func (f *fixture) byID(t *testing.T, id model.AccountID) account.Record {
	t.Helper()
	r, err := f.store.ByID(ctxT(t), id)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	return r
}

func (f *fixture) state(t *testing.T, id model.AccountID) account.State {
	t.Helper()
	st, err := f.store.StateByID(ctxT(t), id)
	if err != nil {
		t.Fatalf("StateByID: %v", err)
	}
	return st
}

// keep wraps err so the kind survives while the linter sees a wrapped error; nil stays nil.
func keep(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("test: %w", err)
}
