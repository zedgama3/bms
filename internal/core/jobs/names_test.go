package jobs_test

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/retry"
	"github.com/zedgama3/bms/internal/model"
)

// ---- F1 ----------------------------------------------------------------------------------

func TestName_Validate_BadGrammar_ReturnsInvalid(t *testing.T) {
	long96 := strings.Repeat("a", 31) + "." + strings.Repeat("b", 64) // the longest the owner and ident grammars allow
	tests := []struct {
		name string
		in   string
		code string // "" = any code; "ok" = valid
	}{
		{"owner and ident", "volunteers.remind", "ok"},
		{"hyphenated ident", "people.retention-unused", "ok"},
		{"digits after a letter", "a1.b2", "ok"},
		{"96 bytes", long96, "ok"},
		{"empty", "", "required"},
		{"97 bytes", "a" + long96, "too_long"},
		{"ident of 65 bytes", "a." + strings.Repeat("b", 65), ""},
		{"no dot", "volunteers", "malformed"},
		{"upper-case owner", "Volunteers.remind", "malformed"},
		{"upper-case ident", "volunteers.Remind", "malformed"},
		{"three parts", "a.b.c", "malformed"},
		{"empty owner", ".remind", "malformed"},
		{"empty ident", "volunteers.", "malformed"},
		{"ident leading hyphen", "a.-b", "malformed"},
		{"ident trailing hyphen", "a.b-", "malformed"},
		{"ident double hyphen", "a.b--c", "malformed"},
		{"ident starts with a digit", "a.1b", "malformed"},
		{"space", "a.b c", "malformed"},
		{"underscore", "a.b_c", "malformed"},
		{"non-ascii", "a.bé", "malformed"},
		{"newline", "a.b\n", "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := jobs.Name(tc.in).Validate()
			if tc.code == "ok" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "name", "")
			if tc.code != "" && !hasField(err, "name", tc.code) && tc.code != "malformed" {
				t.Fatalf("want code %q, got %v", tc.code, describe(err))
			}
		})
	}
}

func TestName_Owner_ReturnsPartBeforeFirstDot(t *testing.T) {
	tests := []struct{ in, want string }{
		{"volunteers.remind", "volunteers"},
		{"jobs.purge", "jobs"},
		{"", ""},
		{"nodot", ""},
		{"Bad.name", ""},
		{"a.b.c", ""},
	}
	for _, tc := range tests {
		if got := jobs.Name(tc.in).Owner(); got != tc.want {
			t.Errorf("Name(%q).Owner() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---- Status ------------------------------------------------------------------------------

func TestStatus_StringAndParse_RoundTrip(t *testing.T) {
	want := map[jobs.Status]string{
		jobs.Queued: "queued", jobs.Running: "running", jobs.Done: "done",
		jobs.Failed: "failed", jobs.Canceled: "canceled",
	}
	for s, w := range want {
		if s.String() != w {
			t.Errorf("%d.String() = %q, want %q", s, s.String(), w)
		}
		got, err := jobs.ParseStatus(w)
		wantNoErr(t, err)
		if got != s {
			t.Errorf("ParseStatus(%q) = %v, want %v", w, got, s)
		}
	}
	if jobs.Status(0).String() == "queued" {
		t.Error("the zero Status must not be a valid word")
	}
}

func TestParseStatus_Unknown_ReturnsInvalid(t *testing.T) {
	for _, in := range []string{"", "QUEUED", "queued ", "finished", "Status(0)"} {
		if _, err := jobs.ParseStatus(in); err == nil || !errs.Is(err, errs.Invalid) {
			t.Errorf("ParseStatus(%q) err = %v, want Invalid", in, err)
		}
	}
}

// ---- F2 ----------------------------------------------------------------------------------

func TestDecl_Validate_EveryOutOfRange_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		every time.Duration
		ok    bool
	}{
		{"on demand", 0, true},
		{"minimum", jobs.MinEvery, true},
		{"maximum", jobs.MaxEvery, true},
		{"below minimum", jobs.MinEvery - time.Nanosecond, false},
		{"above maximum", jobs.MaxEvery + time.Nanosecond, false},
		{"negative", -time.Minute, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := jobs.Decl{Name: "volunteers.sweep", Every: tc.every, Handler: nop}.Validate()
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "every", "range")
		})
	}
}

