package account_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/identity/account"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

var now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func good() account.Record {
	return account.Record{
		ID:                model.NewID[model.AccountID](),
		Person:            model.NewID[model.PersonID](),
		Email:             "river.stone@example.org",
		PasswordHash:      secret.New("$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2g"),
		PasswordChangedAt: now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func TestRecord_Validate_Table(t *testing.T) {
	tests := []struct {
		name  string
		mod   func(*account.Record)
		field string
	}{
		{"good", func(*account.Record) {}, ""},
		{"zero id", func(r *account.Record) { r.ID = model.AccountID{} }, "id"},
		{"zero person", func(r *account.Record) { r.Person = model.PersonID{} }, "person"},
		{"bad email", func(r *account.Record) { r.Email = "nope" }, "email"},
		{"empty email", func(r *account.Record) { r.Email = "" }, "email"},
		{"unset hash", func(r *account.Record) { r.PasswordHash = secret.Secret{} }, "password_hash"},
		{"negative generation", func(r *account.Record) { r.Generation = -1 }, "generation"},
		{"zero password_changed_at", func(r *account.Record) { r.PasswordChangedAt = time.Time{} }, "password_changed_at"},
		{"zero created_at", func(r *account.Record) { r.CreatedAt = time.Time{} }, "created_at"},
		{"zero updated_at", func(r *account.Record) { r.UpdatedAt = time.Time{} }, "updated_at"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := good()
			tc.mod(&r)
			err := r.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v (%v)", errs.KindOf(err), err)
			}
			for _, fe := range validate.Fields(err) {
				if fe.Field == tc.field {
					return
				}
			}
			t.Fatalf("no field error for %q in %v", tc.field, validate.Fields(err))
		})
	}
}

func TestRecord_Validate_ReportsEveryProblem(t *testing.T) {
	err := account.Record{}.Validate()
	if got := len(validate.Fields(err)); got < 7 {
		t.Errorf("%d problems reported, want all of them", got)
	}
}

func TestRecord_NeverPrintsTheHashOrAddress(t *testing.T) {
	r := good()
	for _, s := range []string{fmt.Sprintf("%v", r), fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", r)} {
		if strings.Contains(s, "argon2id") || strings.Contains(s, "aGFzaGhhc2g") {
			t.Errorf("hash printed: %s", s)
		}
	}
	if err := (account.Record{PasswordHash: r.PasswordHash}).Validate(); err != nil &&
		strings.Contains(err.Error(), "aGFzaGhhc2g") {
		t.Error("Validate error carries the hash")
	}
}

func TestKey_IsZero(t *testing.T) {
	if !(account.Key{}).IsZero() {
		t.Error("zero key not zero")
	}
	if (account.Key{CreatedAt: now}).IsZero() || (account.Key{ID: model.NewID[model.AccountID]()}).IsZero() {
		t.Error("a key with a field set reports zero")
	}
}
