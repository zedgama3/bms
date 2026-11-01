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
	if _, err := s.Revision(t.Context()); err != nil {
		t.Fatalf("Revision: %v", err)
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
	if _, err := s.Revision(t.Context()); err != nil {
		t.Fatalf("Revision: %v", err)
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

// ---- the generic text check, on a public-URL-shaped key ------------------------------------

// The regional.* rules moved to package regional with their tests (boundaries step 7); what
// stays here is the generic text check of Set, driven through an area named like the
// regional's (settings no longer declares it).
func publicURLArea() Area {
	return Area{Name: "regional", Core: true, Decls: []Decl{{Key: "public_url", Kind: Text}}}
}

// NUL and DEL are refused by the generic text check before any declared rule runs
// (settings.md, owner decision via coordinator): code control_chars, no reason param.
func TestSet_PublicURL_NulAndDel_GenericControlChars(t *testing.T) {
	for name, in := range map[string]string{"NUL": "https://burn.example.test/\x00", "DEL": "https://burn.example.test/\x7f"} {
		t.Run(name, func(t *testing.T) {
			st := newMemStore(1)
			s := newD40Svc(t, st, secret.Secret{}, publicURLArea())
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
