//go:build integration

package regional_test

// The logo paths that commit (settings.md, amendment 2026-10-21): F37 to F54 that need a
// real database, and the contract lines about the snapshot and the ETag. Two kinds of rig:
// one over filestest.Fake (fast, cannot roll back), one over the real files.Service on the
// same throw-away database and an in-memory object store (rolls back; proves the discard).

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// ---- happy paths and the contract lines --------------------------------------------------

func TestSetLogo_AcceptedKinds_AttachAuditAndBump(t *testing.T) {
	tests := []struct {
		name, file, ctype string
		data              func(*testing.T) []byte
	}{
		{"png", "logo.png", "image/png", func(t *testing.T) []byte { return pngOf(t, 5) }},
		{"jpeg", "logo.jpg", "image/jpeg", func(t *testing.T) []byte { return jpegOf(t) }},
		{"webp", "logo.webp", "image/webp", func(*testing.T) []byte { return webpOf() }},
		{"png with a spoofed .svg name", "logo.svg", "image/png", func(t *testing.T) []byte { return pngOf(t, 6) }},
		{"png with a spoofed .jpg name", "logo.jpg", "image/png", func(t *testing.T) []byte { return pngOf(t, 7) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRealLogoI(t, files.DefaultPolicy())
			before := r.dbRevision()
			res := r.mustSetLogo(tc.file, tc.data(t))

			f := r.onlyFile()
			if res.Logo != f.ID.String() {
				t.Errorf("LogoResult.Logo = %q, want %q", res.Logo, f.ID)
			}
			if int64(res.Revision) != before+1 || r.dbRevision() != before+1 {
				t.Errorf("revision = %d (db %d), want %d", res.Revision, r.dbRevision(), before+1)
			}
			if f.Record != regional.LogoRef || f.Level != model.SensitivityPublic || !f.Subject.IsZero() {
				t.Errorf("file record = %+v level %v subject %v", f.Record, f.Level, f.Subject)
			}
			if f.ContentType != tc.ctype {
				t.Errorf("ContentType = %q, want the sniffed %q (never the extension)", f.ContentType, tc.ctype)
			}
			ch := entriesWith(r.rec.Recorded(), "regional.logo.changed")
			if len(ch) != 1 {
				t.Fatalf("%d regional.logo.changed entries, want 1 (%v)", len(ch), actionsOf(r.rec.Recorded()))
			}
			e := ch[0]
			if e.Record != regional.LogoRef || len(e.Fields) != 1 || e.Fields[0] != "logo" || e.Reason != "file:"+f.ID.String() {
				t.Errorf("entry = record %v fields %v reason %q", e.Record, e.Fields, e.Reason)
			}
			if got := r.publicLogo(); got != f.ID.String() {
				t.Errorf("Public.Logo = %q, want %q", got, f.ID)
			}
		})
	}
}

func TestSetLogo_ExactlyMaxLogoBytes_IsAccepted(t *testing.T) {
	r := newLogoI(t)
	res, err := r.set("logo.png", padded(pngOf(t, 1), int(regional.MaxLogoBytes)))
	wantNoErr(t, err)
	if res.Logo == "" {
		t.Error("no logo ID for a file of exactly MaxLogoBytes")
	}
}

func TestSetLogo_TextPartsBeforeTheFile_AreIgnored(t *testing.T) {
	r := newLogoI(t)
	body, ct := multipartOf(t, [][2]string{{"note", "ignored"}, {"other", "also ignored"}}, "logo.png", "image/png", pngOf(t, 1))
	w, f := r.send(r.actx(), http.MethodPut, "/logo", body, ct)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%v)", w.Code, f.last())
	}
	var res regional.LogoResult
	wantNoErr(t, json.Unmarshal(w.Body.Bytes(), &res))
	if res.Logo == "" {
		t.Error("PUT answered no logo ID")
	}
}

func TestSetLogo_Superuser_Succeeds(t *testing.T) {
	r := newLogoI(t)
	ctx := asSuper(context.Background(), newPerson())
	_, err := r.svc.SetLogo(ctx, "logo.png", bytes.NewReader(pngOf(t, 1)))
	wantNoErr(t, err)
}

