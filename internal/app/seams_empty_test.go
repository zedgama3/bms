package app

import "testing"

// TestSeamTable_IsEmpty: the boundaries work is finished (step 13), so the table in
// plan/07-abstractions.md reads exactly "None." and the real internal/app holds no
// late-bound candidate, no stored or returned capture and no stored func literal.
func TestSeamTable_IsEmpty(t *testing.T) {
	if rows := seamRows(t, seamDoc); len(rows) != 0 {
		t.Errorf("seam table lists %d rows, want None.: %v", len(rows), rows)
	}
	cands, lits := seamScan(t, ".")
	for name, why := range cands {
		t.Errorf("late-bound candidate %s: %s", name, why)
	}
	for _, l := range lits {
		t.Errorf("stored literal: %s", l)
	}
}
