package settings

// D40 (owner, 2026-10-20) tests, written from plan/design/settings.md: F22a (re-saving a
// Secret counts as a change), F23a (the area Check runs over stored rows inside the Write
// transaction), the regional.public_url rule through Set, and the sealing and contrast tests
// that need no database. Every value is invented.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/settings/rows"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const sealKeyText = "an-invented-sealing-key-of-32-bytes-or-more"

// sealStore is a memStore that also keeps sealed rows, like the real table, and records every
// sealed write.
type sealStore struct {
	*memStore
	smu    sync.Mutex
	sealed map[string][]byte
	writes [][]byte
}

func newSealStore(rev int64) *sealStore {
	return &sealStore{memStore: newMemStore(rev), sealed: map[string][]byte{}}
}

func (s *sealStore) UpsertSetting(ctx context.Context, key string, value *string, sealed []byte, now time.Time) (bool, error) {
	if sealed == nil {
		return s.memStore.UpsertSetting(ctx, key, value, nil, now)
	}
	s.smu.Lock()
	defer s.smu.Unlock()
	s.sealed[key] = append([]byte(nil), sealed...)
	s.writes = append(s.writes, append([]byte(nil), sealed...))
	return true, nil
}

func (s *sealStore) DeleteSetting(ctx context.Context, key string) (bool, error) {
	s.smu.Lock()
	_, had := s.sealed[key]
	delete(s.sealed, key)
	s.smu.Unlock()
	h2, err := s.memStore.DeleteSetting(ctx, key)
	return had || h2, err
}

func (s *sealStore) LoadSettings(ctx context.Context) ([]rows.Setting, error) {
	out, err := s.memStore.LoadSettings(ctx)
	s.smu.Lock()
	defer s.smu.Unlock()
	for k, b := range s.sealed {
		out = append(out, rows.Setting{Key: k, Sealed: append([]byte(nil), b...)})
	}
	return out, err
}

type d40Svc struct {
	*Service
	person model.PersonID
	rec    *audittest.Fake
}

func newD40Svc(t *testing.T, st store, key secret.Secret, areas ...Area) *d40Svc {
	t.Helper()
	c := telemetrytest.New(t)
	person := model.NewID[model.PersonID]()
	az := authztest.NewFake()
	actor := func(context.Context) (authz.Actor, bool) { return authz.Actor{Person: person}, true }
	az.SetActor(actor)
	az.Grant(person, PermissionManage)
	rec := audittest.NewFake("settings")
	svc, err := newService(Options{
		DB: dbtest.NewFake(), Clock: clock.NewFake(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)),
		Logger: c.Logger(), Tracer: c.Tracer("settings"), Audit: rec,
		Authz: az, Actor: actor, Areas: areas, SealingKey: key,
	}, st)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return &d40Svc{Service: svc, person: person, rec: rec}
}

func (s *d40Svc) set(t *testing.T, kv ...string) (Revision, error) {
	t.Helper()
	c := Changes{Values: map[string]*string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		v := kv[i+1]
		c.Values[kv[i]] = &v
	}
	rev, err := s.Set(t.Context(), c)
	return rev, err //nolint:wrapcheck // test helper
}

func (s *d40Svc) mustSet(t *testing.T, kv ...string) Revision {
	t.Helper()
	rev, err := s.set(t, kv...)
	if err != nil {
		t.Fatalf("Set(%v): %v", kv, err)
	}
	return rev
}

func (s *d40Svc) mustReset(t *testing.T, key string) {
	t.Helper()
	if _, err := s.Set(t.Context(), Changes{Values: map[string]*string{key: nil}}); err != nil {
		t.Fatalf("Set(reset %s): %v", key, err)
	}
}

func kindOf(t *testing.T, err error, want errs.Kind) {
	t.Helper()
	if !errs.Is(err, want) {
		t.Fatalf("error kind = %v (%v), want %v", errs.KindOf(err), err, want)
	}
}

func mailArea(check func(map[string]string) error) Area {
	return Area{Name: "mail", Decls: []Decl{
		{Key: "host", Kind: Text, Default: ""},
		{Key: "password", Kind: Secret},
	}, Check: check}
}

// ---- F22a ------------------------------------------------------------------------------

