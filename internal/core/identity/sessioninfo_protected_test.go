package identity_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/model"
)

// siGuard is a Guard that protects one account, or fails every question with err.
type siGuard struct {
	mu        sync.Mutex
	protected map[model.AccountID]bool
	err       error
	asked     int
}

func (g *siGuard) Protected(_ context.Context, a model.AccountID) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked++
	if g.err != nil {
		return false, g.err
	}
	return g.protected[a], nil
}

func (g *siGuard) questions() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.asked
}

// F110 (amendment 2026-10-30, D58).
func TestSessionInfo_GuardError_AnswersUnprotectedAndLogs(t *testing.T) {
	s, ctx := signedIn(t, false, false)
	g := &siGuard{protected: map[model.AccountID]bool{s.Account: true}, err: errDown}
	r := newRig(t, dbtest.NewFake(), func(o *identity.Options) {
		o.Guard = g
		o.Names = &nameSource{name: "Dusty Comet"}
		o.Lister = &lister{perms: []string{"hello.greet"}}
	})
	info, err := r.svc.SessionInfo(ctx())
	wantNoErr(t, err)
	if info.Protected {
		t.Error("Protected = true after a Guard error, want false")
	}
	if !info.SignedIn || info.PersonID != s.Person || info.AccountID != s.Account ||
		info.PlayaName != "Dusty Comet" || len(info.Permissions) != 1 {
		t.Errorf("the rest of the answer was affected: %+v", info)
	}
	if g.questions() != 1 {
		t.Errorf("Guard asked %d times, want 1", g.questions())
	}
	var warned bool
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelWarn {
			warned = true
		}
		for k, v := range rec.Attrs {
			if strings.Contains(strings.ToLower(k+v), "@") {
				t.Errorf("log line carries an address-like value: %s=%s", k, v)
			}
		}
	}
	if !warned {
		t.Error("no warning logged for a Guard error")
	}
	body, err := json.Marshal(info)
	wantNoErr(t, err)
	if strings.Contains(string(body), `"protected"`) {
		t.Errorf("protected present on the wire after a Guard error: %s", body)
	}
}

// F110: no warning when the Guard answers.
func TestSessionInfo_GuardAnswers_NoWarning(t *testing.T) {
	_, ctx := signedIn(t, false, false)
	r := newRig(t, dbtest.NewFake(), func(o *identity.Options) { o.Guard = &siGuard{} })
	_, err := r.svc.SessionInfo(ctx())
	wantNoErr(t, err)
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelWarn {
			t.Errorf("unexpected warning: %s", rec.Message)
		}
	}
}

// F111.
func TestSessionInfo_Protected_TrueOnlyForGuardedAccount(t *testing.T) {
	s, ctx := signedIn(t, false, false)
	wire := func(t *testing.T, info identity.SessionInfo) string {
		t.Helper()
		b, err := json.Marshal(info)
		wantNoErr(t, err)
		return string(b)
	}
	t.Run("protected", func(t *testing.T) {
		g := &siGuard{protected: map[model.AccountID]bool{s.Account: true}}
		r := newRig(t, dbtest.NewFake(), func(o *identity.Options) { o.Guard = g })
		info, err := r.svc.SessionInfo(ctx())
		wantNoErr(t, err)
		if !info.Protected {
			t.Error("Protected = false for a guarded account")
		}
		if !strings.Contains(wire(t, info), `"protected":true`) {
			t.Errorf("wire lacks protected:true: %s", wire(t, info))
		}
	})
	t.Run("not protected", func(t *testing.T) {
		g := &siGuard{protected: map[model.AccountID]bool{model.NewID[model.AccountID](): true}}
		r := newRig(t, dbtest.NewFake(), func(o *identity.Options) { o.Guard = g })
		info, err := r.svc.SessionInfo(ctx())
		wantNoErr(t, err)
		if info.Protected {
			t.Error("Protected = true for an unguarded account")
		}
		if strings.Contains(wire(t, info), "protected") {
			t.Errorf("protected not omitted: %s", wire(t, info))
		}
	})
	t.Run("nil guard", func(t *testing.T) {
		r := newRig(t, dbtest.NewFake())
		info, err := r.svc.SessionInfo(ctx())
		wantNoErr(t, err)
		if info.Protected || strings.Contains(wire(t, info), "protected") {
			t.Errorf("nil Guard answered protected: %s", wire(t, info))
		}
	})
	t.Run("signed out", func(t *testing.T) {
		g := &siGuard{protected: map[model.AccountID]bool{s.Account: true}}
		r := newRig(t, dbtest.NewFake(), func(o *identity.Options) { o.Guard = g })
		info, err := r.svc.SessionInfo(from(ctxT(t), "198.51.100.7"))
		wantNoErr(t, err)
		if info.Protected || strings.Contains(wire(t, info), "protected") {
			t.Errorf("signed-out answer protected: %s", wire(t, info))
		}
		if g.questions() != 0 {
			t.Errorf("Guard asked %d times for a signed-out call", g.questions())
		}
	})
}
