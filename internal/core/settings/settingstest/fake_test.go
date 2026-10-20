package settingstest_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// The Fake is a settings.Reader.
var _ settings.Reader = (*settingstest.Fake)(nil)

func decls() []settings.Decl {
	return []settings.Decl{
		{Key: "greeting", Kind: settings.Text, Default: "Hello, burner"},
		{Key: "limit", Kind: settings.Int, Default: "7"},
		{Key: "on", Kind: settings.Bool, Default: "true"},
		{Key: "wait", Kind: settings.Duration, Default: "72h"},
		{Key: "mode", Kind: settings.Enum, Default: "a", Options: []string{"a", "b"}},
		{Key: "password", Kind: settings.Secret},
	}
}

func wantKind(t *testing.T, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("kind = %v (%v), want %v", errs.KindOf(err), err, kind)
	}
}

func TestFake_AnswersTheDefaults(t *testing.T) {
	ctx := context.Background()
	f := settingstest.NewFake("hello", decls())
	if v, err := f.Text(ctx, "greeting"); err != nil || v != "Hello, burner" {
		t.Errorf("Text = %q, %v", v, err)
	}
	if v, err := f.Int(ctx, "limit"); err != nil || v != 7 {
		t.Errorf("Int = %d, %v", v, err)
	}
	if v, err := f.Bool(ctx, "on"); err != nil || !v {
		t.Errorf("Bool = %v, %v", v, err)
	}
	if v, err := f.Duration(ctx, "wait"); err != nil || v != 72*time.Hour {
		t.Errorf("Duration = %v, %v", v, err)
	}
	if v, err := f.Text(ctx, "mode"); err != nil || v != "a" {
		t.Errorf("Text on an enum = %q, %v", v, err)
	}
	if v, err := f.Secret(ctx, "password"); err != nil || v.IsSet() {
		t.Errorf("unset Secret = set:%v, %v", v.IsSet(), err)
	}
}

func TestFake_SetOverridesAndResetsNothingElse(t *testing.T) {
	ctx := context.Background()
	f := settingstest.NewFake("hello", decls())
	f.Set("greeting", "Welcome")
	f.Set("limit", "12")
	f.Set("on", "false")
	f.Set("wait", "90m")
	f.Set("mode", "b")
	if v := first(f.Text(ctx, "greeting")); v != "Welcome" {
		t.Errorf("greeting = %q", v)
	}
	if v := first(f.Int(ctx, "limit")); v != 12 {
		t.Errorf("limit = %d", v)
	}
	if v := first(f.Bool(ctx, "on")); v {
		t.Error("on = true")
	}
	if v := first(f.Duration(ctx, "wait")); v != 90*time.Minute {
		t.Errorf("wait = %v", v)
	}
	if v := first(f.Text(ctx, "mode")); v != "b" {
		t.Errorf("mode = %q", v)
	}
	// Setting again replaces.
	f.Set("limit", "13")
	if v := first(f.Int(ctx, "limit")); v != 13 {
		t.Errorf("limit = %d", v)
	}
	// A fresh Fake is unaffected: no shared state between fakes.
	g := settingstest.NewFake("hello", decls())
	if v := first(g.Int(ctx, "limit")); v != 7 {
		t.Errorf("another Fake saw the override: %d", v)
	}
}

func TestFake_SetSecret(t *testing.T) {
	ctx := context.Background()
	f := settingstest.NewFake("mail", decls())
	f.SetSecret("password", secret.New("invented-pass"))
	v, err := f.Secret(ctx, "password")
	if err != nil || v.Value() != "invented-pass" {
		t.Errorf("Secret = %v", err)
	}
	if fmt.Sprint(v) == "invented-pass" || fmt.Sprintf("%+v", v) == "invented-pass" {
		t.Error("the secret formats as its plaintext")
	}
}

func TestFake_UndeclaredKey_ReturnsInvalid(t *testing.T) {
	ctx := context.Background()
	f := settingstest.NewFake("hello", decls())
	for _, key := range []string{"", "nope", "hello.greeting", "Greeting"} {
		t.Run(key, func(t *testing.T) {
			_, err := f.Bool(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = f.Int(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = f.Duration(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = f.Text(ctx, key)
			wantKind(t, err, errs.Invalid)
			_, err = f.Secret(ctx, key)
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestFake_KindMismatch_ReturnsInvalid(t *testing.T) {
	ctx := context.Background()
	f := settingstest.NewFake("hello", decls())
	tests := map[string]func() error{
		"Int on a text":      func() error { return errOf(f.Int(ctx, "greeting")) },
		"Bool on an int":     func() error { return errOf(f.Bool(ctx, "limit")) },
		"Duration on a bool": func() error { return errOf(f.Duration(ctx, "on")) },
		"Text on an int":     func() error { return errOf(f.Text(ctx, "limit")) },
		"Text on a secret":   func() error { return errOf(f.Text(ctx, "password")) },
		"Secret on a text":   func() error { return errOf(f.Secret(ctx, "greeting")) },
		"Int on an enum":     func() error { return errOf(f.Int(ctx, "mode")) },
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) { wantKind(t, call(), errs.Invalid) })
	}
}

func TestFake_NoDecls_EveryReadIsInvalid(t *testing.T) {
	f := settingstest.NewFake("quiet", nil)
	_, err := f.Text(context.Background(), "anything")
	wantKind(t, err, errs.Invalid)
}

func TestFake_ConcurrentUse_IsRaceFree(_ *testing.T) {
	f := settingstest.NewFake("hello", decls())
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			f.Set("limit", fmt.Sprint(i))
			f.SetSecret("password", secret.New(fmt.Sprint("pw", i)))
		}()
		go func() {
			defer wg.Done()
			ignore(f.Int(context.Background(), "limit"))
			ignore(f.Secret(context.Background(), "password"))
			ignore(f.Text(context.Background(), "greeting"))
		}()
	}
	wg.Wait()
}

// first returns the first of two results; for reads whose error a test does not need.
func first[T, U any](v T, _ U) T { return v }

// ignore swallows results of a call made only to exercise a path.
func ignore(...any) {}

func errOf[T any](_ T, err error) error { return err }
