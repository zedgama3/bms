//go:build integration

package people_test

// Amendment P-W1 (D57, D58): the once-per-session first-sign-in prompt, F91 to F101. Every
// person is invented. The prompt time is the service's clock (GREATEST(now, IssuedAt)).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // faultDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // faultDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// sessionIssued is a signed-in context for p whose session was issued at issued.
func sessionIssued(p model.PersonID, issued time.Time, super bool) context.Context {
	signedIn.Store(p, true)
	s := sessionOf(p, super, false)
	s.IssuedAt, s.ExpiresAt = issued, issued.Add(8*time.Hour)
	return identity.WithSession(context.Background(), s)
}

func (r *irig) ctxIssued(p model.PersonID, issued time.Time, super bool) context.Context {
	ctx := sessionIssued(p, issued, super)
	c, cancel := context.WithTimeout(ctx, testTimeout)
	r.t.Cleanup(cancel)
	return c
}

// promptedAt reads core.person.name_prompted_at for p (nil: NULL).
func (r *irig) promptedAt(p model.PersonID) *time.Time {
	r.t.Helper()
	var ts *time.Time
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT name_prompted_at FROM core.person WHERE id = $1", [16]byte(p)).Scan(&ts); err != nil {
		r.t.Fatalf("reading name_prompted_at: %v", err)
	}
	return ts
}

func (r *irig) updatedAt(p model.PersonID) time.Time {
	r.t.Helper()
	var ts time.Time
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT updated_at FROM core.person WHERE id = $1", [16]byte(p)).Scan(&ts); err != nil {
		r.t.Fatalf("reading updated_at: %v", err)
	}
	return ts
}

func wantPrompted(t *testing.T, r *irig, p model.PersonID, want time.Time) {
	t.Helper()
	got := r.promptedAt(p)
	if got == nil {
		t.Fatalf("name_prompted_at is NULL, want %v", want)
	}
	if !got.Truncate(time.Microsecond).Equal(want.Truncate(time.Microsecond)) {
		t.Errorf("name_prompted_at = %v, want %v", got.UTC(), want.UTC())
	}
}

func (r *irig) me(ctx context.Context) people.Profile {
	r.t.Helper()
	prof, err := r.svc.Me(ctx)
	wantNoErr(r.t, err)
	return prof
}

// unnamed inserts a person with no playa name.
func (r *irig) unnamed() model.PersonID { return r.insertPerson(seed{}) }

// ---- fault injection for the mark ------------------------------------------------------

// markFaultDB fails the statement that records the prompt (an UPDATE of name_prompted_at) with
// err, and passes everything else. With readOnly it also answers ReadOnly() true and refuses
// every Write transaction with ErrReadOnly, as a standby does.
type markFaultDB struct {
	db.DB
	err      error
	readOnly bool
}

func isMark(sql string) bool {
	s := sql
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	return len(s) >= 6 && (s[:6] == "UPDATE" || s[:6] == "update") && containsFold(sql, "name_prompted_at")
}

func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }

func (f *markFaultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.err != nil && isMark(sql) {
		return pgconn.CommandTag{}, f.err
	}
	return f.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // a test double passes errors through
}

func (f *markFaultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.err != nil && isMark(sql) {
		return nil, f.err
	}
	return f.DB.Query(ctx, sql, args...) //nolint:wrapcheck // a test double passes errors through
}

func (f *markFaultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.err != nil && isMark(sql) {
		return errRow{f.err}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *markFaultDB) ReadOnly() bool { return f.readOnly || f.DB.ReadOnly() }

func (f *markFaultDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	if f.readOnly && mode == db.Write {
		return errs.E("test.markFaultDB.Transact", errs.Unavailable, db.ErrReadOnly)
	}
	return f.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // a test double passes errors through
}

func withDB(wrap func(db.DB) db.DB) func(*people.Options) {
	return func(o *people.Options) { o.DB = wrap(o.DB) }
}

// ---- F91 to F95, F100, F101 --------------------------------------------------------------

// F91
func TestMe_Unnamed_FirstReadPrompts(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	before := r.updatedAt(p)
	issued := r.clk.Now().Add(-time.Hour)
	prof := r.me(r.ctxIssued(p, issued, false))
	if !prof.FirstPrompt {
		t.Fatal("FirstPrompt = false on the first read of a session with no name")
	}
	wantPrompted(t, r, p, r.clk.Now()) // GREATEST(now, IssuedAt) with now after IssuedAt
	if got := r.updatedAt(p); !got.Equal(before) {
		t.Errorf("updated_at moved from %v to %v: recording the prompt is not an edit", before, got)
	}
	if prof.Person.ID != p {
		t.Errorf("Person.ID = %v, want the actor", prof.Person.ID)
	}
}

// F91: nothing audited, nothing published, no permission needed beyond being signed in.
func TestMe_Prompt_NothingAudited(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	prof := r.me(r.ctxIssued(p, r.clk.Now().Add(-time.Minute), false))
	if !prof.FirstPrompt {
		t.Fatal("setup: no prompt")
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("the prompt recorded %d audit entries, want none", n)
	}
	if n := len(r.bus.Published()); n != 0 {
		t.Errorf("the prompt published %d signals, want none", n)
	}
}

// F92
func TestMe_SecondReadSameSession_NoPrompt(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	ctx := r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false)
	if !r.me(ctx).FirstPrompt {
		t.Fatal("setup: first read did not prompt")
	}
	first := *r.promptedAt(p)
	wantNoErr(t, r.clk.Advance(5*time.Minute))
	for range 3 {
		if r.me(ctx).FirstPrompt {
			t.Fatal("a later read of the same session prompted again")
		}
	}
	if got := r.promptedAt(p); got == nil || !got.Equal(first) {
		t.Errorf("name_prompted_at changed from %v to %v on a non-prompting read", first, got)
	}
}

