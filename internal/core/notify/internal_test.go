package notify

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify/message"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ---- fakes ------------------------------------------------------------------------------

// fakeStore is a store that records the sender's calls. Methods it does not define panic
// (the embedded interface is nil), which is how a test notices an unexpected store call.
type fakeStore struct {
	store

	mu           sync.Mutex
	leaseRows    []message.Record
	leaseCalls   int
	expireCalls  int
	expireOnce   chan struct{} // closed on the first ExpireStale call, when set
	expireResult [2]int64
	countErr     error

	sent     []fenced
	failed   []fenced
	requeued []fenced
	released []fenced
	// markCtxErr is ctx.Err() as MarkSent saw it.
	markCtxErr []error
}

type fenced struct {
	id    message.ID
	lease time.Time
	token string
}

func (f *fakeStore) Lease(context.Context, time.Time, int, time.Time) ([]message.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaseCalls++
	rows := f.leaseRows
	f.leaseRows = nil
	return rows, nil
}

func (f *fakeStore) MarkSent(ctx context.Context, id message.ID, lease, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markCtxErr = append(f.markCtxErr, ctx.Err())
	f.sent = append(f.sent, fenced{id: id, lease: lease})
	return true, nil
}

func (f *fakeStore) MarkFailed(_ context.Context, id message.ID, lease time.Time, token string, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, fenced{id, lease, token})
	return true, nil
}

func (f *fakeStore) Requeue(_ context.Context, id message.ID, lease time.Time, token string, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requeued = append(f.requeued, fenced{id, lease, token})
	return true, nil
}

func (f *fakeStore) Release(_ context.Context, id message.ID, lease, _ time.Time, token string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, fenced{id, lease, token})
	return true, nil
}

func (f *fakeStore) CountOpenByStatus(context.Context) (map[message.Status]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[message.Status]int{}, f.countErr
}

func (f *fakeStore) ExpireStale(context.Context, time.Time, int) (int64, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expireCalls++
	if f.expireOnce != nil {
		select {
		case <-f.expireOnce:
		default:
			close(f.expireOnce)
		}
	}
	return f.expireResult[0], f.expireResult[1], nil
}

func (f *fakeStore) snapshot() (sent, failed, requeued, released []fenced) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fenced(nil), f.sent...), append([]fenced(nil), f.failed...),
		append([]fenced(nil), f.requeued...), append([]fenced(nil), f.released...)
}

type testChannel struct {
	mu        sync.Mutex
	fn        func(ctx context.Context, e Envelope) error
	envelopes []Envelope
}

func (c *testChannel) Send(ctx context.Context, e Envelope) error {
	c.mu.Lock()
	fn := c.fn
	c.envelopes = append(c.envelopes, e)
	c.mu.Unlock()
	if fn != nil {
		return fn(ctx, e)
	}
	return nil
}

func (*testChannel) Ping(context.Context) error { return nil }

func (c *testChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.envelopes)
}

// rigSettings is the "notify" area over the declared keys, set to a local relay that needs
// no security and a sender, so the rig is configured.
func rigSettings() *settingstest.Fake {
	f := settingstest.NewFake("notify", Settings)
	f.Set("smtp_host", "smtp.invalid")
	f.Set("smtp_port", "25")
	f.Set("smtp_security", "none")
	f.Set("sender_address", "noreply@example.org")
	return f
}

// siteSrc is Regional and Terms with fixed answers.
type siteSrc struct{}

func (siteSrc) Profile(context.Context) (regional.Profile, error) {
	return regional.Profile{Name: "Test Burn", PublicURL: "https://burn.example.org", Language: "en"}, nil
}

func (siteSrc) TermsFor(context.Context, string) (map[string]settings.Forms, error) { return nil, nil }

// addrSrc is Accounts: it answers one address or one error for everybody.
type addrSrc struct {
	email model.Email
	err   error
}