func TestDecl_Validate_TimeoutOutOfRange_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		ok   bool
	}{
		{"default", 0, true},
		{"minimum", time.Second, true},
		{"maximum", jobs.MaxRunTimeout, true},
		{"below minimum", time.Second - time.Nanosecond, false},
		{"above maximum", jobs.MaxRunTimeout + time.Nanosecond, false},
		{"negative", -time.Second, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := jobs.Decl{Name: "volunteers.sweep", Timeout: tc.d, Handler: nop}.Validate()
			if tc.ok {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, "timeout", "range")
		})
	}
}

func TestDecl_Validate_NilHandler_ReturnsInvalid(t *testing.T) {
	wantField(t, jobs.Decl{Name: "volunteers.sweep"}.Validate(), "handler", "required")
}

func TestDecl_Validate_BadName_ReturnsInvalid(t *testing.T) {
	wantField(t, jobs.Decl{Name: "Bad", Handler: nop}.Validate(), "name", "")
}

func TestDecl_Validate_EveryProblem_IsListed(t *testing.T) {
	err := jobs.Decl{Name: "", Every: time.Second, Timeout: time.Hour * 2}.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"name", "every", "timeout", "handler"} {
		if !hasField(err, f, "") {
			t.Errorf("field %q missing from %v", f, describe(err))
		}
	}
}

// ---- F3 ----------------------------------------------------------------------------------

func TestRequest_Validate_KeyTooLong_ReturnsInvalid(t *testing.T) {
	ok := jobs.Request{Name: "volunteers.remind", Key: strings.Repeat("k", jobs.MaxKeyBytes)}
	wantNoErr(t, ok.Validate())
	bad := jobs.Request{Name: "volunteers.remind", Key: strings.Repeat("k", jobs.MaxKeyBytes+1)}
	wantField(t, bad.Validate(), "key", "too_long")
}

func TestRequest_Validate_KeyMalformed_ReturnsInvalid(t *testing.T) {
	for _, k := range []string{"has space", "slash/key", "é", "new\nline", "semi;colon", "@at"} {
		wantField(t, jobs.Request{Name: "volunteers.remind", Key: k}.Validate(), "key", "malformed")
	}
	for _, k := range []string{"", "A-Za-z0-9._:-", "2026-10-19T12:00:00Z", "admin:abc", "sig:people"} {
		wantNoErr(t, jobs.Request{Name: "volunteers.remind", Key: k}.Validate())
	}
}

func TestRequest_Validate_RunAtTooFar_ReturnsInvalid(t *testing.T) {
	now := time.Now()
	wantNoErr(t, jobs.Request{Name: "volunteers.remind"}.Validate()) // zero = now
	wantNoErr(t, jobs.Request{Name: "volunteers.remind", RunAt: now.Add(-48 * time.Hour)}.Validate())
	wantNoErr(t, jobs.Request{Name: "volunteers.remind", RunAt: now.Add(jobs.MaxDelay - 24*time.Hour)}.Validate())
	wantField(t, jobs.Request{Name: "volunteers.remind", RunAt: now.Add(jobs.MaxDelay + 24*time.Hour)}.Validate(), "run_at", "range")
}

func TestRequest_Validate_BadName_ReturnsInvalid(t *testing.T) {
	wantField(t, jobs.Request{Name: "nope"}.Validate(), "name", "")
}

func TestRequest_Validate_Args_AreChecked(t *testing.T) {
	refs := func(n int) jobs.Args {
		p := signals.Payload{}
		for i := range n {
			p["r"+strings.Repeat("a", i+1)] = signals.RecordID(model.NewID[model.PersonID]())
		}
		return p
	}
	wantNoErr(t, jobs.Request{Name: "volunteers.remind", Args: nil}.Validate())
	wantNoErr(t, jobs.Request{Name: "volunteers.remind", Args: jobs.Args{}}.Validate())
	wantNoErr(t, jobs.Request{Name: "volunteers.remind", Args: refs(signals.MaxRefs)}.Validate())
	wantKind(t, jobs.Request{Name: "volunteers.remind", Args: refs(signals.MaxRefs + 1)}.Validate(), errs.Invalid)
	zero := jobs.Args{"person": signals.RecordID{}}
	wantKind(t, jobs.Request{Name: "volunteers.remind", Args: zero}.Validate(), errs.Invalid)
	badKey := jobs.Args{"Bad Key": signals.RecordID(newPerson())}
	wantKind(t, jobs.Request{Name: "volunteers.remind", Args: badKey}.Validate(), errs.Invalid)
}

