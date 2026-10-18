package audit_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var spoolName = regexp.MustCompile(`^[0-9]+-([0-9]+)-[0-9a-f]{8}\.jsonl$`)

func TestRecord_ReadOnly_Spools(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	e := readEntry(bob)
	e.Reason = "lost key, asked at the gate"
	wantNoErr(t, f.scoped(t, "identity").Record(withSession(ctxT(t), person(alice)), e, loginEntry()))

	if n := len(f.db.Calls()); n != 0 {
		t.Errorf("a read-only copy ran %d statements", n)
	}
	files := f.spoolFiles(t)
	if len(files) != 1 {
		t.Fatalf("spool files = %v, want one active file", files)
	}
	if lines := f.spoolLines(t); len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	l := f.spooled(t)[0]
	if str(l["action"]) != "identity.account.read" || str(l["outcome"]) != "succeeded" {
		t.Errorf("action/outcome = %v/%v", l["action"], l["outcome"])
	}
	if str(l["subject"]) != bob.String() {
		t.Errorf("subject = %v", l["subject"])
	}
	rec := obj(l["record"])
	if str(rec["kind"]) != "identity.account" || str(rec["id"]) != (model.AccountID{1}).String() {
		t.Errorf("record = %v", rec)
	}
	if fields := list(l["fields"]); len(fields) != 2 || fields[0] != "email" || fields[1] != "last_login_at" {
		t.Errorf("fields = %v", l["fields"])
	}
	if str(l["level"]) != "personal" {
		t.Errorf("level = %v", l["level"])
	}
	if str(l["reason"]) != "lost key, asked at the gate" {
		t.Errorf("reason = %v", l["reason"])
	}
}

func TestRecord_ReadOnlyBeforeAttempt_DoesNotTryTheDatabase(t *testing.T) {
	// DB.ReadOnly() true before the attempt sends the batch to the spool without a statement.
	flaky := &flakyDB{}
	f := newFx(t, func(o *audit.Options) { o.DB = flaky })
	flaky.Fake = f.db
	flaky.setFailExec(errs.E("test", errs.Internal, fmt.Errorf("must not be called")))
	f.readOnly()
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	if len(f.spooled(t)) != 1 {
		t.Error("entry not spooled")
	}
}

func TestSpoolLine_Format(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	f.titles.titles[alice] = []string{"Medic"}
	f.opts.Titles = f.titles
	log := f.build(t)
	rec, err := log.Scoped("identity")
	wantNoErr(t, err)

	ctx, span := f.opts.Tracer.Start(withSession(ctxT(t), superuser(alice)), "test.request")
	defer span.End()
	full := readEntry(bob)
	full.Reason = "needed for a lost-child report"
	wantNoErr(t, rec.Record(ctx, full))
	minimal := audit.Entry{Action: "identity.login-failed", Outcome: audit.Failed, Actor: audit.Actor{Kind: audit.ActorAnonymous}}
	wantNoErr(t, rec.Record(withSession(ctxT(t), audit.Actor{}), minimal))

	lines := f.spoolLines(t)
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}

	// Key order: v, id, at, instance, trace_id, action, outcome, actor, subject, record, fields, level, reason.
	order := []string{"v", "id", "at", "instance", "trace_id", "action", "outcome", "actor", "subject", "record", "fields", "level", "reason"}
	prev := -1
	for _, k := range order {
		i := strings.Index(lines[0], `"`+k+`":`)
		if i < 0 {
			t.Fatalf("full line lacks key %q", k)
		}
		if i < prev {
			t.Errorf("key %q is out of order in the spool line", k)
		}
		prev = i
	}
	if !strings.HasPrefix(lines[0], `{"v":1,"id":"`) {
		t.Errorf("a line must start with {\"v\":1,\"id\":...")
	}
	if a := obj(f.spooled(t)[0]["actor"]); a["superuser"] != true || str(a["person"]) != alice.String() || str(a["kind"]) != "person" {
		t.Errorf("actor = %v", a)
	} else if ts := list(a["titles"]); len(ts) != 1 || ts[0] != "Medic" {
		t.Errorf("actor.titles = %v", a["titles"])
	}

	// A minimal line omits every empty optional key (trace_id, subject, record, fields, level, reason).
	m := f.spooled(t)[1]
	for _, k := range []string{"trace_id", "subject", "record", "fields", "level", "reason"} {
		if _, ok := m[k]; ok {
			t.Errorf("minimal line carries %q", k)
		}
	}
	a := obj(m["actor"])
	if len(a) != 1 || str(a["kind"]) != "anonymous" {
		t.Errorf("minimal actor = %v, want only kind", a)
	}
	if str(m["outcome"]) != "failed" {
		t.Errorf("outcome = %v", m["outcome"])
	}
	for _, l := range lines {
		if strings.Contains(l, "\n") {
			t.Error("a line holds a newline")
		}
	}
}

