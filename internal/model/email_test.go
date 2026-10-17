package model_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

func TestParseEmail_AcceptsTable(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "sparkle.pony@example.org", "sparkle.pony@example.org"},
		{"trimmed", "  sparkle@example.org \t\n", "sparkle@example.org"},
		{"upper-case kept", "Sparkle.Pony@Example.ORG", "Sparkle.Pony@Example.ORG"},
		{"plus tag", "sparkle+burn@example.org", "sparkle+burn@example.org"},
		{"dots", "a.b.c@sub.example.org", "a.b.c@sub.example.org"},
		{"localhost", "root@localhost", "root@localhost"},
		{"non-ascii local", "josé@example.org", "josé@example.org"},
		{"local 64 bytes", strings.Repeat("a", 64) + "@example.org", strings.Repeat("a", 64) + "@example.org"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := model.ParseEmail(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if string(e) != tc.want {
				t.Errorf("got %q", e)
			}
			if err := e.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestParseEmail_Empty_ReturnsInvalid(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		_, err := model.ParseEmail(in)
		if !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: %v", in, err)
		}
		found := false
		for f := range fieldNames(err) {
			if f == "email" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want field email", in)
		}
	}
	if err := model.Email("").Validate(); !errs.Is(err, errs.Invalid) {
		t.Errorf("Validate empty: %v", err)
	}
	if !fieldNames(model.Email("").Validate())["email"] {
		t.Error("Validate empty: want field email")
	}
}

func TestParseEmail_TooLong_ReturnsInvalid(t *testing.T) {
	long := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." +
		strings.Repeat("d", 63) + "." + strings.Repeat("e", 50) // > 254
	tests := map[string]string{
		"local 65":  strings.Repeat("a", 65) + "@example.org",
		"total 255": strings.Repeat("a", 60) + "@" + strings.Repeat("b", 194),
		"way long":  long,
	}
	if len(tests["total 255"]) != 255 {
		t.Fatalf("test setup: %d", len(tests["total 255"]))
	}
	if len(long) <= 254 {
		t.Fatalf("test setup: %d", len(long))
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := model.ParseEmail(in); !errs.Is(err, errs.Invalid) {
				t.Errorf("ParseEmail: %v", err)
			}
			if err := model.Email(in).Validate(); !errs.Is(err, errs.Invalid) {
				t.Errorf("Validate: %v", err)
			}
		})
	}
	// exactly 254 bytes is fine
	ok := strings.Repeat("a", 60) + "@" + strings.Repeat("b", 193)
	if len(ok) != 254 {
		t.Fatalf("test setup: %d", len(ok))
	}
	if _, err := model.ParseEmail(ok); err != nil {
		t.Errorf("254 bytes: %v", err)
	}
}

