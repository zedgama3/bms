//go:build integration

package settings_test

// D40 against a real database: F22a (a Secret saved again is a change) and F23a (the area
// Check runs inside the Write transaction over the stored rows). Values are invented.

import (
	"testing"
)

// F22a: the same plaintext saved twice leaves different ciphertext, bumps the revision both
// times, and audits both.
func TestSet_SecretResave_AlwaysChanges_Stored(t *testing.T) {
	r := newIRig(t)
	const plain = "invented-smtp-pass-5150"
	rev1 := r.mustSet(kPassword, plain)
	first := r.sealed(kPassword)
	rev2 := r.mustSet(kPassword, plain)
	second := r.sealed(kPassword)
	if rev2 <= rev1 {
		t.Errorf("revisions %d then %d: a re-save must bump", rev1, rev2)
	}
	if string(first) == string(second) {
		t.Error("a re-save left identical ciphertext")
	}
	if got := r.dbRevision(); got != int64(rev2) {
		t.Errorf("stored revision %d, want %d", got, rev2)
	}
	n := 0
	for _, e := range r.rec.Recorded() {
		if string(e.Action) == "settings.setting.changed" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d settings.setting.changed entries, want 2", n)
	}
}