// F22a: a Secret saved again with the value it already has is a change: written with a fresh
// nonce, the revision bumped, one audit entry each time, and never the value in the entry.
func TestSet_SecretResave_AlwaysChanges(t *testing.T) {
	st := newSealStore(1)
	s := newD40Svc(t, st, secret.New(sealKeyText), mailArea(nil))
	const plain = "invented-smtp-pass-0001"

	r1 := s.mustSet(t, "mail.password", plain)
	r2 := s.mustSet(t, "mail.password", plain)
	r3 := s.mustSet(t, "mail.password", plain)
	if r1 >= r2 || r2 >= r3 {
		t.Errorf("revisions %d, %d, %d: want strictly increasing on every re-save", r1, r2, r3)
	}
	if len(st.writes) != 3 {
		t.Fatalf("%d sealed writes, want 3", len(st.writes))
	}
	if string(st.writes[0]) == string(st.writes[1]) || string(st.writes[1]) == string(st.writes[2]) {
		t.Error("the same plaintext sealed to identical bytes (nonce reuse)")
	}
	n := 0
	for _, e := range s.rec.Recorded() {
		if string(e.Action) == "settings.setting.changed" {
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d settings.setting.changed entries, want 3", n)
	}
	for _, e := range s.rec.Recorded() {
		if strings.Contains(strings.Join(e.Fields, "|"), plain) {
			t.Error("an audit entry carries the secret")
		}
	}
	got, err := s.mustScoped(t, "mail").Secret(t.Context(), "password")
	if err != nil || got.Value() != plain {
		t.Errorf("Secret after re-save: %v", err)
	}
}

func (s *d40Svc) mustScoped(t *testing.T, area string) Reader {
	t.Helper()
	r, err := s.Scoped(area)
	if err != nil {
		t.Fatalf("Scoped(%s): %v", area, err)
	}
	return r
}

// F22a: neither a declaration's Check nor the area Check can tell whether a Secret is already
// set: the area Check sees "" for every Secret key, before, during and after.
func TestSet_AreaCheck_SeesEmptySecret_SetOrNot(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	check := func(v map[string]string) error {
		mu.Lock()
		defer mu.Unlock()
		pw, ok := v["password"]
		if !ok {
			seen = append(seen, "<missing>")
		} else {
			seen = append(seen, pw)
		}
		return nil
	}
	s := newD40Svc(t, newSealStore(1), secret.New(sealKeyText), mailArea(check))
	s.mustSet(t, "mail.host", "mx.example.test")        // secret unset
	s.mustSet(t, "mail.password", "invented-pass-aaaa") // submitting one
	s.mustSet(t, "mail.host", "mx2.example.test")       // secret now set
	s.mustSet(t, "mail.password", "invented-pass-aaaa") // re-saving it
	s.mustSet(t, "mail.host", "mx3.example.test", "mail.password", "invented-pass-bbbb")
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("the area Check never ran")
	}
	for i, v := range seen {
		if v != "" {
			t.Errorf("call %d: the area Check saw password %q, want \"\" (a Secret's value is always empty)", i, v)
		}
	}
}

// F22a: a Secret's own Check gets the submitted plaintext only and its failure never echoes it.
func TestSet_SecretCheckFailure_NeverEchoesPlaintext(t *testing.T) {
	const plain = "short-invented"
	area := Area{Name: "mail", Decls: []Decl{{Key: "password", Kind: Secret, Check: func(v string) error {
		if len(v) < 20 {
			return textError(v + " is too short")
		}
		return nil
	}}}}
	s := newD40Svc(t, newSealStore(1), secret.New(sealKeyText), area)
	_, err := s.set(t, "mail.password", plain)
	kindOf(t, err, errs.Invalid)
	if strings.Contains(err.Error(), plain) {
		t.Error("the error echoes the secret")
	}
}

type textError string

func (e textError) Error() string { return string(e) }

// ---- F23a ------------------------------------------------------------------------------