func TestSetLogo_Replace_LeavesOneRow(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	first := r.mustSetLogo("a.png", pngOf(t, 1))
	second := r.mustSetLogo("b.png", pngOf(t, 2))
	if first.Logo == second.Logo {
		t.Fatal("a replacement kept the old ID")
	}
	if int64(second.Revision) != int64(first.Revision)+1 {
		t.Errorf("revision %d then %d", first.Revision, second.Revision)
	}
	if r.fileRows() != 1 || r.onlyFile().ID.String() != second.Logo {
		t.Errorf("rows = %d, remaining %s, want one row %s", r.fileRows(), r.onlyFile().ID, second.Logo)
	}
	if got := r.publicLogo(); got != second.Logo {
		t.Errorf("Public.Logo = %q, want %q", got, second.Logo)
	}
	if n := len(entriesWith(r.rec.Recorded(), "regional.logo.changed")); n != 2 {
		t.Errorf("%d regional.logo.changed entries, want 2", n)
	}
	// The first object goes once the sweep has run.
	_, err := r.fsvc.Reap(ctxT(t))
	wantNoErr(t, err)
	if keys := r.mem.Keys(objects.Bucket("settings")); len(keys) != 1 {
		t.Errorf("%d objects in the settings bucket after Reap, want 1", len(keys))
	}
}

func TestPublic_ETagAndLogoMoveTogether(t *testing.T) {
	r := newLogoI(t)
	_, etag0, logo0 := r.publicETag("")
	if logo0 != "" {
		t.Fatalf("logo before any upload = %q", logo0)
	}
	if code, _, _ := r.publicETag(etag0); code != http.StatusNotModified {
		t.Fatalf("unchanged revision: status %d, want 304", code)
	}

	res := r.mustSetLogo("logo.png", pngOf(t, 1))
	code, etag1, logo1 := r.publicETag(etag0) // the shell's refetch with its old tag
	if code != http.StatusOK {
		t.Fatalf("after SetLogo the old ETag answered %d, want 200", code)
	}
	if etag1 == etag0 || logo1 != res.Logo {
		t.Errorf("etag %q -> %q, logo %q (want %q)", etag0, etag1, logo1, res.Logo)
	}

	_, err := r.svc.RemoveLogo(r.actx())
	wantNoErr(t, err)
	code, etag2, logo2 := r.publicETag(etag1)
	if code != http.StatusOK || etag2 == etag1 || etag2 == etag0 || logo2 != "" {
		t.Errorf("after RemoveLogo: status %d etag %q (was %q, %q) logo %q", code, etag2, etag1, etag0, logo2)
	}
}

// ---- F50: RemoveLogo ---------------------------------------------------------------------

func TestRemoveLogo_RemovesAuditsAndBumps(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	set := r.mustSetLogo("logo.png", pngOf(t, 1))
	before := r.dbRevision()

	res, err := r.svc.RemoveLogo(r.actx())
	wantNoErr(t, err)
	if res.Logo != "" || int64(res.Revision) != before+1 || r.dbRevision() != before+1 {
		t.Errorf("result = %+v, db revision %d, want logo \"\" revision %d", res, r.dbRevision(), before+1)
	}
	if r.fileRows() != 0 || r.publicLogo() != "" {
		t.Errorf("rows %d, Public.Logo %q after removal", r.fileRows(), r.publicLogo())
	}
	rm := entriesWith(r.rec.Recorded(), "regional.logo.removed")
	if len(rm) != 1 || rm[0].Record != regional.LogoRef || rm[0].Reason != "file:"+set.Logo ||
		len(rm[0].Fields) != 1 || rm[0].Fields[0] != "logo" {
		t.Errorf("removed entries = %+v", rm)
	}
	_, err = r.fsvc.Reap(ctxT(t))
	wantNoErr(t, err)
	if keys := r.mem.Keys(objects.Bucket("settings")); len(keys) != 0 {
		t.Errorf("%d objects left after Reap", len(keys))
	}
}

func TestRemoveLogo_None_IsNoop(t *testing.T) {
	r := newLogoI(t)
	r.mustSet("regional.name", "Camp Quill") // some revision above the initial one
	before, entriesBefore := r.dbRevision(), len(r.rec.Recorded())
	for i := range 2 { // idempotent
		res, err := r.svc.RemoveLogo(r.actx())
		wantNoErr(t, err)
		if res.Logo != "" || int64(res.Revision) != before {
			t.Errorf("call %d: result %+v, want the current revision %d and no logo", i, res, before)
		}
	}
	if r.dbRevision() != before {
		t.Errorf("revision moved from %d to %d on a no-op", before, r.dbRevision())
	}
	if len(r.rec.Recorded()) != entriesBefore {
		t.Errorf("a no-op recorded %v", actionsOf(r.rec.Recorded()[entriesBefore:]))
	}
}

// ---- F47: the audit record fails ---------------------------------------------------------