// F93: a later session (IssuedAt after the column) prompts once more, once.
func TestMe_NewSession_PromptsAgain(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	s1 := r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false)
	if !r.me(s1).FirstPrompt {
		t.Fatal("setup: session 1 did not prompt")
	}
	wantNoErr(t, r.clk.Advance(2*time.Hour))
	s2 := r.ctxIssued(p, r.clk.Now().Add(-time.Minute), false)
	if !r.me(s2).FirstPrompt {
		t.Fatal("a new session did not prompt")
	}
	wantPrompted(t, r, p, r.clk.Now())
	if r.me(s2).FirstPrompt {
		t.Error("the new session prompted twice")
	}
	if r.me(s1).FirstPrompt {
		t.Error("the old session prompted after the new one")
	}
}

// F94
func TestMe_OlderSession_AfterPrompt_NoPrompt(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	older := r.ctxIssued(p, r.clk.Now().Add(-2*time.Hour), false)
	newer := r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false)
	if !r.me(newer).FirstPrompt {
		t.Fatal("setup: the newer session did not prompt")
	}
	if r.me(older).FirstPrompt {
		t.Error("an older session prompted after a newer one was prompted")
	}
	wantPrompted(t, r, p, r.clk.Now())
}

// F95
func TestMe_Named_NoPromptNoWrite(t *testing.T) {
	r := newIRig(t, false)
	p := r.insertPerson(seed{Playa: mPlaya})
	r.spy.reset()
	prof := r.me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false))
	if prof.FirstPrompt {
		t.Error("a named person was prompted")
	}
	if c := r.spy.counts(); c.WriteTx != 0 {
		t.Errorf("%d write transactions for Me on a named row", c.WriteTx)
	}
	if r.promptedAt(p) != nil {
		t.Error("name_prompted_at set on a named row")
	}
}

// F95
func TestMe_Superuser_NoPromptNoWrite(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	r.spy.reset()
	prof := r.me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), true))
	if prof.FirstPrompt {
		t.Error("the superuser was prompted")
	}
	if c := r.spy.counts(); c.WriteTx != 0 {
		t.Errorf("%d write transactions for the superuser's Me", c.WriteTx)
	}
	if r.promptedAt(p) != nil {
		t.Error("name_prompted_at set for the superuser")
	}
}

// F95: once a name is saved the prompt ends for good, whatever the session.
func TestMe_AfterNaming_NeverPromptsAgain(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	if !r.me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false)).FirstPrompt {
		t.Fatal("setup: no prompt")
	}
	wantNoErr(t, r.clk.Advance(time.Hour))
	ctx := r.ctxIssued(p, r.clk.Now().Add(-time.Minute), false)
	_, err := r.svc.UpdateMe(ctx, people.ProfileInput{PlayaName: str("Dusty Ember")})
	wantNoErr(t, err)
	wantNoErr(t, r.clk.Advance(time.Hour))
	if r.me(r.ctxIssued(p, r.clk.Now().Add(-time.Minute), false)).FirstPrompt {
		t.Error("a named person was prompted in a new session")
	}
}

// F100: a copy whose clock lags the one that signed the person in.
func TestMe_ClockBehindIssuedAt_StillOnce(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	issued := r.clk.Now().Add(100 * 365 * 24 * time.Hour) // far ahead of both this clock and the database's
	ctx := r.ctxIssued(p, issued, false)
	if !r.me(ctx).FirstPrompt {
		t.Fatal("no prompt")
	}
	wantPrompted(t, r, p, issued) // the column is IssuedAt, not the lagging now
	if r.me(ctx).FirstPrompt {
		t.Error("the next read prompted again: the column was written below IssuedAt")
	}
}

// F101
func TestGet_Self_NeverPrompts(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	r.grant(p, permDirectory)
	r.spy.reset()
	prof, err := r.svc.Get(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false), p)
	wantNoErr(t, err)
	if prof.FirstPrompt {
		t.Error("Get on one's own ID reported a prompt")
	}
	if c := r.spy.counts(); c.WriteTx != 0 {
		t.Errorf("%d write transactions for Get", c.WriteTx)
	}
	if r.promptedAt(p) != nil {
		t.Error("Get recorded a prompt")
	}
	// And Get does not consume a prompt Me would give.
	if !r.me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false)).FirstPrompt {
		t.Error("Me did not prompt after a Get")
	}
}