func (a addrSrc) EmailOf(context.Context, model.PersonID) (model.Email, error) {
	return a.email, a.err
}

// profSrc is Profiles for a person with no language, no name and no contact address.
type profSrc struct{}

func (profSrc) PlayaNameOf(context.Context, model.PersonID) (string, error) { return "", nil }
func (profSrc) LanguageOf(context.Context, model.PersonID) (string, error)  { return "", nil }
func (profSrc) ContactEmailOf(context.Context, model.PersonID) (model.Email, error) {
	return "", errs.E("test.ContactEmailOf", errs.NotFound, errors.New("no profile"))
}

type rig struct {
	svc *Service
	st  *fakeStore
	clk *clock.Fake
	ch  *testChannel
	cap *telemetrytest.Capture
}

func newRig(t *testing.T, mutate ...func(*Options, *fakeStore, *testChannel, *rig)) *rig {
	t.Helper()
	r := &rig{
		st:  &fakeStore{},
		clk: clock.NewFake(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)),
		ch:  &testChannel{},
		cap: telemetrytest.New(t),
	}
	o := Options{
		DB: dbtest.NewFake(), Clock: r.clk, Logger: r.cap.Logger(), Tracer: r.cap.Tracer("notify"),
		Meter: r.cap.Meter("notify"), Audit: audittest.NewFake("notify"), Authz: authztest.NewFake(),
		Actor:    func(context.Context) (authz.Actor, bool) { return authz.Actor{}, false },
		Settings: rigSettings(),
		Regional: siteSrc{}, Terms: siteSrc{}, Accounts: addrSrc{email: "someone@example.org"}, Profiles: profSrc{},
		Channel: func(SMTPConfig) (Channel, error) { return r.ch, nil }, Hostname: "bms.example.org",
		Lease: 30 * time.Second, SendTimeout: 5 * time.Second,
	}
	for _, m := range mutate {
		m(&o, r.st, r.ch, r)
	}
	svc, err := newService(o, r.st)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	r.svc = svc
	return r
}

func (r *rig) row(lease time.Duration) message.Record {
	now := r.clk.Now()
	return message.Record{
		ID: model.NewID[message.ID](), Owner: "notify", Template: "notify.test", Person: model.NewID[model.PersonID](),
		Subject: "subject", Body: "body", Attempts: 1, Status: message.Sending,
		LeasedUntil: now.Add(lease), ExpiresAt: now.Add(MaxAge),
	}
}

func ids(fs []fenced) []message.ID {
	out := make([]message.ID, len(fs))
	for i, f := range fs {
		out[i] = f.id
	}
	return out
}

// ---- tests ------------------------------------------------------------------------------

func TestSendOnce_LeaseAboutToEnd_StopsAndReleasesTheRest(t *testing.T) {
	r := newRig(t)
	rows := []message.Record{r.row(30 * time.Second), r.row(30 * time.Second), r.row(30 * time.Second)}
	r.st.leaseRows = rows
	// Each send takes 14 s of fake time: the third row would start at 28 s with a 5 s timeout
	// against a lease that ends at 30 s.
	r.ch.fn = func(context.Context, Envelope) error { return r.clk.Advance(14 * time.Second) }
	res, err := r.svc.SendOnce(context.Background())
	if err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	if res.Sent != 2 || r.ch.count() != 2 {
		t.Fatalf("sent %d, envelopes %d, want 2 and 2", res.Sent, r.ch.count())
	}
	sent, _, _, released := r.st.snapshot()
	if len(released) != 1 || released[0].id != rows[2].ID || released[0].lease != rows[2].LeasedUntil {
		t.Fatalf("released = %+v, want only the third row, fenced by its lease", released)
	}
	if got := ids(sent); got[0] != rows[0].ID || got[1] != rows[1].ID {
		t.Fatalf("sent = %v", got)
	}
}

