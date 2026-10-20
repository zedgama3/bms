package settings

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/settings/rows"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Every value in this file is invented.

// memStore is an in-memory store: one revision and a map of plain values.
type memStore struct {
	mu     sync.Mutex
	rev    int64
	values map[string]string
}

func newMemStore(rev int64) *memStore { return &memStore{rev: rev, values: map[string]string{}} }

func (m *memStore) setRevision(rev int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rev = rev
}

func (m *memStore) Revision(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rev, nil
}
func (m *memStore) LockRevision(ctx context.Context) (int64, error) { return m.Revision(ctx) }
func (m *memStore) BumpRevision(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rev++
	return m.rev, nil
}
func (m *memStore) LoadSettings(context.Context) ([]rows.Setting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []rows.Setting
	for k, v := range m.values {
		out = append(out, rows.Setting{Key: k, Value: &v})
	}
	return out, nil
}
func (m *memStore) LoadTerminology(context.Context) ([]rows.Term, error) { return nil, nil }
func (m *memStore) UpsertSetting(_ context.Context, key string, value *string, _ []byte, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value == nil {
		return true, nil
	}
	old, had := m.values[key]
	m.values[key] = *value
	return !had || old != *value, nil
}
func (m *memStore) DeleteSetting(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, had := m.values[key]
	delete(m.values, key)
	return had, nil
}
func (m *memStore) UpsertTermForms(context.Context, string, string, []string, []string, time.Time) ([]string, error) {
	return nil, nil
}
func (m *memStore) DeleteTermForms(context.Context, string, string, []string) ([]string, error) {
	return nil, nil
}

type testSvc struct {
	*Service
	store  *memStore
	cap    *telemetrytest.Capture
	person model.PersonID
}

func newTestSvc(t *testing.T, rev int64, areas ...Area) *testSvc {
	t.Helper()
	c := telemetrytest.New(t)
	person := model.NewID[model.PersonID]()
	az := authztest.NewFake()
	actor := func(context.Context) (authz.Actor, bool) { return authz.Actor{Person: person}, true }
	az.SetActor(actor)
	az.Grant(person, PermissionManage)
	st := newMemStore(rev)
	svc, err := newService(Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)),
		Logger: c.Logger(), Tracer: c.Tracer("settings"), Audit: audittest.NewFake("settings"),
		Authz: az, Actor: actor, Areas: areas,
	}, st)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return &testSvc{Service: svc, store: st, cap: c, person: person}
}

func regressionWarnings(c *telemetrytest.Capture) int {
	n := 0
	for _, r := range c.Records() {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, "went backwards") {
			n++
		}
	}
	return n
}

func TestInternalChangeOf_SecretCheckFailure_DoesNotEchoPlaintext(t *testing.T) {
	const plaintext = "correct-horse-battery"
	tests := map[string]error{
		"plain error":      errors.New(plaintext + " is too weak"),
		"field error":      &validate.FieldError{Field: "password", Code: "range", Reason: "bad: " + plaintext},
		"wrapped field":    errs.E("check", errs.Invalid, &validate.FieldError{Code: "too_long", Params: map[string]string{"max": "9"}, Reason: plaintext}),
		"errs.E plain":     errs.E("check", errs.Invalid, errors.New(plaintext)),
		"joined plaintext": errors.Join(errors.New(plaintext), errors.New("x")),
	}
	for name, checkErr := range tests {
		t.Run(name, func(t *testing.T) {
			e := entry{area: "mail", full: "mail.password", decl: Decl{
				Key: "password", Kind: Secret, Check: func(string) error { return checkErr },
			}}
			v := plaintext
			_, problems := (&Service{}).changeOf(e, &v)
			if len(problems) == 0 {
				t.Fatal("no problem reported")
			}
			for _, p := range problems {
				if strings.Contains(p.Error(), plaintext) {
					t.Errorf("the problem echoes the plaintext: %v", p)
				}
				for _, fe := range validate.Fields(p) {
					if fe.Field != "mail.password" || fe.Code == "" {
						t.Errorf("field error = %+v", fe)
					}
				}
			}
		})
	}
}

func TestInternalChangeOf_PlainCheckFailure_KeepsReason(t *testing.T) {
	e := entry{area: "mail", full: "mail.host", decl: Decl{
		Key: "host", Kind: Text, Check: func(string) error { return errors.New("that host is not allowed") },
	}}
	v := "x"
	_, problems := (&Service{}).changeOf(e, &v)
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "that host is not allowed") {
		t.Errorf("problems = %v", problems)
	}
}

func TestInternalChangeOf_SecretClear_SkipsCheck(t *testing.T) {
	calls := 0
	e := entry{area: "mail", full: "mail.password", decl: Decl{
		Key: "password", Kind: Secret,
		Check: func(string) error { calls++; return errors.New("never empty") },
	}}
	empty := ""
	for name, p := range map[string]*string{"empty string": &empty, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			ch, problems := (&Service{}).changeOf(e, p)
			if len(problems) != 0 || !ch.reset {
				t.Errorf("change = %+v, problems = %v", ch, problems)
			}
		})
	}
	if calls != 0 {
		t.Errorf("Check ran %d times for a clear", calls)
	}
}