func TestSpool_FileModeNameAndAppendOnly(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	rec := f.scoped(t, "identity")
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))
	files := f.spoolFiles(t)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	m := spoolName.FindStringSubmatch(filepath.Base(files[0]))
	if m == nil {
		t.Fatalf("file name %q does not match <unix-ms>-<pid>-<4 hex bytes>.jsonl", filepath.Base(files[0]))
	}
	if m[1] != fmt.Sprint(os.Getpid()) {
		t.Errorf("pid in name = %s, want %d", m[1], os.Getpid())
	}
	st, err := os.Stat(files[0])
	wantNoErr(t, err)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", st.Mode().Perm())
	}
	dst, err := os.Stat(f.spoolDir())
	wantNoErr(t, err)
	if dst.Mode().Perm() != 0o700 {
		t.Errorf("spool dir mode = %v, want 0700", dst.Mode().Perm())
	}
	// Later calls append to the same file, and each is on disk when Record returns.
	before, err := os.ReadFile(files[0])
	wantNoErr(t, err)
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))
	after, err := os.ReadFile(files[0])
	wantNoErr(t, err)
	if !strings.HasPrefix(string(after), string(before)) || len(after) <= len(before) {
		t.Error("the active file was not appended to")
	}
	if got := f.spoolFiles(t); len(got) != 1 {
		t.Errorf("a second Record opened another file: %v", got)
	}
}

func TestSpool_TwoLogsOnOneDir_NeverClash(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	other := f.build(t)
	r2, err := other.Scoped("identity")
	wantNoErr(t, err)
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	wantNoErr(t, r2.Record(ctxT(t), loginEntry()))
	if got := f.spoolFiles(t); len(got) != 2 {
		t.Errorf("files = %v, want one per Log (O_EXCL names never clash)", got)
	}
}

func TestSpool_RotatesOnImportOnce_AndOnClose(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	rec := f.scoped(t, "identity")
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))
	first := f.spoolFiles(t)
	// The copy can write again: ImportOnce rotates and imports this process's own spool.
	f.db.SetReadOnly(false)
	got, err := f.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 1 {
		t.Errorf("Files = %d, want 1 (the rotated active file)", got.Files)
	}
	if left := f.spoolFiles(t); len(left) != 0 {
		t.Errorf("spool after import = %v", left)
	}
	// Back to read-only: the next write opens a new file, not the old name.
	f.readOnly()
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))
	second := f.spoolFiles(t)
	if len(second) != 1 || second[0] == first[0] {
		t.Errorf("after rotation files = %v (old %v)", second, first)
	}
	// Close syncs and closes; the file is complete and importable by another process.
	wantNoErr(t, f.log.Close(ctxT(t)))
	if lines := f.spoolLines(t); len(lines) != 1 {
		t.Errorf("lines after Close = %d", len(lines))
	}
	g := newFx(t)
	g.dir = f.dir
	g.opts.DataDir = f.dir
	g.log = g.build(t)
	got, err = g.log.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 1 || len(g.spoolFiles(t)) != 0 {
		t.Errorf("another process did not import the closed file: %+v", got)
	}
}

func TestRecord_ReadOnly_SpoolUnwritable_ReturnsUnavailable(t *testing.T) {
	t.Run("data dir is a file", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		wantNoErr(t, os.WriteFile(blocker, []byte("x"), 0o600))
		f := newFx(t, func(o *audit.Options) { o.DataDir = blocker })
		f.readOnly()
		err := f.scoped(t, "identity").Record(ctxT(t), loginEntry())
		wantKind(t, err, errs.Unavailable)
	})
	t.Run("spool dir is read-only", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		f := newFx(t)
		wantNoErr(t, os.MkdirAll(f.spoolDir(), 0o700))
		wantNoErr(t, os.Chmod(f.spoolDir(), 0o500)) //nolint:gosec // the test makes the directory read-only on purpose
		t.Cleanup(func() {
			if err := os.Chmod(f.spoolDir(), 0o700); err != nil { //nolint:gosec // restores the mode so TempDir can clean up
				t.Logf("restoring the directory mode: %v", err)
			}
		})
		f.readOnly()
		err := f.scoped(t, "identity").Record(ctxT(t), loginEntry())
		wantKind(t, err, errs.Unavailable)
	})
	t.Run("fails every time, writes nothing", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		wantNoErr(t, os.WriteFile(blocker, []byte("x"), 0o600))
		f := newFx(t, func(o *audit.Options) { o.DataDir = blocker })
		f.readOnly()
		rec := f.scoped(t, "identity")
		for range 3 {
			wantKind(t, rec.Record(ctxT(t), loginEntry()), errs.Unavailable)
		}
		if n := len(f.db.Calls()); n != 0 {
			t.Errorf("statements = %d", n)
		}
	})
}