func TestSetLogo_AuditFails_RollsBackAndDiscards(t *testing.T) {
	boom := errs.E("test.audit", errs.Unavailable, errors.New("audit store is down"))

	t.Run("no earlier logo", func(t *testing.T) {
		r := newRealLogoI(t, files.DefaultPolicy())
		before := r.dbRevision()
		r.rec.Fail(boom)
		_, err := r.set("logo.png", pngOf(t, 1))
		wantKind(t, err, errs.Unavailable)
		r.rec.Fail(nil)
		if r.fileRows() != 0 {
			t.Errorf("%d file rows after a rolled-back upload", r.fileRows())
		}
		if keys := r.mem.Keys(objects.Bucket("settings")); len(keys) != 0 {
			t.Errorf("%d objects left in the bucket: the staged file was not discarded", len(keys))
		}
		if r.dbRevision() != before || r.publicLogo() != "" {
			t.Errorf("revision %d (was %d), Public.Logo %q", r.dbRevision(), before, r.publicLogo())
		}
		if len(entriesWith(r.rec.Recorded(), "regional.logo.changed")) != 0 {
			t.Error("an entry was kept for a failed upload")
		}
	})

	t.Run("an earlier logo survives a failed replacement", func(t *testing.T) {
		r := newRealLogoI(t, files.DefaultPolicy())
		old := r.mustSetLogo("old.png", pngOf(t, 1))
		before := r.dbRevision()
		r.rec.Fail(boom)
		_, err := r.set("new.png", pngOf(t, 2))
		wantKind(t, err, errs.Unavailable)
		r.rec.Fail(nil)
		if r.fileRows() != 1 || r.onlyFile().ID.String() != old.Logo {
			t.Errorf("the earlier logo did not survive (rows %d)", r.fileRows())
		}
		if keys := r.mem.Keys(objects.Bucket("settings")); len(keys) != 1 {
			t.Errorf("%d objects in the bucket, want 1 (the earlier logo)", len(keys))
		}
		if r.dbRevision() != before || r.publicLogo() != old.Logo {
			t.Errorf("revision %d (was %d), Public.Logo %q", r.dbRevision(), before, r.publicLogo())
		}
	})

	t.Run("fake store: staged is empty after", func(t *testing.T) {
		r := newLogoI(t)
		r.rec.Fail(boom)
		_, err := r.set("logo.png", pngOf(t, 1))
		wantKind(t, err, errs.Unavailable)
		if len(r.fs.Staged()) != 0 {
			t.Errorf("%d staged objects left", len(r.fs.Staged()))
		}
	})
}

func TestRemoveLogo_AuditFails_RollsBack(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	set := r.mustSetLogo("logo.png", pngOf(t, 1))
	before := r.dbRevision()
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("audit store is down")))
	_, err := r.svc.RemoveLogo(r.actx())
	wantKind(t, err, errs.Unavailable)
	r.rec.Fail(nil)
	if r.fileRows() != 1 || r.onlyFile().ID.String() != set.Logo {
		t.Error("the logo was removed despite the failed audit record")
	}
	if r.count(`SELECT count(*) FROM core.file_tombstone`) != 0 {
		t.Error("a tombstone survived the rollback")
	}
	if r.dbRevision() != before || r.publicLogo() != set.Logo {
		t.Errorf("revision %d (was %d), Public.Logo %q", r.dbRevision(), before, r.publicLogo())
	}
}

// ---- F48: Attach fails after Stage -------------------------------------------------------

func TestSetLogo_AttachFails_RollsBackAndDiscards(t *testing.T) {
	r := newLogoI(t)
	before := r.dbRevision()
	r.spy.setAttachErr(errs.E("test.attach", errs.Invalid, &validate.FieldError{Field: "staged", Code: "expired", Reason: "was staged too long ago"}))
	_, err := r.set("logo.png", pngOf(t, 1))
	wantField(t, err, "staged", "expired")
	if len(r.fs.Staged()) != 0 || len(r.fs.Files()) != 0 {
		t.Errorf("staged %d attached %d after a failed Attach", len(r.fs.Staged()), len(r.fs.Files()))
	}
	if _, _, discards := r.spy.counts(); discards == 0 {
		t.Error("the staged object was never discarded")
	}
	if r.dbRevision() != before || len(r.rec.Recorded()) != 0 {
		t.Errorf("revision %d (was %d), audit %v", r.dbRevision(), before, actionsOf(r.rec.Recorded()))
	}

	// A store kind passes through unchanged too.
	r.spy.setAttachErr(errs.E("test.attach", errs.Unavailable, errors.New("store is down")))
	_, err = r.set("logo.png", pngOf(t, 1))
	wantKind(t, err, errs.Unavailable)
	if len(r.fs.Staged()) != 0 {
		t.Error("staged object left after an Unavailable Attach")
	}
}