func TestSendOnce_OutcomesAreFencedByTheLease(t *testing.T) {
	r := newRig(t)
	row := r.row(time.Minute)
	r.st.leaseRows = []message.Record{row}
	if _, err := r.svc.SendOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent, _, _, _ := r.st.snapshot()
	if len(sent) != 1 || sent[0].lease != row.LeasedUntil {
		t.Fatalf("MarkSent fence = %+v, want %v", sent, row.LeasedUntil)
	}
}

func TestSendOnce_CanceledAfterAccepted_StillMarksSent(t *testing.T) {
	r := newRig(t)
	r.st.leaseRows = []message.Record{r.row(time.Minute)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.ch.fn = func(context.Context, Envelope) error { cancel(); return nil } // accepted, then shutdown
	res, err := r.svc.SendOnce(ctx)
	if err != nil || res.Sent != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	sent, _, _, released := r.st.snapshot()
	if len(sent) != 1 || len(released) != 0 {
		t.Fatalf("sent=%d released=%d, want 1 and 0", len(sent), len(released))
	}
	if r.st.markCtxErr[0] != nil {
		t.Fatalf("MarkSent ran on a canceled context: %v", r.st.markCtxErr[0])
	}
}

func TestSendOnce_MalformedAddress_FailsFinallyAsAddress(t *testing.T) {
	r := newRig(t, func(o *Options, _ *fakeStore, _ *testChannel, _ *rig) {
		o.Accounts = addrSrc{email: "not an address"}
	})
	r.st.leaseRows = []message.Record{r.row(time.Minute)}
	res, err := r.svc.SendOnce(context.Background())
	if err != nil || res.Failed != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	_, failed, requeued, _ := r.st.snapshot()
	if len(failed) != 1 || failed[0].token != "address" || len(requeued) != 0 || r.ch.count() != 0 {
		t.Fatalf("failed=%+v requeued=%+v sends=%d", failed, requeued, r.ch.count())
	}
	var logged bool
	for _, rec := range r.cap.Records() {
		if strings.Contains(rec.Message, "failed for good") && rec.Attrs["error"] == "address" {
			logged = true
		}
	}
	if !logged {
		t.Error("the final-failure warning does not carry the error token")
	}
}

func TestSendOnce_AddressLookupFails_RequeuedAsLookup(t *testing.T) {
	r := newRig(t, func(o *Options, _ *fakeStore, _ *testChannel, _ *rig) {
		o.Accounts = addrSrc{err: errs.E("test", errs.Unavailable, errors.New("down"))}
	})
	r.st.leaseRows = []message.Record{r.row(time.Minute)}
	res, err := r.svc.SendOnce(context.Background())
	if err != nil || res.Retried != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	_, _, requeued, _ := r.st.snapshot()
	if len(requeued) != 1 || requeued[0].token != "lookup" {
		t.Fatalf("requeued = %+v", requeued)
	}
}

func TestClose_CancelsTheBatchInFlight_AndGivesTheRowBack(t *testing.T) {
	r := newRig(t)
	row := r.row(time.Minute)
	r.st.leaseRows = []message.Record{row}
	entered := make(chan struct{})
	r.ch.fn = func(ctx context.Context, _ Envelope) error {
		close(entered)
		<-ctx.Done()
		return errs.E("test.channel", errs.Canceled, ctx.Err())
	}
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(context.Background()) }()
	<-entered
	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.svc.Close(cctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	_, _, _, released := r.st.snapshot()
	if len(released) != 1 || released[0].id != row.ID {
		t.Fatalf("released = %+v", released)
	}
}

func TestRun_AfterClose_SendsNothing(t *testing.T) {
	r := newRig(t)
	r.st.leaseRows = []message.Record{r.row(time.Minute)}
	if err := r.svc.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Run(context.Background()); err != nil {
		t.Fatalf("Run after Close: %v", err)
	}
	if r.st.leaseCalls != 0 || r.st.expireCalls != 0 || r.ch.count() != 0 {
		t.Fatalf("leases=%d expiries=%d sends=%d, want none", r.st.leaseCalls, r.st.expireCalls, r.ch.count())
	}
}

func TestRun_SweepsAtStart(t *testing.T) {
	r := newRig(t)
	r.st.expireOnce = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	<-r.st.expireOnce // no clock advance: the sweep ran on the first pass
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestExpireStale_CountsOnlyNewlyExpiredRows(t *testing.T) {
	r := newRig(t)
	r.st.expireResult = [2]int64{3, 1} // three rows cleared, one of them newly failed
	n, err := r.svc.ExpireStale(context.Background())
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	var total float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.notify.delivered" && m.Attrs["outcome"] == "expired" {
			total += m.Value
		}
	}
	if total != 1 {
		t.Fatalf("delivered{expired} = %v, want 1", total)
	}
}

