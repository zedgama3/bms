package hello_test

import (
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/modules/hello"
)

// F32
func TestNew_NilNotify_ReturnsInvalid(t *testing.T) {
	d := rawDeps(t)
	d.Notify = nil
	m, err := hello.New(ctxT(t), d)
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("New err = %v, want Invalid", err)
	}
	if m != nil {
		t.Errorf("New returned a module with an error")
	}
	if !strings.Contains(err.Error(), "Notify") {
		t.Errorf("err = %v, want it to name Notify", err)
	}
}

// F33
func TestGreet_NotifyNotConfigured_StillGreets(t *testing.T) {
	f := newFx(t, cfg{})
	f.mail.SetConfigured(false)
	f.start(t)
	got, err := f.mod.Service().Greet(ctxT(t))
	if err != nil {
		t.Fatalf("Greet with e-mail not set up: %v", err)
	}
	if rows := f.store.Rows(); len(rows) != 1 || rows[0].ID != got.ID {
		t.Errorf("rows = %+v, want the greeting saved", rows)
	}
	if n := len(f.sig.Published()); n != 1 {
		t.Errorf("Published = %d, want 1", n)
	}
	if sent := f.mail.Sent(); len(sent) != 0 {
		t.Errorf("Sent = %+v, want nothing queued", sent)
	}
	var debug []string
	for _, r := range f.cap.Records() {
		if r.Level == slog.LevelDebug && r.Attrs["notify"] == "off" {
			debug = append(debug, r.Message)
			for k, v := range r.Attrs {
				if strings.Contains(v, got.ID.String()) || strings.Contains(v, alice.String()) {
					t.Errorf("the debug line carries an ID in %q", k)
				}
			}
		}
	}
	if len(debug) != 1 {
		t.Errorf("debug lines with notify=off = %d, want 1", len(debug))
	}
}

// F34
func TestGreet_NotifyFails_RollsBackAndPublishesNothing(t *testing.T) {
	for _, kind := range []errs.Kind{errs.Invalid, errs.Unavailable, errs.Timeout, errs.Canceled, errs.Internal} {
		t.Run(kind.String(), func(t *testing.T) {
			f := newFx(t, cfg{})
			f.mail.Fail(errs.E("test.notify", kind, errors.New("refused")))
			f.start(t)
			if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, kind) {
				t.Fatalf("err = %v, want %v", err, kind)
			}
			if n := len(f.sig.Published()); n != 0 {
				t.Errorf("Published = %d after a failed Send, want 0", n)
			}
			if sent := f.mail.Sent(); len(sent) != 0 {
				t.Errorf("Sent = %+v after a failed Send, want nothing", sent)
			}
			if n := f.mod.Seen(); n != 0 {
				t.Errorf("Seen = %d, want 0 (nothing was delivered)", n)
			}
		})
	}
}

func TestGreet_NotifyReadOnly_ReturnsUnavailableAndSendsNothing(t *testing.T) {
	f := newFx(t, cfg{readOnly: true})
	if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Unavailable) {
		t.Fatalf("err = %v, want Unavailable", err)
	}
	if sent := f.mail.Sent(); len(sent) != 0 {
		t.Errorf("Sent = %+v on a read-only copy, want nothing", sent)
	}
}

// F35
func TestGreet_SendsGreetedToActorAfterCommit(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	got, err := f.mod.Service().Greet(ctxT(t))
	if err != nil {
		t.Fatalf("Greet: %v", err)
	}
	sent := f.mail.Sent()
	if len(sent) != 1 {
		t.Fatalf("Sent = %+v, want exactly one message", sent)
	}
	m := sent[0]
	if m.Template != hello.TemplateGreeted || hello.TemplateGreeted != "hello.greeted" {
		t.Errorf("Template = %q, want hello.greeted", m.Template)
	}
	if len(m.To) != 1 || m.To[0] != alice {
		t.Errorf("To = %v, want only the actor", m.To)
	}
	if m.Key != "greeted:"+got.ID.String() {
		t.Errorf("Key = %q, want greeted:<id>", m.Key)
	}
	if len(m.Vars) != 1 || m.Vars["word"] != "howdy" {
		t.Errorf("Vars = %v, want {word: howdy}", m.Vars)
	}
	if m.Link != "/hello/" {
		t.Errorf("Link = %q, want /hello/", m.Link)
	}
}

func TestGreet_InsertFails_SendsNothing(t *testing.T) {
	f := newFx(t, cfg{})
	f.store.Fail("Insert", errs.E("test.store", errs.Unavailable, errors.New("boom")))
	if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Unavailable) {
		t.Fatalf("err = %v, want Unavailable", err)
	}
	if sent := f.mail.Sent(); len(sent) != 0 {
		t.Errorf("Sent = %+v after a failed Insert, want nothing", sent)
	}
}

func TestGreet_EachGreetingHasItsOwnKey(t *testing.T) {
	f := newFx(t, cfg{})
	for range 2 {
		if _, err := f.mod.Service().Greet(ctxT(t)); err != nil {
			t.Fatalf("Greet: %v", err)
		}
	}
	sent := f.mail.Sent()
	if len(sent) != 2 || sent[0].Key == sent[1].Key {
		t.Errorf("Sent keys = %+v, want two different keys", sent)
	}
}

func TestGreet_NoActorOrDenied_SendsNothing(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Allow: hello.DenyAll{}}})
	if _, err := f.mod.Service().Greet(ctxT(t)); !errs.Is(err, errs.Permission) {
		t.Fatalf("err = %v, want Permission", err)
	}
	if sent := f.mail.Sent(); len(sent) != 0 {
		t.Errorf("Sent = %+v for a refused Greet, want nothing", sent)
	}
}

// F36
func TestTemplates_Parse(t *testing.T) {
	tpl := hello.Manifest.Templates
	if tpl == nil {
		t.Fatal("Manifest.Templates is nil")
	}
	if err := notify.CheckTemplates(hello.Manifest.Name, tpl); err != nil {
		t.Fatalf("CheckTemplates: %v", err)
	}
	for _, name := range []string{"greeted.en.tmpl", "greeted.es.tmpl"} {
		raw, err := fs.ReadFile(tpl, name)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		if !strings.Contains(string(raw), "{{.Vars.word}}") || !strings.HasPrefix(string(raw), "Subject:") {
			t.Errorf("%s does not carry the Subject header and the word variable", name)
		}
	}
	entries, err := fs.ReadDir(tpl, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("templates holds %d files, want exactly greeted.en and greeted.es", len(entries))
	}
	if hello.TemplateGreeted.Owner() != hello.Manifest.Name {
		t.Errorf("TemplateGreeted owner = %q, want %q", hello.TemplateGreeted.Owner(), hello.Manifest.Name)
	}
}

func TestManifest_TemplatesAreTheEmbeddedDirectory(t *testing.T) {
	if _, err := fs.Stat(hello.Manifest.Templates, "greeted.en.tmpl"); err != nil {
		t.Errorf("greeted.en.tmpl is not at the root of Manifest.Templates: %v", err)
	}
}