func limitsArea() Area {
	return Area{Name: "lim", Decls: []Decl{
		{Key: "low", Kind: Int, Default: "10"},
		{Key: "high", Kind: Int, Default: "100"},
	}, Check: func(v map[string]string) error {
		if atoi(v["high"]) < atoi(v["low"]) {
			return &validate.FieldError{Field: "high", Code: "range", Reason: "high must be at least low"}
		}
		return nil
	}}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// F23a: the cache says low=10 (so high=5 would be refused), but the stored row says low=3
// (another process wrote it and this copy has not polled): the batch is valid against what is
// stored, so it is accepted.
func TestSet_StaleSnapshot_AreaCheckUsesStoredRows(t *testing.T) {
	st := newMemStore(1)
	s := newD40Svc(t, st, secret.Secret{}, limitsArea())
	// load the snapshot: low=10, high=100
	if _, err := s.Regional(t.Context()); err != nil {
		t.Fatalf("Regional: %v", err)
	}
	st.mu.Lock()
	st.values["lim.low"] = "3" // written elsewhere; the revision is unchanged, so no refresh yet
	st.mu.Unlock()

	if _, err := s.set(t, "lim.high", "5"); err != nil {
		t.Fatalf("a batch valid against the stored rows was refused by a stale cache: %v", err)
	}
}

// F23a, the other direction: the cache says low=10 and high=20 would pass, but the stored row
// says low=50, so the area Check refuses it and nothing is written.
func TestSet_StaleSnapshot_StoredRowsStillRefuse(t *testing.T) {
	st := newMemStore(1)
	s := newD40Svc(t, st, secret.Secret{}, limitsArea())
	if _, err := s.Regional(t.Context()); err != nil {
		t.Fatalf("Regional: %v", err)
	}
	st.mu.Lock()
	st.values["lim.low"] = "50"
	st.mu.Unlock()

	_, err := s.set(t, "lim.high", "20")
	kindOf(t, err, errs.Invalid)
	found := false
	for _, fe := range validate.Fields(err) {
		if strings.HasPrefix(fe.Field, "lim.") {
			found = true
		}
	}
	if !found {
		t.Errorf("no field error under lim: %v", err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.values["lim.high"]; ok {
		t.Error("a refused batch was written")
	}
}

// F23a: an area Check that would refuse is not consulted for a batch that touches another
// area.
func TestSet_AreaCheck_RunsOnlyForTouchedAreas(t *testing.T) {
	calls := 0
	other := Area{Name: "other", Decls: []Decl{{Key: "x", Kind: Int, Default: "1"}}, Check: func(map[string]string) error {
		calls++
		return textError("never")
	}}
	s := newD40Svc(t, newMemStore(1), secret.Secret{}, limitsArea(), other)
	s.mustSet(t, "lim.low", "4")
	if calls != 0 {
		t.Errorf("the other area's Check ran %d times for a batch that did not touch it", calls)
	}
}

// ---- regional.public_url through Set ---------------------------------------------------

func urlReasonOf(err error) (code, reason string) {
	for _, fe := range validate.Fields(err) {
		if fe.Field == "regional.public_url" {
			return fe.Code, fe.Params["reason"]
		}
	}
	return "", ""
}

func TestSet_PublicURL_Refusals_Table(t *testing.T) {
	tests := []struct {
		name, in, reason string
	}{
		{"no scheme", "burn.example.test", "scheme"},
		{"ftp", "ftp://burn.example.test", "scheme"},
		{"javascript", "javascript:alert(1)", "scheme"},
		{"scheme-relative", "//burn.example.test", "scheme"},
		{"relative path", "/bms", "scheme"},
		{"no host", "https://", "host"},
		{"no host with path", "https:///bms", "host"},
		{"only a port", "https://:8080", "host"},
		{"port zero", "https://burn.example.test:0", "host"},
		{"port too large", "https://burn.example.test:65536", "host"},
		{"port not a number", "https://burn.example.test:eight", "host"},
		{"userinfo with password", "https://wren:pw@burn.example.test", "userinfo"},
		{"userinfo name only", "https://wren@burn.example.test", "userinfo"},
		{"query", "https://burn.example.test/?a=1", "query"},
		{"empty query", "https://burn.example.test?", "query"},
		{"fragment", "https://burn.example.test/#top", "fragment"},
		{"empty fragment", "https://burn.example.test#", "fragment"},
		{"inner space", "https://burn .example.test", "chars"},
		{"space in path", "https://burn.example.test/a b", "chars"},
		{"tab", "https://burn.example.test/\tx", "chars"},
		{"newline", "https://burn.example.test/\nx", "chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newMemStore(1)
			s := newD40Svc(t, st, secret.Secret{})
			_, err := s.set(t, "regional.public_url", tc.in)
			kindOf(t, err, errs.Invalid)
			code, reason := urlReasonOf(err)
			if code != "url" || reason != tc.reason {
				t.Errorf("code %q reason %q, want url / %s (%v)", code, reason, tc.reason, err)
			}
			if strings.Contains(err.Error(), "wren") || strings.Contains(err.Error(), ":pw@") {
				t.Error("the error echoes the URL's userinfo")
			}
			if _, ok := st.values["regional.public_url"]; ok {
				t.Error("a refused URL was stored")
			}
		})
	}
}

// NUL and DEL are refused by the generic text check before the URL rule runs (settings.md,
// owner decision via coordinator): code control_chars, no reason param.
func TestSet_PublicURL_NulAndDel_GenericControlChars(t *testing.T) {
	for name, in := range map[string]string{"NUL": "https://burn.example.test/\x00", "DEL": "https://burn.example.test/\x7f"} {
		t.Run(name, func(t *testing.T) {
			st := newMemStore(1)
			s := newD40Svc(t, st, secret.Secret{})
			_, err := s.set(t, "regional.public_url", in)
			kindOf(t, err, errs.Invalid)
			code, reason := "", "x"
			for _, fe := range validate.Fields(err) {
				if fe.Field == "regional.public_url" {
					code, reason = fe.Code, fe.Params["reason"]
				}
			}
			if code != "control_chars" || reason != "" {
				t.Errorf("code %q reason %q, want control_chars with no reason", code, reason)
			}
			if _, ok := st.values["regional.public_url"]; ok {
				t.Error("a refused URL was stored")
			}
		})
	}
}

func TestSet_PublicURL_TooLong_Table(t *testing.T) {
	for name, in := range map[string]string{
		"just over": "https://burn.example.test/" + strings.Repeat("a", MaxURLBytes-len("https://burn.example.test/")+1),
		"far over":  "https://burn.example.test/" + strings.Repeat("a", 5000),
	} {
		t.Run(name, func(t *testing.T) {
			s := newD40Svc(t, newMemStore(1), secret.Secret{})
			_, err := s.set(t, "regional.public_url", in)
			kindOf(t, err, errs.Invalid)
			for _, fe := range validate.Fields(err) {
				if fe.Field == "regional.public_url" && fe.Code == "too_long" {
					return
				}
			}
			t.Errorf("no too_long on regional.public_url: %v", err)
		})
	}
}

func TestSet_PublicURL_Canonicalised_Table(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"trailing slash", "https://burn.example.test/", "https://burn.example.test"},
		{"many trailing slashes", "https://burn.example.test////", "https://burn.example.test"},
		{"upper-case scheme", "HTTPS://burn.example.test", "https://burn.example.test"},
		{"upper-case host", "https://BURN.Example.TEST", "https://burn.example.test"},
		{"mixed case everywhere", "HtTp://Burn.EXAMPLE.test:8080/", "http://burn.example.test:8080"},
		{"sub-path kept as given", "https://burn.example.test/Bms/", "https://burn.example.test/Bms"},
		{"surrounding whitespace trimmed", "  https://burn.example.test/  ", "https://burn.example.test"},
		{"port 1", "https://burn.example.test:1", "https://burn.example.test:1"},
		{"port 65535", "https://burn.example.test:65535", "https://burn.example.test:65535"},
		{"already canonical", "http://burn.example.test/bms", "http://burn.example.test/bms"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newMemStore(1)
			s := newD40Svc(t, st, secret.Secret{})
			s.mustSet(t, "regional.public_url", tc.in)
			got, err := s.Regional(t.Context())
			if err != nil {
				t.Fatalf("Regional: %v", err)
			}
			if got.PublicURL != tc.want {
				t.Errorf("PublicURL = %q, want %q", got.PublicURL, tc.want)
			}
			if stored := st.values["regional.public_url"]; stored != tc.want {
				t.Errorf("stored %q, want %q", stored, tc.want)
			}
			// the canonical form is a fixed point: saving it again is a no-op
			rev := st.rev
			s.mustSet(t, "regional.public_url", tc.want)
			if st.rev != rev {
				t.Error("saving the canonical form again bumped the revision")
			}
		})
	}
}

