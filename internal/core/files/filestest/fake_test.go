//nolint:gosec,wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package filestest_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/files/filestest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var _ files.Store = (*filestest.Fake)(nil)

type recTag struct{}

func rec(kind string) audit.Ref { return audit.RefOf(kind, model.NewID[model.ID[recTag]]()) }

func pdf(s string) []byte { return []byte("%PDF-1.4\n% " + s + "\ntrailer\n%%EOF\n") }

func pngBytes() []byte {
	// 1x1 transparent PNG.
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x9aM\x8e\x00\x00\x00\x00IEND\xaeB`\x82")
}

func writeTx(t *testing.T, fn func(ctx context.Context) error) error {
	t.Helper()
	return dbtest.NewFake().Transact(t.Context(), db.Write, fn)
}

func stage(t *testing.T, f *filestest.Fake, name string, body []byte, accept files.Accept) (files.Staged, error) {
	t.Helper()
	return f.Stage(t.Context(), files.Upload{Name: name, Accept: accept, Body: bytes.NewReader(body)})
}

func mustStage(t *testing.T, f *filestest.Fake, name string, body []byte, accept files.Accept) files.Staged {
	t.Helper()
	st, err := stage(t, f, name, body, accept)
	if err != nil {
		t.Fatalf("Stage(%q): %v", name, err)
	}
	return st
}

func attach(t *testing.T, f *filestest.Fake, st files.Staged, a files.Attachment) (files.File, error) {
	t.Helper()
	var out files.File
	err := writeTx(t, func(ctx context.Context) error {
		var err error
		out, err = f.Attach(ctx, st, a)
		return err
	})
	return out, err
}

func TestFake_StageAttachDiscard_Lifecycle(t *testing.T) {
	f := filestest.NewFake("volunteers")
	st, err := stage(t, f, "Cert.PDF", pdf("a"), files.Documents)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if len(f.Staged()) != 1 || len(f.Files()) != 0 {
		t.Fatalf("Staged/Files = %d/%d, want 1/0", len(f.Staged()), len(f.Files()))
	}
	if st.ContentType != "application/pdf" || st.Name != "Cert.pdf" || st.Bucket != "volunteers" {
		t.Errorf("Staged = %+v", st)
	}
	file, err := attach(t, f, st, files.Attachment{Record: rec("volunteers.qualification-record"), Subject: model.NewID[model.PersonID](), Level: model.SensitivityPersonal})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(f.Staged()) != 0 || len(f.Files()) != 1 || f.Files()[0].ID != file.ID {
		t.Errorf("after Attach Staged/Files = %d/%d", len(f.Staged()), len(f.Files()))
	}
	if err := f.Discard(t.Context(), st); err != nil { // no-op once attached
		t.Errorf("Discard after Attach: %v", err)
	}
	if len(f.Files()) != 1 {
		t.Error("Discard after Attach removed the file")
	}

	st2 := mustStage(t, f, "b.pdf", pdf("b"), files.Documents)
	if err := f.Discard(t.Context(), st2); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if len(f.Staged()) != 0 {
		t.Error("Discard left the staged object")
	}
	if err := f.Discard(t.Context(), st2); err != nil {
		t.Errorf("second Discard: %v", err)
	}
}

func TestFake_ValidatesLikeTheRealStore(t *testing.T) {
	f := filestest.NewFake("volunteers")
	for name, body := range map[string][]byte{
		"html": []byte("<html><script>alert(1)</script></html>"),
		"svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`),
		"zip":  []byte("PK\x03\x04junk"),
	} {
		if _, err := stage(t, f, "x.png", body, files.Images); !errs.Is(err, errs.Invalid) {
			t.Errorf("%s: Stage = %v, want Invalid (type)", name, err)
		}
	}
	if _, err := stage(t, f, "x.pdf", pdf("a"), files.Images); !errs.Is(err, errs.Invalid) {
		t.Errorf("pdf where images wanted: %v", err)
	}
	if _, err := stage(t, f, "x.png", pngBytes(), nil); !errs.Is(err, errs.Invalid) {
		t.Errorf("empty Accept: %v", err)
	}
	if _, err := stage(t, f, "x.pdf", nil, files.Documents); !errs.Is(err, errs.Invalid) {
		t.Errorf("empty body: %v", err)
	}
	if len(f.Staged()) != 0 {
		t.Errorf("refused uploads left %d staged", len(f.Staged()))
	}
}