func TestInternalFormatRatio_RoundsDown(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{4.4966, "4.49"},
		{4.4999, "4.49"},
		{4.5, "4.50"},
		{4.56, "4.56"},
		{3.0, "3.00"},
		{1.999, "1.99"},
	}
	for _, tc := range tests {
		if got := formatRatio(tc.in); got != tc.want {
			t.Errorf("formatRatio(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInternalCheckColor_FailingContrastNeverShowsTheMinimum(t *testing.T) {
	// Every grey from #000000 up is checked: a ratio under 4.5 must never print as 4.50.
	check := checkColor(lightBackground)
	for g := 0; g < 256; g++ {
		c := rgb{uint8(g), uint8(g), uint8(g)}
		err := check(c.hex())
		for _, fe := range validate.Fields(err) {
			if fe.Params["ratio"] == "4.50" {
				t.Fatalf("%s: ratio shown as 4.50 although it fails", c.hex())
			}
		}
	}
}

func TestInternalOptionsValidate_ShortSealingKey_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	person := model.NewID[model.PersonID]()
	actor := func(context.Context) (authz.Actor, bool) { return authz.Actor{Person: person}, true }
	base := Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Now()), Logger: c.Logger(), Tracer: c.Tracer("settings"),
		Audit: audittest.NewFake("settings"), Authz: authztest.NewFake(), Actor: actor,
	}
	for name, tc := range map[string]struct {
		key     string
		invalid bool
	}{
		"unset":     {"", false},
		"31 bytes":  {strings.Repeat("k", 31), true},
		"32 bytes":  {strings.Repeat("k", 32), false},
		"long":      {strings.Repeat("k", 200), false},
		"one byte":  {"k", true},
		"31 spaces": {strings.Repeat(" ", 31), true},
	} {
		t.Run(name, func(t *testing.T) {
			o := base
			o.SealingKey = secret.New(tc.key)
			_, err := New(o)
			if !tc.invalid {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			found := false
			for _, fe := range validate.Fields(err) {
				found = found || fe.Field == "settings.sealing_key"
			}
			if !found {
				t.Errorf("no field error on settings.sealing_key: %v", err)
			}
		})
	}
}

func TestInternalRegionalArea_Validates(t *testing.T) {
	a := Area{Name: regionalArea, Core: true, Decls: regionalDecls()}
	if err := a.Validate(); err != nil {
		t.Fatalf("regional area: %v", err)
	}
}

func snapAt(rev int64) *snapshot { return snapshotOf(rev, nil, nil) }

func TestInternalInstall_RefusesOlderAndReplacesAfterRegression(t *testing.T) {
	s := newTestSvc(t, 5)
	ctx := t.Context()

	if !s.install(snapAt(5)) {
		t.Fatal("the first install was refused")
	}
	if s.install(snapAt(4)) || s.install(snapAt(5)) {
		t.Error("install accepted a snapshot that is not newer")
	}
	if got := s.snap.Load().revision; got != 5 {
		t.Fatalf("revision = %d, want 5", got)
	}
	if !s.install(snapAt(6)) {
		t.Error("install refused a newer snapshot")
	}

	s.replace(ctx, snapAt(2))
	if got := s.snap.Load().revision; got != 2 {
		t.Errorf("revision after replace = %d, want 2", got)
	}
	s.replace(ctx, snapAt(1))
	if got := s.snap.Load().revision; got != 1 {
		t.Errorf("revision after a second replace = %d, want 1", got)
	}
	if n := regressionWarnings(s.cap); n != 1 {
		t.Errorf("%d regression warnings, want exactly 1", n)
	}
	if !s.install(snapAt(2)) {
		t.Error("install refused a newer snapshot after a regression")
	}
}

func TestInternalRefresh_RevisionRegression_ReplacesSnapshot(t *testing.T) {
	s := newTestSvc(t, 5)
	ctx := t.Context()
	if _, err := s.current(ctx); err != nil {
		t.Fatal(err)
	}
	if got := s.snap.Load().revision; got != 5 {
		t.Fatalf("revision = %d", got)
	}
	s.store.setRevision(2) // a restore to an earlier backup
	s.refresh(ctx)
	if got := s.snap.Load().revision; got != 2 {
		t.Errorf("revision after the poll = %d, want 2", got)
	}
	s.store.setRevision(3)
	s.refresh(ctx)
	if got := s.snap.Load().revision; got != 3 {
		t.Errorf("revision after the next poll = %d, want 3", got)
	}
	if n := regressionWarnings(s.cap); n != 1 {
		t.Errorf("%d regression warnings, want 1", n)
	}
}

func TestInternalAdopt_UnconfirmedOlderSnapshot_IsDropped(t *testing.T) {
	s := newTestSvc(t, 5)
	ctx := t.Context()
	s.install(snapAt(5))
	// The poll loaded revision 4, but the database is at 5 again: a local write moved on.
	s.adopt(ctx, snapAt(4))
	if got := s.snap.Load().revision; got != 5 {
		t.Errorf("revision = %d, want 5", got)
	}
	if n := regressionWarnings(s.cap); n != 0 {
		t.Errorf("%d regression warnings, want 0", n)
	}
}

func TestInternalSet_AfterRestore_InstallsTheWrittenSnapshot(t *testing.T) {
	area := Area{Name: "mail", Core: true, Decls: []Decl{{Key: "note", Kind: Text, Default: "hello"}}}
	s := newTestSvc(t, 5, area)
	ctx := t.Context()
	if _, err := s.current(ctx); err != nil {
		t.Fatal(err)
	}
	s.store.setRevision(2) // restored to an earlier backup; this process still holds revision 5
	v := "after the restore"
	rev, err := s.Set(ctx, Changes{Values: map[string]*string{"mail.note": &v}})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if rev != 3 {
		t.Errorf("revision = %d, want 3", rev)
	}
	if got := s.snap.Load().revision; got != 3 {
		t.Fatalf("snapshot revision = %d, want 3", got)
	}
	rd, err := s.Scoped("mail")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := rd.Text(ctx, "note"); err != nil || got != "after the restore" {
		t.Errorf("note = %q, %v", got, err)
	}
	if n := regressionWarnings(s.cap); n != 1 {
		t.Errorf("%d regression warnings, want 1", n)
	}
}