func TestParseEmail_BadShape_ReturnsInvalid(t *testing.T) {
	tests := map[string]string{
		"no at":            "sparkle.example.org",
		"two ats":          "a@b@example.org",
		"empty local":      "@example.org",
		"empty domain":     "sparkle@",
		"domain dot lead":  "sparkle@.example.org",
		"domain dot trail": "sparkle@example.org.",
		"only at":          "@",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := model.ParseEmail(in); !errs.Is(err, errs.Invalid) {
				t.Errorf("ParseEmail: %v", err)
			}
			if err := model.Email(in).Validate(); !errs.Is(err, errs.Invalid) {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestParseEmail_ForbiddenCharacters_ReturnsInvalid(t *testing.T) {
	tests := map[string]string{
		"space local":     "spa rkle@example.org",
		"space domain":    "sparkle@exam ple.org",
		"tab":             "spar\tkle@example.org",
		"newline":         "sparkle@example.org\nBcc: x@example.org",
		"cr":              "spark\rle@example.org",
		"nul":             "spark\x00le@example.org",
		"del":             "spark\x7fle@example.org",
		"lt":              "<sparkle@example.org",
		"gt":              "sparkle@example.org>",
		"comma":           "a,b@example.org",
		"semicolon":       "a;b@example.org",
		"quote":           `"a"@example.org`,
		"comma in domain": "a@exa,mple.org",
		"unicode space":   "a b@example.org",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := model.ParseEmail(in); !errs.Is(err, errs.Invalid) {
				t.Errorf("ParseEmail: %v", err)
			}
			if err := model.Email(in).Validate(); !errs.Is(err, errs.Invalid) {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestEmailValidate_MatchesParseEmailWithoutTrimming(t *testing.T) {
	const padded = "  sparkle@example.org  "
	if _, err := model.ParseEmail(padded); err != nil {
		t.Fatalf("ParseEmail: %v", err)
	}
	if err := model.Email(padded).Validate(); !errs.Is(err, errs.Invalid) {
		t.Fatalf("Validate with space: %v", err)
	}
}

func TestEmailCanonical_LowercasesAndIsIdempotent(t *testing.T) {
	tests := []struct{ in, want model.Email }{
		{"Sparkle.Pony@Example.ORG", "sparkle.pony@example.org"},
		{"already@lower.example.org", "already@lower.example.org"},
		{"A.B+Tag@X.Example.Org", "a.b+tag@x.example.org"}, // no dot or +tag stripping
		{"", ""},
	}
	for _, tc := range tests {
		got := tc.in.Canonical()
		if got != tc.want {
			t.Errorf("%q: got %q", tc.in, got)
		}
		if got.Canonical() != got {
			t.Errorf("%q: not idempotent", tc.in)
		}
	}
}

func TestEmailLogValue_RedactsUnderTextAndJSONHandlers(t *testing.T) {
	const addr = "sparkle.pony@example.org"
	e := model.Email(addr)

	if v := e.LogValue(); v.String() != secret.Redacted {
		t.Errorf("LogValue %q", v.String())
	}
	if v := model.Email("").LogValue(); v.String() != secret.Unset {
		t.Errorf("empty LogValue %q", v.String())
	}

	handlers := map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	}
	for name, mk := range handlers {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			l := slog.New(mk(&buf))
			l.Info("direct", slog.Any("e", e))
			l.Info("empty", slog.Any("e", model.Email("")))
			out := buf.String()
			if strings.Contains(out, addr) {
				t.Errorf("address leaked: %s", out)
			}
			if !strings.Contains(out, secret.Redacted) {
				t.Errorf("no redaction marker: %s", out)
			}
			if !strings.Contains(out, secret.Unset) {
				t.Errorf("no unset marker: %s", out)
			}
		})
	}
}

func TestParseEmail_InvalidUTF8OrFormatChars_ReturnsInvalidWithoutEcho(t *testing.T) {
	const m = "hunter2-LEAK"
	tests := map[string]string{
		"invalid utf8 local":  "\xff@example.org",
		"invalid utf8 domain": "a@exam\xffple.org",
		"zero-width space":    "spa\u200Brkle@example.org",
		"zero-width domain":   "sparkle@exam\u200Bple.org",
		"bidi override":       "spar\u202Ekle@example.org",
		"leading zwsp":        "\u200Bsparkle@example.org",
		"marker with cf":      m + "\u200B@example.org",
		"marker invalid utf8": m + "\xff@example.org",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := model.ParseEmail(in)
			if !errs.Is(err, errs.Invalid) {
				t.Errorf("ParseEmail: %v", err)
			}
			assertNoEcho(t, err, m)
			err = model.Email(in).Validate()
			if !errs.Is(err, errs.Invalid) {
				t.Errorf("Validate: %v", err)
			}
			assertNoEcho(t, err, m)
			if err != nil && (strings.Contains(err.Error(), "\u200b") || strings.Contains(err.Error(), "\u202e") || strings.Contains(err.Error(), "\xff")) {
				t.Errorf("error echoes input characters")
			}
		})
	}
}

func TestParseErrors_NeverEchoInput(t *testing.T) {
	const marker = "hunter2-LEAK"
	for name, in := range map[string]string{
		"no at":      marker,
		"two ats":    marker + "@a@b",
		"forbidden":  marker + "<@example.org",
		"long local": strings.Repeat("a", 65) + marker + "@example.org",
		"bad domain": marker + "@.example.org",
	} {
		t.Run("email "+name, func(t *testing.T) {
			_, err := model.ParseEmail(in)
			assertNoEcho(t, err, marker)
			assertNoEcho(t, model.Email(in).Validate(), marker)
		})
	}
	_, err := model.ParseSensitivity(marker)
	assertNoEcho(t, err, marker)
	_, err = model.ParseID[model.PersonID](marker)
	assertNoEcho(t, err, marker)
	_, err = model.ParseID[model.PersonID]("0190a1b2-c3d4-4e5f-8a6b-" + marker)
	assertNoEcho(t, err, marker)
}
