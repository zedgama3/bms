package regional_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// stubReader is a settings.Reader that answers from the embedded Reader, except where a test
// overrides one key's text or makes a key (or every key) fail.
type stubReader struct {
	settings.Reader
	text    map[string]string // key -> text returned instead of the Reader's
	failKey string            // "" none; a bare key; "*" every key
	err     error
}

func (s stubReader) failing(key string) error {
	if s.err != nil && (s.failKey == "*" || s.failKey == key) {
		return s.err
	}
	return nil
}

func (s stubReader) Text(ctx context.Context, key string) (string, error) {
	if err := s.failing(key); err != nil {
		return "", err
	}
	if v, ok := s.text[key]; ok {
		return v, nil
	}
	return s.Reader.Text(ctx, key) //nolint:wrapcheck // test double
}

func (s stubReader) Bool(ctx context.Context, key string) (bool, error) {
	if err := s.failing(key); err != nil {
		return false, err
	}
	return s.Reader.Bool(ctx, key) //nolint:wrapcheck // test double
}

func (s stubReader) Int(ctx context.Context, key string) (int64, error) {
	if err := s.failing(key); err != nil {
		return 0, err
	}
	return s.Reader.Int(ctx, key) //nolint:wrapcheck // test double
}

func (s stubReader) Duration(ctx context.Context, key string) (time.Duration, error) {
	if err := s.failing(key); err != nil {
		return 0, err
	}
	return s.Reader.Duration(ctx, key) //nolint:wrapcheck // test double
}

func (s stubReader) Secret(ctx context.Context, key string) (secret.Secret, error) {
	if err := s.failing(key); err != nil {
		return secret.Secret{}, err
	}
	return s.Reader.Secret(ctx, key) //nolint:wrapcheck // test double
}

// regionalKeys are the seven bare keys of area regional (boundaries, "regional").
var regionalKeys = []string{"name", "contact_email", "public_url", "language", "timezone", "color_primary", "color_primary_dark"}

func TestSettings_DeclaresTheSevenKeys(t *testing.T) {
	var got []string
	for _, d := range regional.Settings {
		got = append(got, d.Key)
	}
	if strings.Join(got, ",") != strings.Join(regionalKeys, ",") {
		t.Errorf("regional.Settings keys = %v, want %v", got, regionalKeys)
	}
	if err := area().Validate(); err != nil {
		t.Errorf("the regional area does not validate: %v", err)
	}
}

// Profile reads all seven keys, each into its own field.
func TestProfile_ReadsSevenKeys(t *testing.T) {
	r := newRig(t)
	for k, v := range map[string]string{
		"name":               "Prairie Dog Burn",
		"contact_email":      "hello@burn.example.test",
		"public_url":         "https://burn.example.test",
		"language":           "es",
		"timezone":           "America/Denver",
		"color_primary":      "#9a3412",
		"color_primary_dark": "#f97316",
	} {
		r.rd.Set(k, v)
	}
	got, err := r.svc.Profile(t.Context())
	wantNoErr(t, err)
	want := regional.Profile{
		Name: "Prairie Dog Burn", ContactEmail: "hello@burn.example.test", PublicURL: "https://burn.example.test",
		Language: "es", Timezone: "America/Denver", ColorPrimary: "#9a3412", ColorPrimaryDark: "#f97316",
	}
	if got != want {
		t.Errorf("Profile = %+v, want %+v", got, want)
	}
}

// Nothing set: the declared defaults, not zero values (the name is "BMS", the language "en").
func TestProfile_NothingSet_IsTheDeclaredDefaults(t *testing.T) {
	r := newRig(t)
	got, err := r.svc.Profile(t.Context())
	wantNoErr(t, err)
	want := regional.Profile{Name: "BMS", Language: "en"}
	if got != want {
		t.Errorf("Profile = %+v, want %+v", got, want)
	}
}

// B10: a key that fails to read fails Profile with the Reader's kind, whichever key it is.
func TestProfile_ReaderFails_KeepsTheKind(t *testing.T) {
	kinds := []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled, errs.Invalid, errs.Internal}
	for _, key := range append([]string{"*"}, regionalKeys...) {
		for _, kind := range kinds {
			t.Run(key+"/"+kind.String(), func(t *testing.T) {
				r := newRig(t, func(o *regional.Options) {
					o.Settings = stubReader{Reader: r0(t), failKey: key, err: errs.E("test.reader", kind, errors.New("reader down"))}
				})
				got, err := r.svc.Profile(t.Context())
				wantKind(t, err, kind)
				if got != (regional.Profile{}) {
					t.Errorf("Profile = %+v alongside an error", got)
				}
			})
		}
	}
}

// r0 is a fresh Reader for the "regional" area over the declarations.
func r0(t testing.TB) settings.Reader {
	t.Helper()
	return settingstest.NewFake("regional", regional.Settings)
}

// A cancelled context reaches the Reader: the Reader's Canceled comes back, not a default.
func TestProfile_CancelledContext_ReturnsCanceledWhenTheReaderDoes(t *testing.T) {
	r := newRig(t, func(o *regional.Options) {
		o.Settings = stubReader{Reader: r0(t), failKey: "*", err: errs.E("test.reader", errs.Canceled, context.Canceled)}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := r.svc.Profile(ctx)
	wantKind(t, err, errs.Canceled)
}

// A value stored before the canonical form existed, valid but not canonical, reads canonical
// (moved from settings: TestRegional_StoredNonCanonicalPublicURL_ReadsCanonical).
func TestRegional_StoredNonCanonicalPublicURL_ReadsCanonical(t *testing.T) {
	r := newRig(t)
	r.rd.Set("public_url", "HTTPS://Burn.Example.test/")
	got, err := r.svc.Profile(t.Context())
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got.PublicURL != "https://burn.example.test" {
		t.Errorf("PublicURL = %q, want the canonical form", got.PublicURL)
	}
}

// A stored value that no longer passes (an upgrade) reads as unset, the default, with a
// warning (decision 5; moved from settings).
func TestRegional_StoredBadPublicURL_FallsBackToDefault(t *testing.T) {
	r := newRig(t, func(o *regional.Options) {
		o.Settings = stubReader{Reader: r0(t), text: map[string]string{"public_url": "https://wren:pw@burn.example.test/?a=1"}} //nolint:gosec // G101: an invented URL with userinfo, the very input the rule must refuse
	})
	got, err := r.svc.Profile(t.Context())
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got.PublicURL != "" {
		t.Errorf("PublicURL = %q, want the default", got.PublicURL)
	}
	for _, rec := range r.cap.Records() {
		if strings.Contains(rec.Message, "wren") || strings.Contains(rec.Message, ":pw@") {
			t.Errorf("a log record echoes the stored URL's userinfo: %q", rec.Message)
		}
	}
}