// ---- F44, F45: real files rules -----------------------------------------------------------

func TestSetLogo_Malformed_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"png signature then junk", append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0xAB}, 64)...)},
		{"truncated png", pngOf(t, 1)[:20]},
		{"jpeg signature then junk", append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 8)...)},
		{"webp signature then junk", append([]byte("RIFF\x10\x00\x00\x00WEBP"), bytes.Repeat([]byte{0xAB}, 8)...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRealLogoI(t, files.DefaultPolicy())
			before := r.dbRevision()
			_, err := r.set("logo.png", tc.data)
			wantField(t, err, "file", "malformed")
			if r.fileRows() != 0 || len(r.mem.Keys(objects.Bucket("settings"))) != 0 {
				t.Error("a malformed image left a row or an object")
			}
			if r.dbRevision() != before || len(r.rec.Recorded()) != 0 {
				t.Errorf("revision %d (was %d), audit %v", r.dbRevision(), before, actionsOf(r.rec.Recorded()))
			}
		})
	}
}

func TestSetLogo_AtQuota_ReturnsConflictQuota(t *testing.T) {
	t.Run("quota already used up at Stage or Attach", func(t *testing.T) {
		r := newRealLogoI(t, files.Policy{MaxBytes: 25 << 20, QuotaBytes: 1, MaxPerRecord: 20})
		before := r.dbRevision()
		_, err := r.set("logo.png", pngOf(t, 1))
		wantKind(t, err, errs.Conflict)
		if !hasField(err, "file", "quota") {
			t.Errorf("no file/quota field in %v", describe(err))
		}
		if r.fileRows() != 0 || len(r.mem.Keys(objects.Bucket("settings"))) != 0 {
			t.Error("a refused logo left a row or an object")
		}
		if r.dbRevision() != before || len(r.rec.Recorded()) != 0 {
			t.Errorf("revision %d (was %d), audit %v", r.dbRevision(), before, actionsOf(r.rec.Recorded()))
		}
	})

	t.Run("an existing logo is untouched by a refused replacement", func(t *testing.T) {
		// First learn what the stored logo weighs, then set the quota to exactly that.
		probe := newRealLogoI(t, files.DefaultPolicy())
		probe.mustSetLogo("old.png", pngOf(t, 1))
		quota := probe.onlyFile().Size
		r := newRealLogoI(t, files.Policy{MaxBytes: 25 << 20, QuotaBytes: quota, MaxPerRecord: 20})
		old := r.mustSetLogo("old.png", pngOf(t, 1))
		before := r.dbRevision()
		_, err := r.set("new.png", pngOf(t, 99))
		wantKind(t, err, errs.Conflict)
		if r.fileRows() != 1 || r.onlyFile().ID.String() != old.Logo || r.dbRevision() != before {
			t.Errorf("rows %d, revision %d (was %d)", r.fileRows(), r.dbRevision(), before)
		}
		if len(r.mem.Keys(objects.Bucket("settings"))) != 1 {
			t.Error("the refused upload's object was not discarded")
		}
	})
}

// A metadata chunk in the upload does not reach the served bytes (files decision 9).
func TestGetLogo_ServedBytesCarryNoUploadedMetadata(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	r.mustSetLogo("logo.png", pngWithText(t, nameMarker))
	w, f := r.send(context.Background(), http.MethodGet, "/logo", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /logo = %d (%v)", w.Code, f.last())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(nameMarker)) {
		t.Error("a tEXt chunk survived into the served logo")
	}
	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q", w.Header().Get("Content-Type"))
	}
}

// pngWithText inserts a tEXt chunk after IHDR.
func pngWithText(t testing.TB, text string) []byte {
	t.Helper()
	src := pngOf(t, 1)
	const sig = 8
	ihdrEnd := sig + 8 + 13 + 4 // length+type, data, crc
	data := append([]byte("Comment\x00"), text...)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data))) //nolint:gosec // bounded fixture sizes
	body := append([]byte("tEXt"), data...)
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	out := append([]byte{}, src[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, src[ihdrEnd:]...)
}

// ---- F49: concurrent uploads -------------------------------------------------------------