// Authority is taken from the raw text and an IPv6 zone or a percent-encoded host survives
// save then read unchanged (a fixed point of the canonical form).
func TestSet_PublicURL_FixedPoints(t *testing.T) {
	for name, in := range map[string]string{
		"ipv6 zone":            "http://[fe80::1%25eth0]:8080",
		"percent-encoded host": "https://ex%C3%A9mple.org",
	} {
		t.Run(name, func(t *testing.T) {
			st := newMemStore(1)
			s := newD40Svc(t, st, secret.Secret{})
			s.mustSet(t, "regional.public_url", in)
			got, err := s.Regional(t.Context())
			// the host is lower-cased as a whole, so %C3 reads back as %c3: the same bytes
			if want := strings.ToLower(in); err != nil || got.PublicURL != want {
				t.Fatalf("PublicURL = %q, %v; want %q", got.PublicURL, err, want)
			}
			rev := st.rev
			s.mustSet(t, "regional.public_url", got.PublicURL)
			if st.rev != rev {
				t.Error("saving the read-back value again changed it")
			}
		})
	}
}

// A zero-width space (a Unicode format character) is refused as url / chars, in the host, the
// path and at the edges.
func TestSet_PublicURL_FormatCharacters_Refused(t *testing.T) {
	for name, in := range map[string]string{
		"in the host":      "https://burn\u200b.example.test",
		"in the path":      "https://burn.example.test/a\u200bb",
		"after the scheme": "https://\u200bburn.example.test",
		"word joiner":      "https://burn.example.test/\u2060",
	} {
		t.Run(name, func(t *testing.T) {
			s := newD40Svc(t, newMemStore(1), secret.Secret{})
			_, err := s.set(t, "regional.public_url", in)
			kindOf(t, err, errs.Invalid)
			if code, reason := urlReasonOf(err); code != "url" || reason != "chars" {
				t.Errorf("code %q reason %q, want url / chars", code, reason)
			}
		})
	}
}