func TestFake_Attach_RulesOfTheRealStore(t *testing.T) {
	f := filestest.NewFake("volunteers")
	st := mustStage(t, f, "a.pdf", pdf("a"), files.Documents)
	good := files.Attachment{Record: rec("volunteers.qualification-record"), Level: model.SensitivityInternal}

	if _, err := attach(t, f, st, files.Attachment{Record: rec("incidents.photo"), Level: model.SensitivityInternal}); !errs.Is(err, errs.Invalid) {
		t.Errorf("foreign area: %v, want Invalid", err)
	}
	if _, err := attach(t, f, st, files.Attachment{Record: rec("volunteers.qualification-record"), Level: 0}); !errs.Is(err, errs.Invalid) {
		t.Errorf("zero level: %v, want Invalid", err)
	}
	if _, err := attach(t, f, st, files.Attachment{Level: model.SensitivityInternal}); !errs.Is(err, errs.Invalid) {
		t.Errorf("zero record: %v, want Invalid", err)
	}
	other := filestest.NewFake("incidents")
	foreign := mustStage(t, other, "a.pdf", pdf("a"), files.Documents)
	if _, err := attach(t, f, foreign, good); !errs.Is(err, errs.Invalid) {
		t.Errorf("another owner's Staged: %v, want Invalid", err)
	}
	if len(f.Files()) != 0 {
		t.Fatal("a refused Attach stored a file")
	}
	if _, err := attach(t, f, st, good); err != nil {
		t.Fatalf("good Attach: %v", err)
	}
	if _, err := attach(t, f, st, good); !errs.Is(err, errs.Internal) {
		t.Errorf("Staged attached twice: %v, want Internal", err)
	}
	if _, err := f.Attach(t.Context(), st, good); !errs.Is(err, errs.Internal) {
		t.Errorf("Attach outside a transaction: %v, want Internal", err)
	}
}

