//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Privacy (files.md, "Ownership and privacy" and Test plan): a name, a declared type and the
// bytes never reach a log, span, metric, error message, audit entry, object key or path. The
// telemetry capture also fails the test at cleanup on any policy violation.

func namedPDF() []byte { return pdfDoc("% " + nameMarker + "\n") }

func leaks(r *rig, what string) {
	r.t.Helper()
	if r.cap.Contains(nameMarker) {
		r.t.Errorf("%s: the name or bytes marker reached a log record, span or metric", what)
	}
	for _, e := range r.audit.Recorded() {
		if strings.Contains(e.Reason, nameMarker) {
			r.t.Errorf("%s: audit entry %s carries the marker in Reason", what, e.Action)
		}
		for _, f := range e.Fields {
			if strings.Contains(f, nameMarker) {
				r.t.Errorf("%s: audit entry %s carries the marker in Fields", what, e.Action)
			}
		}
	}
}

func noMarker(t testing.TB, err error, what string) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), nameMarker) {
		t.Errorf("%s: error message carries the name: %v", what, err)
	}
}

func TestStage_NameNeverLeaks(t *testing.T) {
	r := newRig(t, withAttachedLookup(false, nil))
	name := nameMarker + ".pdf"

	// success
	st, err := stageBytes(ctxT(t), r.vol, name, files.Documents, namedPDF())
	wantNoErr(t, err)
	wantNoErr(t, r.vol.Discard(ctxT(t), st))

	// refused type
	_, err = stageBytes(ctxT(t), r.vol, name, files.Images, namedPDF())
	wantKind(t, err, errs.Invalid)
	noMarker(t, err, "type")

	// too large
	r.policy.set(files.Policy{MaxBytes: 1 << 20, MaxPerRecord: 20})
	_, err = stageBytes(ctxT(t), r.vol, name, files.Documents, pdfOfSize(t, 1<<20+1))
	wantKind(t, err, errs.Invalid)
	noMarker(t, err, "too_large")

	// bad name
	_, err = stageBytes(ctxT(t), r.vol, nameMarker+"\x00.pdf", files.Documents, namedPDF())
	wantKind(t, err, errs.Invalid)
	noMarker(t, err, "control chars")

	// store failures
	r.store.fail("put", errs.E("test.Put", errs.Unavailable, errors.New("down")))
	_, err = stageBytes(ctxT(t), r.vol, name, files.Documents, namedPDF())
	wantKind(t, err, errs.Unavailable)
	noMarker(t, err, "put fails")
	r.store.up()
	st = mustStage(t, r.vol, name, files.Documents, namedPDF())
	r.store.fail("delete", unavailable("delete"))
	err = r.vol.Discard(ctxT(t), st)
	wantKind(t, err, errs.Unavailable)
	noMarker(t, err, "discard fails")
	r.store.up()

	// the object key is not the name, and nothing in the store's listing reveals it
	for _, k := range r.objectKeys(ownerVol) {
		if strings.Contains(strings.ToLower(k), "quillfeather") || strings.Contains(k, ".pdf") {
			t.Errorf("object key %q reveals the name", k)
		}
	}
	// start-up and sweep logs
	r.store.down()
	wantNoErr(t, r.svc.Probe(ctxT(t)))
	r.store.up()
	r.clk.Advance(files.OrphanAge + time.Hour) //nolint:errcheck,gosec // fake clock
	_, err = r.svc.SweepOrphans(ctxT(t))
	wantNoErr(t, err)

	leaks(r, "Stage paths")
}

func TestStage_DeclaredTypeNeverLeaks(t *testing.T) {
	r := newRig(t)
	req := multipartRequest(t, filePart("file", nameMarker+".png", "application/x-"+nameMarker, refusedBodies["html named png"]))
	mp, err := files.ReadUpload(req, "file")
	wantNoErr(t, err)
	_, err = r.vol.Stage(ctxT(t), files.Upload{Name: mp.Name, Accept: files.Images, Body: mp.Body})
	wantKind(t, err, errs.Invalid)
	noMarker(t, err, "declared type")
	leaks(r, "declared type")
}

func TestObjectLayout_StoreHoldsNoName(t *testing.T) {
	r := newRig(t)
	st := mustStage(t, r.vol, nameMarker+".pdf", files.Documents, pdfDoc(""))
	keys := r.objectKeys(st.Bucket)
	if len(keys) == 0 {
		t.Fatal("Stage left no object in the bucket")
	}
	for _, k := range append(keys, string(st.Bucket), string(st.Key)) {
		if strings.Contains(strings.ToLower(k), "quillfeather") {
			t.Errorf("store path %q carries the name", k)
		}
	}
}

func TestTempFileNames_CarryNoName(t *testing.T) {
	r := newRig(t)
	body := &watchingReader{data: bytes.NewReader(pdfOfSize(t, 300<<10)), onRead: func() {
		ents, _ := os.ReadDir(r.tmp) //nolint:errcheck // best effort peek
		for _, e := range ents {
			if strings.Contains(strings.ToLower(e.Name()), "quillfeather") {
				t.Errorf("temp file %q carries the name", e.Name())
			}
		}
	}}
	_, err := r.vol.Stage(ctxT(t), files.Upload{Name: nameMarker + ".pdf", Accept: files.Documents, Body: body})
	wantNoErr(t, err)
}

type watchingReader struct {
	data   *bytes.Reader
	onRead func()
}

func (w *watchingReader) Read(p []byte) (int, error) {
	w.onRead()
	if len(p) > 4096 {
		p = p[:4096]
	}
	return w.data.Read(p)
}

func TestServe_NameNeverEntersBodyOrOtherHeaders(t *testing.T) {
	f, c := serveFixture(3, "application/pdf", nameMarker+".pdf", pdfDoc(""))
	rec := doServe(f, c)
	for k, vs := range rec.Header() {
		if k == "Content-Disposition" {
			continue // the only header that carries the name, by design
		}
		for _, v := range vs {
			if strings.Contains(v, nameMarker) {
				t.Errorf("header %s carries the name", k)
			}
		}
	}
}

var _ = context.Background