// A value stored before D40 in a non-canonical but valid form reads back canonical.
func TestRegional_StoredNonCanonicalPublicURL_ReadsCanonical(t *testing.T) {
	st := newMemStore(1)
	st.values["regional.public_url"] = "HTTPS://Burn.Example.test/"
	s := newD40Svc(t, st, secret.Secret{})
	got, err := s.Regional(t.Context())
	if err != nil {
		t.Fatalf("Regional: %v", err)
	}
	if got.PublicURL != "https://burn.example.test" {
		t.Errorf("PublicURL = %q, want the canonical form", got.PublicURL)
	}
}

func TestSet_PublicURL_EmptyIsUnset(t *testing.T) {
	st := newMemStore(1)
	s := newD40Svc(t, st, secret.Secret{})
	s.mustSet(t, "regional.public_url", "https://burn.example.test")
	s.mustSet(t, "regional.public_url", "")
	got, err := s.Regional(t.Context())
	if err != nil || got.PublicURL != "" {
		t.Errorf("PublicURL = %q, %v; want unset", got.PublicURL, err)
	}
}

// A stored value that no longer passes (an upgrade) falls back to the default (decision 5).
func TestRegional_StoredBadPublicURL_FallsBackToDefault(t *testing.T) {
	st := newMemStore(1)
	st.values["regional.public_url"] = "https://wren:pw@burn.example.test/?a=1"
	s := newD40Svc(t, st, secret.Secret{})
	got, err := s.Regional(t.Context())
	if err != nil {
		t.Fatalf("Regional: %v", err)
	}
	if got.PublicURL != "" {
		t.Errorf("PublicURL = %q, want the default", got.PublicURL)
	}
}

// ---- contrast, without a database ------------------------------------------------------

func TestSet_Regional_Contrast_Table(t *testing.T) {
	tests := []struct {
		name, key, value string
		ok               bool
	}{
		{"light ok", "regional.color_primary", "#9a3412", true},
		{"light too pale", "regional.color_primary", "#f97316", false},
		{"light white on white", "regional.color_primary", "#ffffff", false},
		{"light just usable black", "regional.color_primary", "#000000", true},
		{"dark ok", "regional.color_primary_dark", "#f97316", true},
		{"dark too dark", "regional.color_primary_dark", "#1e3a8a", false},
		{"not a colour", "regional.color_primary", "orange", false},
		{"short hex", "regional.color_primary", "#fff", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newD40Svc(t, newMemStore(1), secret.Secret{})
			_, err := s.set(t, tc.key, tc.value)
			if tc.ok {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			kindOf(t, err, errs.Invalid)
			for _, fe := range validate.Fields(err) {
				if fe.Field == tc.key && fe.Code == "contrast" {
					if fe.Params["min"] != "4.5" || fe.Params["ratio"] == "" {
						t.Errorf("params = %v", fe.Params)
					}
				}
			}
		})
	}
}