func TestSetLogo_Concurrent_OneRowRemains(t *testing.T) {
	r := newRealLogoI(t, files.DefaultPolicy())
	const n = 6
	var wg sync.WaitGroup
	results := make([]regional.LogoResult, n)
	errsOut := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		data := pngOf(t, uint8(10+i*20))
		go func() {
			defer wg.Done()
			<-start
			results[i], errsOut[i] = r.set(fmt.Sprintf("logo-%d.png", i), data)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errsOut {
		if err != nil {
			t.Errorf("upload %d: %v", i, err)
		}
	}
	if r.fileRows() != 1 {
		t.Fatalf("%d rows remain, want 1", r.fileRows())
	}
	if n := len(entriesWith(r.rec.Recorded(), "regional.logo.changed")); n != 6 {
		t.Errorf("%d regional.logo.changed entries, want one per upload (6)", n)
	}
	seen := map[int64]bool{}
	for _, res := range results {
		if seen[int64(res.Revision)] {
			t.Errorf("revision %d returned twice: the writes were not serialised", res.Revision)
		}
		seen[int64(res.Revision)] = true
	}
	if got := r.publicLogo(); got != r.onlyFile().ID.String() {
		t.Errorf("Public.Logo = %q, the remaining file is %s", got, r.onlyFile().ID)
	}
	_, err := r.fsvc.Reap(ctxT(t))
	wantNoErr(t, err)
	if keys := r.mem.Keys(objects.Bucket("settings")); len(keys) != 1 {
		t.Errorf("%d objects after Reap, want 1", len(keys))
	}
}

// ---- F54: the snapshot load ---------------------------------------------------------------

func TestPublic_LogoLookupFails_ReturnsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind errs.Kind
	}{
		{"unavailable", errs.E("test", errs.Unavailable, errors.New("down")), errs.Unavailable},
		{"timeout", errs.E("test", errs.Timeout, errors.New("slow")), errs.Timeout},
		{"canceled", errs.E("test", errs.Canceled, errors.New("gone")), errs.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newLogoI(t)
			r.fs.Fail(tc.err)
			_, err := r.svc.Public(r.actx())
			wantKind(t, err, tc.kind)
			// The failure is not remembered: the next load succeeds.
			r.fs.Fail(nil)
			if _, err := r.svc.Public(r.actx()); err != nil {
				t.Errorf("after recovery: %v", err)
			}
		})
	}
}

// Public stays a snapshot read with no I/O once loaded (decision 16).
func TestPublic_AfterLoad_DoesNotTouchTheFilesStore(t *testing.T) {
	r := newLogoI(t)
	res := r.mustSetLogo("logo.png", pngOf(t, 1))
	ignore(r.svc.Public(r.actx()))
	r.fs.Fail(errs.E("test", errs.Unavailable, errors.New("down")))
	p, err := r.svc.Public(r.actx())
	wantNoErr(t, err)
	if p.Logo != res.Logo {
		t.Errorf("Public.Logo = %q, want %q", p.Logo, res.Logo)
	}
}

func TestRun_ReplicaSeesTheNewLogoAfterOnePoll(t *testing.T) {
	writer := newLogoIPoll(t)
	reader := newIRigOn(t, writer.pool, writer.pool, poll, func(o *regional.Options) { o.Files = writer.spy })
	ctx := ctxT(t)
	if p, err := reader.svc.Public(ctx); err != nil || p.Logo != "" {
		t.Fatalf("reader Public = %+v, %v", p, err)
	}
	run := startRun(t, reader)
	res := writer.mustSetLogo("logo.png", pngOf(t, 1))
	wantNoErr(t, reader.clk.Advance(poll))
	eventually(t, "the replica to see the new logo ID", func() bool {
		p, err := reader.svc.Public(ctx)
		return err == nil && p.Logo == res.Logo
	})
	run.shutdown(t)
}

// ---- privacy: the file name is a marker that must go nowhere -----------------------------

func TestSetLogo_FileName_ReachesNoLogSpanOrAuditEntry(t *testing.T) {
	r := newLogoI(t)
	r.mustSetLogo(nameMarker+".png", pngOf(t, 1))
	// And a refused one, so the error paths are covered too.
	ignore(r.set(nameMarker+".svg", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>")))
	ignore(r.svc.RemoveLogo(r.actx()))
	if r.cap.Contains(nameMarker) {
		t.Error("the uploaded file's name reached a log, span or metric")
	}
	for _, e := range r.rec.Recorded() {
		if strings.Contains(fmt.Sprintf("%+v", e), nameMarker) {
			t.Errorf("the uploaded file's name reached the audit entry %s", e.Action)
		}
	}
}
