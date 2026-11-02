//go:build integration

package datarights_test

// Amendment P-W1 (Data section): the export carries name_prompted_at once a prompt has been
// recorded, and omits it before. Every person is invented.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/identity"
)

func TestExport_NamePromptedAt_AbsentBeforeAndPresentAfterAPrompt(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	p := r.insertPerson(seed{})
	_, actx := r.admin(permExport)

	read := func() (string, bool) {
		raw, err := r.svc.Export(actx, p)
		wantNoErr(t, err)
		var doc struct {
			Person map[string]json.RawMessage `json:"person"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("export is not JSON: %v", err)
		}
		if doc.Person == nil {
			t.Fatal("export has no person")
		}
		v, ok := doc.Person["name_prompted_at"]
		return string(v), ok
	}

	if v, ok := read(); ok {
		t.Fatalf("name_prompted_at = %s before any prompt, want it omitted", v)
	}

	// A session issued before now prompts for a playa name on its first read (people P-W1).
	sess := sessionOf(p, false)
	sess.IssuedAt, sess.ExpiresAt = r.clk.Now().Add(-time.Hour), r.clk.Now().Add(7*time.Hour)
	prof, err := r.ppl.Me(identity.WithSession(ctxT(t), sess))
	wantNoErr(t, err)
	if !prof.FirstPrompt {
		t.Fatal("setup: the first read did not prompt")
	}
	var want *time.Time
	wantNoErr(t, r.pool.QueryRow(ctxT(t), "SELECT name_prompted_at FROM core.person WHERE id = $1", [16]byte(p)).Scan(&want))
	if want == nil {
		t.Fatal("setup: name_prompted_at is NULL after a prompt")
	}

	v, ok := read()
	if !ok {
		t.Fatal("name_prompted_at missing from the export after a prompt")
	}
	var got time.Time
	if err := json.Unmarshal([]byte(v), &got); err != nil {
		t.Fatalf("name_prompted_at = %s is not a timestamp: %v", v, err)
	}
	if !got.Truncate(time.Microsecond).Equal(want.Truncate(time.Microsecond)) {
		t.Errorf("name_prompted_at = %v, want %v", got.UTC(), want.UTC())
	}
}