// ---- constants, permissions, Record ------------------------------------------------------

func TestConstants_MatchSpec(t *testing.T) {
	checks := []struct {
		name      string
		got, want any
	}{
		{"MinEvery", jobs.MinEvery, time.Minute},
		{"MaxEvery", jobs.MaxEvery, 168 * time.Hour},
		{"MaxRunTimeout", jobs.MaxRunTimeout, time.Hour},
		{"MaxDelay", jobs.MaxDelay, 366 * 24 * time.Hour},
		{"MaxKeyBytes", jobs.MaxKeyBytes, 128},
		{"DefaultPollInterval", jobs.DefaultPollInterval, 10 * time.Second},
		{"DefaultLease", jobs.DefaultLease, 2 * time.Minute},
		{"DefaultRunTimeout", jobs.DefaultRunTimeout, 5 * time.Minute},
		{"DefaultWorkers", jobs.DefaultWorkers, 2},
		{"MaxWorkers", jobs.MaxWorkers, 32},
		{"DoneTTL", jobs.DoneTTL, 7 * 24 * time.Hour},
		{"MaxRecent", jobs.MaxRecent, 50},
		{"MaxBatch", jobs.MaxBatch, 1000},
		{"NameRedeliver", jobs.NameRedeliver, jobs.Name("signals.redeliver")},
		{"NamePurge", jobs.NamePurge, jobs.Name("jobs.purge")},
		{"PermissionRead", jobs.PermissionRead, "jobs.read"},
		{"PermissionManage", jobs.PermissionManage, "jobs.manage"},
		{"DefaultRetry", jobs.DefaultRetry, retry.Policy{Initial: time.Minute, Max: time.Hour, Multiplier: 2, MaxAttempts: 10}},
	}
	for _, c := range checks {
		if fmt.Sprintf("%T:%v", c.got, c.got) != fmt.Sprintf("%T:%v", c.want, c.want) && fmt.Sprint(c.got) != fmt.Sprint(c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	wantNoErr(t, jobs.NamePurge.Validate())
	wantNoErr(t, jobs.NameRedeliver.Validate())
	wantNoErr(t, jobs.DefaultRetry.Validate())
}

func TestPermissions_DeclaredAsSpecified(t *testing.T) {
	want := map[authz.Permission]bool{permRead: false, permManage: false}
	for _, p := range jobs.Permissions {
		if _, ok := want[p.Name]; !ok {
			t.Errorf("unexpected permission %q", p.Name)
			continue
		}
		want[p.Name] = true
		wantNoErr(t, p.Validate())
		if p.Scope != authz.ScopeSite {
			t.Errorf("%s scope = %v, want site", p.Name, p.Scope)
		}
		if p.Reaches != model.SensitivityInternal {
			t.Errorf("%s reaches %v, want internal", p.Name, p.Reaches)
		}
		switch p.Name {
		case permRead:
			if len(p.StarterRoles) != len(authz.SiteAdminRoles) {
				t.Errorf("jobs.read starter roles = %v, want %v", p.StarterRoles, authz.SiteAdminRoles)
			}
		case permManage:
			if len(p.StarterRoles) != 1 || p.StarterRoles[0] != "settings-admin" {
				t.Errorf("jobs.manage starter roles = %v, want [settings-admin]", p.StarterRoles)
			}
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("permission %q is not declared", p)
		}
	}
}

func TestRecord_LogValue_EmitsIDAndNameOnly(t *testing.T) {
	id := model.NewID[jobs.JobID]()
	rec := jobs.Record{
		ID: id, Name: "volunteers.remind", Key: "secret-looking-key", Status: jobs.Failed,
		Attempts: 3, LastError: "invalid", Instance: "test-a", TraceID: strings.Repeat("a", 32),
	}
	v := rec.LogValue()
	if v.Kind() != slog.KindGroup {
		t.Fatalf("LogValue kind = %v, want group", v.Kind())
	}
	var keys []string
	for _, a := range v.Group() {
		keys = append(keys, a.Key)
		if strings.Contains(a.Value.String(), "secret-looking-key") {
			t.Errorf("attribute %q carries the key", a.Key)
		}
	}
	if len(keys) != 2 {
		t.Errorf("LogValue attributes = %v, want id and name only", keys)
	}
}