func TestSet_Regional_ContrastKnownPairs_Unit(t *testing.T) {
	s := newD40Svc(t, newMemStore(1), secret.Secret{})
	// #f97316 on white is about 2.8:1 (the ratio reads down, never up to the minimum).
	_, err := s.set(t, "regional.color_primary", "#f97316")
	var ratio string
	for _, fe := range validate.Fields(err) {
		if fe.Field == "regional.color_primary" && fe.Code == "contrast" {
			ratio = fe.Params["ratio"]
		}
	}
	if !strings.HasPrefix(ratio, "2.") && !strings.HasPrefix(ratio, "3.") {
		t.Errorf("ratio for #f97316 on white = %q, want about 2.8", ratio)
	}
}

func mustPublic(t *testing.T, s *d40Svc) Public {
	t.Helper()
	p, err := s.Public(t.Context())
	if err != nil {
		t.Fatalf("Public: %v", err)
	}
	return p
}

func mustTheme(t *testing.T, s *d40Svc) string {
	t.Helper()
	css, err := s.ThemeCSS(t.Context())
	if err != nil {
		t.Fatalf("ThemeCSS: %v", err)
	}
	return css
}

func TestThemeCSS_Rendering_Unit(t *testing.T) {
	s := newD40Svc(t, newMemStore(1), secret.Secret{})
	ctx := t.Context()
	css, err := s.ThemeCSS(ctx)
	if err != nil || css != "" {
		t.Fatalf("default theme = %q, %v; want empty", css, err)
	}
	if p := mustPublic(t, s); p.Theme {
		t.Error("Public.Theme true with no colours")
	}

	s.mustSet(t, "regional.color_primary", "#9a3412")
	css = mustTheme(t, s)
	if !strings.HasPrefix(css, "@media not (prefers-color-scheme: dark) { :root {") || strings.Contains(css, "@media (prefers-color-scheme: dark)") {
		t.Fatalf("light only: css = %q", css)
	}
	if !strings.Contains(css, "--primary: #9a3412;") || !strings.Contains(css, "--primary-fg: #ffffff;") {
		t.Errorf("light only: css = %q", css)
	}
	if p := mustPublic(t, s); !p.Theme {
		t.Error("Public.Theme false with a colour set")
	}

	s.mustSet(t, "regional.color_primary_dark", "#f97316")
	css = mustTheme(t, s)
	if strings.Count(css, ":root") != 2 || strings.Count(css, "@media (prefers-color-scheme: dark)") != 1 ||
		strings.Count(css, "@media not (prefers-color-scheme: dark)") != 1 {
		t.Fatalf("both: css = %q", css)
	}
	if !strings.Contains(css, "--primary-fg: #1a1a1a;") {
		t.Errorf("dark rule should use the dark foreground: %q", css)
	}

	s.mustReset(t, "regional.color_primary")
	css = mustTheme(t, s)
	if strings.Count(css, "--primary:") != 1 || !strings.Contains(css, "prefers-color-scheme: dark") || strings.Contains(css, "#9a3412") {
		t.Errorf("dark only: css = %q", css)
	}

	s.mustReset(t, "regional.color_primary_dark")
	if css = mustTheme(t, s); css != "" {
		t.Errorf("reset to empty: css = %q", css)
	}
}

// ---- sealing, without a database -------------------------------------------------------

func TestSealer_UnsetKey_IsNilNotAnError(t *testing.T) {
	sl, err := newSealer(secret.Secret{})
	if err != nil || sl != nil {
		t.Errorf("newSealer(unset) = %v, %v; want nil, nil", sl, err)
	}
}