func TestClose_CtxDone_ReturnsTimeout_FileKept(t *testing.T) {
	f := newFx(t)
	f.readOnly()
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := f.log.Close(ctx)
	if err == nil || (!errs.Is(err, errs.Canceled) && !errs.Is(err, errs.Timeout)) {
		t.Fatalf("Close with a done ctx: kind %v, want Canceled or Timeout", errs.KindOf(err))
	}
	// Whatever happened, the spooled entry is whole and importable.
	if lines := f.spoolLines(t); len(lines) != 1 {
		t.Fatalf("lines = %d, want the one recorded", len(lines))
	}
	f.db.SetReadOnly(false)
	other := f.build(t)
	got, err := other.ImportOnce(ctxT(t))
	wantNoErr(t, err)
	if got.Files != 1 {
		t.Errorf("Files = %d, want 1", got.Files)
	}
}

func TestSpool_NoPersonalDataInTelemetry(t *testing.T) {
	f := newFx(t)
	f.titles.titles[alice] = []string{marker + "title"}
	f.opts.Titles = f.titles
	f.readOnly()
	log := f.build(t)
	rec, err := log.Scoped("identity")
	wantNoErr(t, err)
	e := readEntry(bob)
	e.Fields = []string{"zq9marker7field"}
	e.Reason = "why " + marker
	wantNoErr(t, rec.Record(withSession(ctxT(t), person(alice)), e))

	for _, needle := range []string{marker, "zq9marker7field", alice.String(), bob.String(), f.dir} {
		if f.cap.Contains(needle) {
			t.Errorf("telemetry carries %q", needle)
		}
	}
	// The one warning per active file names the file, not the path.
	var warns int
	for _, r := range f.cap.Records() {
		if r.Message == "audit spooling started" {
			warns++
			if file := r.Attrs["file"]; !spoolName.MatchString(file) {
				t.Errorf("warn file attr = %q, want the file name only", file)
			}
		}
	}
	if warns != 1 {
		t.Errorf("spooling warnings = %d, want 1", warns)
	}
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))
	warns = 0
	for _, r := range f.cap.Records() {
		if r.Message == "audit spooling started" {
			warns++
		}
	}
	if warns != 1 {
		t.Errorf("a second Record in the same file warned again (%d)", warns)
	}
}

func TestRecord_Telemetry_SpansAndMetrics(t *testing.T) {
	f := newFx(t)
	rec := f.scoped(t, "identity")
	wantNoErr(t, rec.Record(ctxT(t), loginEntry(), loginEntry()))
	f.readOnly()
	wantNoErr(t, rec.Record(ctxT(t), loginEntry()))

	var vias []string
	for _, s := range f.cap.Spans() {
		if s.Name != "audit.record" {
			continue
		}
		vias = append(vias, s.Attrs["via"])
		if s.Attrs["area"] != "identity" {
			t.Errorf("span area = %q", s.Attrs["area"])
		}
		if s.Attrs["entries"] == "" {
			t.Error("span lacks the entries count")
		}
	}
	if len(vias) != 2 || vias[0] != "db" || vias[1] != "spool" {
		t.Errorf("audit.record spans via = %v, want [db spool]", vias)
	}
	got := map[string]float64{}
	for _, m := range f.cap.Measurements() {
		if m.Instrument == "bms.audit.recorded" {
			got[m.Attrs["via"]] += m.Value
		}
	}
	if got["db"] < 1 || got["spool"] < 1 {
		t.Errorf("bms.audit.recorded by via = %v", got)
	}

	// A failure counts bms.audit.record_failures{kind}.
	bad := loginEntry()
	bad.Outcome = 0
	wantKind(t, rec.Record(ctxT(t), bad), errs.Invalid)
	var failures int
	for _, m := range f.cap.Measurements() {
		if m.Instrument == "bms.audit.record_failures" && m.Attrs["kind"] == "invalid" {
			failures++
		}
	}
	if failures != 1 {
		t.Errorf("record_failures{kind=invalid} = %d, want 1", failures)
	}
}

func TestSpoolFilesGauge_ObservesTheDirectory(t *testing.T) {
	f := newFx(t)
	gauge := func() (float64, bool) {
		t.Helper()
		wantNoErr(t, f.cap.Telemetry().Collect(ctxT(t)))
		ms := f.cap.Measurements()
		for i := len(ms) - 1; i >= 0; i-- {
			if ms[i].Instrument == "bms.audit.spool_files" {
				return ms[i].Value, true
			}
		}
		return 0, false
	}
	if v, ok := gauge(); !ok || v != 0 {
		t.Errorf("gauge with no spool directory = %v (seen %v), want 0", v, ok)
	}
	f.readOnly()
	wantNoErr(t, f.scoped(t, "identity").Record(ctxT(t), loginEntry()))
	if v, _ := gauge(); v != 1 {
		t.Errorf("gauge with one file = %v, want 1", v)
	}
}