// ---- F96, F97, F98 -----------------------------------------------------------------------

// F96
func TestMe_ReadOnly_NoPromptNoError(t *testing.T) {
	r := newIRig(t, false, withDB(func(d db.DB) db.DB { return &markFaultDB{DB: d, readOnly: true} }))
	p := r.unnamed()
	prof, err := r.svc.Me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false))
	wantNoErr(t, err)
	if prof.FirstPrompt {
		t.Error("a read-only copy prompted: the person would loop")
	}
	if prof.Person.ID != p {
		t.Errorf("Person.ID = %v: the profile was not answered", prof.Person.ID)
	}
	if r.promptedAt(p) != nil {
		t.Error("a read-only copy wrote name_prompted_at")
	}
}

// F96: the mark itself meeting ErrReadOnly (a copy that learns it is read-only mid-request).
func TestMe_MarkMeetsReadOnly_NoPromptNoError(t *testing.T) {
	ro := errs.E("test.mark", errs.Unavailable, db.ErrReadOnly)
	r := newIRig(t, false, withDB(func(d db.DB) db.DB { return &markFaultDB{DB: d, err: ro} }))
	p := r.unnamed()
	prof, err := r.svc.Me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false))
	wantNoErr(t, err)
	if prof.FirstPrompt {
		t.Error("FirstPrompt true although the prompt could not be recorded")
	}
	if prof.Person.ID != p {
		t.Error("the profile was not answered")
	}
}

// F97
func TestMe_MarkFails_ReturnsStoreKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind errs.Kind
	}{
		{"unavailable", errs.Unavailable},
		{"timeout", errs.Timeout},
		{"canceled", errs.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			boom := errs.E("test.mark", tc.kind, errors.New("mark failed"))
			r := newIRig(t, false, withDB(func(d db.DB) db.DB { return &markFaultDB{DB: d, err: boom} }))
			p := r.unnamed()
			prof, err := r.svc.Me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false))
			wantKind(t, err, tc.kind)
			if errors.Is(err, db.ErrReadOnly) {
				t.Error("a non-read-only failure wraps ErrReadOnly")
			}
			if prof.FirstPrompt || !prof.Person.ID.IsZero() {
				t.Errorf("a profile came back with the error: %+v", prof)
			}
			if r.promptedAt(p) != nil {
				t.Error("name_prompted_at set although the mark failed")
			}
		})
	}
}

// F98
func TestMe_NoRow_PromptsWithoutWriting(t *testing.T) {
	r := newIRig(t, false)
	p := newPerson()
	r.spy.reset()
	prof := r.me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false))
	if !prof.FirstPrompt {
		t.Error("a person with no row yet was not prompted on a writable copy")
	}
	if c := r.spy.counts(); c.WriteTx != 0 {
		t.Errorf("%d write transactions for Me with no row", c.WriteTx)
	}
	if n := r.personRows(); n != 0 {
		t.Errorf("%d person rows after Me, want 0 (the repairing save comes with their first save)", n)
	}
	if prof.Person.ID != p {
		t.Errorf("Person.ID = %v, want the actor", prof.Person.ID)
	}
}

// F98
func TestMe_NoRow_ReadOnly_NoPrompt(t *testing.T) {
	r := newIRig(t, false, withDB(func(d db.DB) db.DB { return &markFaultDB{DB: d, readOnly: true} }))
	p := newPerson()
	prof, err := r.svc.Me(r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false))
	wantNoErr(t, err)
	if prof.FirstPrompt {
		t.Error("a read-only copy prompted a person with no row")
	}
}

// ---- F99 ---------------------------------------------------------------------------------

// F99
func TestMe_ConcurrentSameSession_OnePrompt(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	ctx := r.ctxIssued(p, r.clk.Now().Add(-time.Hour), false)
	const n = 12
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		prompts int
		failed  []error
	)
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			prof, err := r.svc.Me(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			if prof.FirstPrompt {
				prompts++
			}
		}()
	}
	close(start)
	wg.Wait()
	for _, err := range failed {
		t.Errorf("Me failed: %v", err)
	}
	if prompts != 1 {
		t.Errorf("%d of %d concurrent reads prompted, want exactly 1", prompts, n)
	}
	if got := r.personRows(); got != 1 {
		t.Errorf("%d person rows, want 1", got)
	}
	if r.promptedAt(p) == nil {
		t.Error("name_prompted_at not set")
	}
}

// Two devices of one person: the second signed-in session older than the first prompt does not
// prompt again (decision 34: several devices do not re-prompt each other).
func TestMe_TwoDevicesSameSession_PromptOnce(t *testing.T) {
	r := newIRig(t, false)
	p := r.unnamed()
	issued := r.clk.Now().Add(-time.Hour)
	phone, laptop := r.ctxIssued(p, issued, false), r.ctxIssued(p, issued, false)
	got := []bool{r.me(phone).FirstPrompt, r.me(laptop).FirstPrompt}
	if got[0] == got[1] {
		t.Errorf("prompts = %v, want exactly one device prompted", got)
	}
}