func TestSealer_RoundTrip_FreshNonce_NoPlaintext(t *testing.T) {
	sl, err := newSealer(secret.New(sealKeyText))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	const plain = "invented-smtp-pass-7731"
	a, err := sl.seal("mail.password", plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if len(a) < 12+16+len(plain) {
		t.Errorf("sealed is %d bytes; want nonce (12) + ciphertext + tag (16)", len(a))
	}
	if strings.Contains(string(a), plain) {
		t.Error("the blob holds the plaintext")
	}
	b, err := sl.seal("mail.password", plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(a) == string(b) || string(a[:12]) == string(b[:12]) {
		t.Error("two seals share a nonce")
	}
	for _, blob := range [][]byte{a, b} {
		got, err := sl.open("mail.password", blob)
		if err != nil || got != plain {
			t.Errorf("open = %q, %v", got, err)
		}
	}
	// a second sealer from the same key (another process) opens it
	sl2, err := newSealer(secret.New(sealKeyText))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	if got, err := sl2.open("mail.password", a); err != nil || got != plain {
		t.Errorf("second sealer: %q, %v", got, err)
	}
}

func TestSealer_Open_Refusals_Table(t *testing.T) {
	sl, err := newSealer(secret.New(sealKeyText))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	other, err := newSealer(secret.New("a-different-invented-sealing-key-0000"))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	const plain = "invented-pass-2"
	blob, err := sl.seal("mail.password", plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	flip := func(i int) []byte {
		b := append([]byte(nil), blob...)
		b[i] ^= 0xff
		return b
	}
	tests := []struct {
		name string
		s    *sealer
		key  string
		blob []byte
	}{
		{"wrong key", other, "mail.password", blob},
		{"flipped nonce byte", sl, "mail.password", flip(0)},
		{"flipped ciphertext byte", sl, "mail.password", flip(20)},
		{"flipped tag byte", sl, "mail.password", flip(len(blob) - 1)},
		{"moved to another setting (AAD is the key)", sl, "mail.token", blob},
		{"truncated", sl, "mail.password", blob[:len(blob)-1]},
		{"forty zero bytes", sl, "mail.password", make([]byte, 40)},
		{"shorter than nonce plus tag", sl, "mail.password", blob[:27]},
		{"empty", sl, "mail.password", []byte{}},
		{"nil", sl, "mail.password", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.s.open(tc.key, tc.blob)
			kindOf(t, err, errs.Internal)
			if got != "" {
				t.Errorf("open returned %q alongside an error", got)
			}
			if strings.Contains(err.Error(), plain) {
				t.Error("the error carries the plaintext")
			}
		})
	}
}

// The reader's view of the same refusals: one broken secret is Internal, the others still read.
func TestReaderSecret_BrokenCiphertext_ReturnsInternalOthersUnaffected(t *testing.T) {
	area := Area{Name: "mail", Decls: []Decl{
		{Key: "password", Kind: Secret},
		{Key: "token", Kind: Secret},
	}}
	sl, err := newSealer(secret.New(sealKeyText))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	pw, err := sl.seal("mail.password", "invented-pass-3")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	tok, err := sl.seal("mail.token", "invented-token-3")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	t.Run("a flipped byte", func(t *testing.T) {
		st := newSealStore(1)
		bad := append([]byte(nil), pw...)
		bad[20] ^= 0xff
		st.sealed["mail.password"], st.sealed["mail.token"] = bad, tok
		s := newD40Svc(t, st, secret.New(sealKeyText), area)
		r := s.mustScoped(t, "mail")
		_, err := r.Secret(t.Context(), "password")
		kindOf(t, err, errs.Internal)
		if v, err := r.Secret(t.Context(), "token"); err != nil || v.Value() != "invented-token-3" {
			t.Errorf("token: %v", err)
		}
	})
	t.Run("ciphertext moved to another key", func(t *testing.T) {
		st := newSealStore(1)
		st.sealed["mail.password"] = tok
		s := newD40Svc(t, st, secret.New(sealKeyText), area)
		_, err := s.mustScoped(t, "mail").Secret(t.Context(), "password")
		kindOf(t, err, errs.Internal)
	})
	t.Run("a truncated row", func(t *testing.T) {
		st := newSealStore(1)
		st.sealed["mail.password"] = make([]byte, 40)
		s := newD40Svc(t, st, secret.New(sealKeyText), area)
		_, err := s.mustScoped(t, "mail").Secret(t.Context(), "password")
		kindOf(t, err, errs.Internal)
	})
	t.Run("a different sealing key", func(t *testing.T) {
		st := newSealStore(1)
		st.sealed["mail.password"] = pw
		s := newD40Svc(t, st, secret.New("a-different-invented-sealing-key-0000"), area)
		_, err := s.mustScoped(t, "mail").Secret(t.Context(), "password")
		kindOf(t, err, errs.Internal)
		if strings.Contains(err.Error(), "invented-pass-3") {
			t.Error("the error carries the plaintext")
		}
	})
	t.Run("no sealing key", func(t *testing.T) {
		st := newSealStore(1)
		st.sealed["mail.password"] = pw
		s := newD40Svc(t, st, secret.Secret{}, area)
		_, err := s.mustScoped(t, "mail").Secret(t.Context(), "password")
		kindOf(t, err, errs.Unsupported)
	})
}