func TestTick_GaugeCountFails_IsLoggedOnce(t *testing.T) {
	r := newRig(t)
	r.st.countErr = errs.E("test", errs.Unavailable, errors.New("down"))
	r.svc.tick(context.Background())
	r.svc.tick(context.Background())
	warns := 0
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelWarn {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("%d warnings for two failing ticks, want 1", warns)
	}
}

func TestRegister_RangeLimits(t *testing.T) {
	cases := []struct {
		name, body string
		ok         bool
	}{
		{"integer range", "{{range 100000000}}x{{end}}", false},
		{"range over a variable", "{{$n := .Vars}}{{range $n}}x{{end}}", false},
		{"three deep", "{{range .Vars}}{{range .Vars}}{{range .Vars}}x{{end}}{{end}}{{end}}", false},
		{"range over vars", "{{range .Vars}}{{.}} {{end}}", true},
		{"two deep", "{{range .Vars}}{{range .Vars}}x{{end}}{{end}}", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			err := r.svc.Register("x", fstest.MapFS{"a.en.tmpl": {Data: []byte("Subject: Hi\n\nText: " + tc.body + "\n")}})
			if tc.ok != (err == nil) {
				t.Fatalf("Register err = %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v", errs.KindOf(err))
			}
		})
	}
}

func TestLogValue_RecordAndEnvelopeAreRedacted(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	rec := message.Record{ID: model.NewID[message.ID](), Subject: "SUBJ-MARK", Body: "BODY-MARK", Address: "ADDR-MARK@example.org"}
	env := Envelope{To: "ENV-MARK@example.org", Subject: "SUBJ-MARK", Body: "BODY-MARK"}
	log.Info("x", slog.Any("record", rec), slog.Any("envelope", env), slog.Any("public", Record{ID: rec.ID, Subject: "SUBJ-MARK"}))
	out := buf.String()
	for _, mark := range []string{"SUBJ-MARK", "BODY-MARK", "ADDR-MARK", "ENV-MARK"} {
		if strings.Contains(out, mark) {
			t.Fatalf("log carries %q: %s", mark, out)
		}
	}
	if !strings.Contains(out, rec.ID.String()) {
		t.Fatalf("log lacks the ID: %s", out)
	}
}

// TestStatus_SkippedParsesAndRoundTrips: the fifth status (D57) has a word, parses back and is
// valid; the zero status and the one after Skipped are not.
func TestStatus_SkippedParsesAndRoundTrips(t *testing.T) {
	if got := Skipped.String(); got != "skipped" {
		t.Fatalf("Skipped.String() = %q", got)
	}
	st, err := ParseStatus("skipped")
	if err != nil || st != Skipped {
		t.Fatalf("ParseStatus(skipped) = %v, %v", st, err)
	}
	if !Skipped.Valid() || Status(0).Valid() || (Skipped + 1).Valid() {
		t.Error("Valid does not span exactly Queued..Skipped")
	}
	text, err := Skipped.MarshalText()
	if err != nil || string(text) != "skipped" {
		t.Errorf("MarshalText = %q, %v", text, err)
	}
}
