package datarights

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func TestCooldown_Take_Table(t *testing.T) {
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	p := model.NewID[model.PersonID]()
	tests := []struct {
		name string
		at   time.Duration
		want bool
	}{
		{"first export", 0, true},
		{"a minute later", time.Minute, false},
		{"just before the cooldown ends", ExportCooldown - time.Nanosecond, false},
		{"when the cooldown ends", ExportCooldown, true},
	}
	c := newCooldown()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.take(p, start.Add(tc.at)); got != tc.want {
				t.Errorf("take at +%v = %v, want %v", tc.at, got, tc.want)
			}
		})
	}
}

func TestCooldown_Release_AllowsAnImmediateRetry(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	p := model.NewID[model.PersonID]()
	c := newCooldown()
	if !c.take(p, now) {
		t.Fatal("first take refused")
	}
	c.release(p, now)
	if !c.take(p, now) {
		t.Error("take after release refused")
	}
	// A later reservation is not released by an earlier one's failure.
	c.release(p, now.Add(-time.Second))
	if c.take(p, now.Add(time.Second)) {
		t.Error("a stale release removed a newer reservation")
	}
}

func TestCooldown_AtTheBound_EvictsTheOldest(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	c := newCooldown()
	first := model.NewID[model.PersonID]()
	c.take(first, now)
	for range MaxTrackedExports - 1 {
		c.take(model.NewID[model.PersonID](), now.Add(time.Second))
	}
	if len(c.last) != MaxTrackedExports {
		t.Fatalf("tracked %d, want %d", len(c.last), MaxTrackedExports)
	}
	c.take(model.NewID[model.PersonID](), now.Add(2*time.Second))
	if len(c.last) != MaxTrackedExports {
		t.Errorf("tracked %d after the bound, want %d", len(c.last), MaxTrackedExports)
	}
	if _, kept := c.last[first]; kept {
		t.Error("the oldest entry was not evicted")
	}
}

func TestWriteReport_OneLinePerPart(t *testing.T) {
	r := DeletionReport{
		Person: model.NewID[model.PersonID](), People: 4, Identity: 1, Access: 2,
		Modules: map[string]Deletion{
			"hello": {Deleted: 3},
			"alpha": {Anonymized: 1, Kept: []Kept{{Kind: "report", Count: 2}}},
		},
		Kept: []Kept{{Kind: auditEntryKind, Count: 9}},
	}
	var b bytes.Buffer
	if err := writeReport(&b, r); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	want := "person " + r.Person.String() + "\npeople 4\nidentity 1\naccess 2\n" +
		"module alpha deleted=0 anonymized=1\nmodule alpha kept report 2\nmodule hello deleted=3 anonymized=0\n" +
		"kept audit.entry 9\n"
	if got := b.String(); got != want {
		t.Errorf("report:\n%s\nwant:\n%s", got, want)
	}
}

func TestStatusOf_RoundTrips(t *testing.T) {
	for _, st := range []RequestStatus{RequestOpen, RequestWithdrawn, RequestDeclined} {
		if got := statusOf(st.String()); got != st {
			t.Errorf("statusOf(%q) = %v, want %v", st.String(), got, st)
		}
	}
	if statusOf("nonsense") != 0 {
		t.Error("an unknown word has a status")
	}
}

func TestCheckNote_Table(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		field string // "" means valid
		code  string
	}{
		{"trims", "  please remove me  ", "please remove me", "", ""},
		{"empty", "", "", "", ""},
		{"at the limit", strings.Repeat("x", MaxNoteBytes), strings.Repeat("x", MaxNoteBytes), "", ""},
		{"over the limit", strings.Repeat("x", MaxNoteBytes+1), "", "note", "too_long"},
		{"newline", "line one\nline two", "", "note", "control_chars"},
		{"bad UTF-8", "\xff", "", "note", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkNote("test", "note", tc.in)
			if tc.field == "" {
				if err != nil || got != tc.want {
					t.Fatalf("checkNote = %q, %v; want %q", got, err, tc.want)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("checkNote err = %v, want Invalid", err)
			}
			found := false
			for _, fe := range validate.Fields(err) {
				found = found || fe.Field == tc.field && fe.Code == tc.code
			}
			if !found {
				t.Errorf("fields = %v, want %s %s", validate.Fields(err), tc.field, tc.code)
			}
		})
	}
}

func TestParseRequestStatus_UnknownWord_ReturnsInvalid(t *testing.T) {
	if _, err := ParseRequestStatus("nonsense"); !errs.Is(err, errs.Invalid) {
		t.Errorf("ParseRequestStatus = %v, want Invalid", err)
	}
	if st, err := ParseRequestStatus("declined"); err != nil || st != RequestDeclined {
		t.Errorf("ParseRequestStatus(declined) = %v, %v", st, err)
	}
}