func TestFake_ReadsDeletesAndScoping(t *testing.T) {
	f := filestest.NewFake("volunteers")
	r := rec("volunteers.qualification-record")
	var ids []files.FileID
	for _, s := range []string{"a", "b", "c"} {
		st := mustStage(t, f, s+".pdf", pdf(s), files.Documents)
		file, err := attach(t, f, st, files.Attachment{Record: r, Level: model.SensitivityInternal})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, file.ID)
	}
	got, err := f.ByRecord(t.Context(), r)
	if err != nil || len(got) != 3 {
		t.Fatalf("ByRecord = %d, %v", len(got), err)
	}
	for i := range ids {
		if got[i].ID != ids[i] {
			t.Errorf("ByRecord not oldest first at %d", i)
		}
	}
	if _, err := f.ByRecord(t.Context(), rec("incidents.photo")); !errs.Is(err, errs.Invalid) {
		t.Errorf("ByRecord foreign area: %v, want Invalid", err)
	}
	if _, err := f.Get(t.Context(), model.NewID[files.FileID]()); !errs.Is(err, errs.NotFound) {
		t.Errorf("Get unknown: %v, want NotFound", err)
	}
	file, c, err := f.Open(t.Context(), ids[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, rerr := io.ReadAll(c)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if cerr := c.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	if file.ID != ids[0] || !bytes.Equal(b, pdf("a")) {
		t.Error("Open returned the wrong file or bytes")
	}
	if _, _, err := f.Open(t.Context(), model.NewID[files.FileID]()); !errs.Is(err, errs.NotFound) {
		t.Errorf("Open unknown: %v, want NotFound", err)
	}

	if err := writeTx(t, func(ctx context.Context) error { return f.Delete(ctx, ids[0]) }); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := writeTx(t, func(ctx context.Context) error { return f.Delete(ctx, ids[0]) }); !errs.Is(err, errs.NotFound) {
		t.Errorf("second Delete: %v, want NotFound", err)
	}
	var n int
	if err := writeTx(t, func(ctx context.Context) error {
		var err error
		n, err = f.DeleteByRecord(ctx, r)
		return err
	}); err != nil || n != 2 {
		t.Errorf("DeleteByRecord = %d, %v; want 2", n, err)
	}
	if err := writeTx(t, func(ctx context.Context) error {
		var err error
		n, err = f.DeleteByRecord(ctx, r)
		return err
	}); err != nil || n != 0 {
		t.Errorf("DeleteByRecord of nothing = %d, %v; want 0, nil", n, err)
	}
	if len(f.Files()) != 0 {
		t.Errorf("Files() = %d after deleting everything", len(f.Files()))
	}
	if err := f.Delete(t.Context(), ids[1]); !errs.Is(err, errs.Internal) && !errs.Is(err, errs.NotFound) {
		t.Errorf("Delete outside a transaction: %v", err)
	}
}

func TestFake_Fail_EveryLaterCallAnswersTheError(t *testing.T) {
	f := filestest.NewFake("volunteers")
	boom := errs.E("test", errs.Unavailable, errors.New("store down"))
	st := mustStage(t, f, "a.pdf", pdf("a"), files.Documents)
	file, err := attach(t, f, st, files.Attachment{Record: rec("volunteers.qualification-record"), Level: model.SensitivityInternal})
	if err != nil {
		t.Fatal(err)
	}
	f.Fail(boom)
	ctx := t.Context()
	if _, err := stage(t, f, "b.pdf", pdf("b"), files.Documents); !errors.Is(err, boom) {
		t.Errorf("Stage: %v", err)
	}
	if err := f.Discard(ctx, st); !errors.Is(err, boom) {
		t.Errorf("Discard: %v", err)
	}
	if _, err := f.Get(ctx, file.ID); !errors.Is(err, boom) {
		t.Errorf("Get: %v", err)
	}
	if _, err := f.ByRecord(ctx, file.Record); !errors.Is(err, boom) {
		t.Errorf("ByRecord: %v", err)
	}
	if _, _, err := f.Open(ctx, file.ID); !errors.Is(err, boom) {
		t.Errorf("Open: %v", err)
	}
	if err := writeTx(t, func(ctx context.Context) error { return f.Delete(ctx, file.ID) }); !errors.Is(err, boom) {
		t.Errorf("Delete: %v", err)
	}
	if err := writeTx(t, func(ctx context.Context) error { _, err := f.DeleteByRecord(ctx, file.Record); return err }); !errors.Is(err, boom) {
		t.Errorf("DeleteByRecord: %v", err)
	}
	if _, err := attach(t, f, st, files.Attachment{Record: file.Record, Level: model.SensitivityInternal}); !errors.Is(err, boom) {
		t.Errorf("Attach: %v", err)
	}
}

func TestFake_Stripped_ImagesGetStrippedFlag(t *testing.T) {
	f := filestest.NewFake("volunteers")
	st, err := stage(t, f, "a.png", pngBytes(), files.Images)
	if err != nil {
		t.Fatal(err)
	}
	if st.ContentType != "image/png" || !st.Stripped {
		t.Errorf("Staged = %+v, want image/png and Stripped", st)
	}
}

func TestFake_SafeForConcurrentUse(_ *testing.T) {
	f := filestest.NewFake("volunteers")
	done := make(chan struct{})
	for i := range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			st, err := f.Stage(context.Background(), files.Upload{Name: "a.pdf", Accept: files.Documents, Body: bytes.NewReader(pdf(string(rune('a' + i%26))))})
			if err == nil {
				_ = f.Discard(context.Background(), st) //nolint:errcheck // test
			}
			_ = f.Staged()
			_ = f.Files()
		}()
	}
	for range 8 {
		<-done
	}
}
