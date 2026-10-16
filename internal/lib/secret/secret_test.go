package secret_test

import (
	"bytes"
	"encoding"
	"encoding/gob"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"text/template"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// leakMarker is a value that cannot occur by accident in any output.
const leakMarker = "ZQ9MARKER7"

var (
	_ fmt.Formatter            = secret.Secret{}
	_ fmt.Stringer             = secret.Secret{}
	_ fmt.GoStringer           = secret.Secret{}
	_ encoding.TextMarshaler   = secret.Secret{}
	_ slog.LogValuer           = secret.Secret{}
	_ encoding.TextUnmarshaler = (*secret.Secret)(nil)
)

type embedsSecret struct {
	Name string
	secret.Secret
}

type holdsSecret struct {
	Name string
	S    secret.Secret
	P    *secret.Secret
	u    secret.Secret
}

type deepHolder struct {
	h holdsSecret
}

func TestNew_ValueAndIsSet(t *testing.T) {
	tests := []struct {
		name string
		v    string
	}{
		{"empty", ""},
		{"one char", "x"},
		{"64KiB", strings.Repeat("a", 64<<10)},
		{"spaces", "  pw  "},
		{"NUL", "a\x00b"},
		{"invalid UTF-8", "a\xff\xfeb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := secret.New(tc.v)
			if s.Value() != tc.v {
				t.Errorf("Value mismatch")
			}
			if s.IsSet() != (len(tc.v) > 0) {
				t.Errorf("IsSet = %v", s.IsSet())
			}
			if tc.v == "" && s != (secret.Secret{}) {
				t.Error("New(\"\") != Secret{}")
			}
		})
	}
}

func TestSecret_ZeroValue_IsUnset(t *testing.T) {
	for name, s := range map[string]secret.Secret{"literal": {}, "NewEmpty": secret.New("")} {
		t.Run(name, func(t *testing.T) {
			if s.IsSet() || s.Value() != "" {
				t.Error("want unset with empty Value")
			}
			for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%d", "%x", "%q"} {
				want := secret.Unset
				if verb == "%q" {
					want = `"` + secret.Unset + `"`
				}
				if got := fmt.Sprintf(verb, s); got != want {
					t.Errorf("Sprintf(%q) = %q, want %q", verb, got, want)
				}
			}
			if s.String() != secret.Unset {
				t.Errorf("String = %q", s.String())
			}
		})
	}
}

func TestSecret_RedactionConstants_AndInterfaces(t *testing.T) {
	if secret.Redacted != "[redacted]" || secret.Unset != "[unset]" {
		t.Fatalf("marker spelling changed: %q %q", secret.Redacted, secret.Unset)
	}
	for _, tc := range []struct {
		name string
		s    secret.Secret
		want string
	}{{"set", secret.New(leakMarker), secret.Redacted}, {"unset", secret.Secret{}, secret.Unset}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.String(); got != tc.want {
				t.Errorf("String = %q", got)
			}
			if got := tc.s.GoString(); got != tc.want {
				t.Errorf("GoString = %q", got)
			}
			b, err := tc.s.MarshalText()
			if err != nil || string(b) != tc.want {
				t.Errorf("MarshalText = %q, %v", b, err)
			}
			if got := tc.s.LogValue(); got.Kind() != slog.KindString || got.String() != tc.want {
				t.Errorf("LogValue = %v (%v)", got, got.Kind())
			}
		})
	}
}

func TestFormat_HonoursWidthPrecisionAndFlags(t *testing.T) {
	// pairs of (verb, equivalent string verb the marker is formatted with).
	pairs := []struct{ verb, via string }{
		{"%v", "%s"}, {"%s", "%s"}, {"%-12v|", "%-12s|"}, {"%12s", "%12s"}, {"%q", "%q"},
		{"%20q", "%20q"}, {"%-20s", "%-20s"}, {"%.3s", "%.3s"}, {"%d", "%s"}, {"%x", "%s"},
		{"%X", "%s"}, {"%08d", "%08s"}, {"% x", "% s"}, {"%#x", "%#s"}, {"%c", "%s"},
		{"%U", "%s"}, {"%t", "%s"}, {"%e", "%s"}, {"%o", "%s"}, {"%b", "%s"},
		{"%#v", "%s"}, {"%+v", "%s"}, {"%z", "%s"},
	}
	for _, sub := range []struct {
		name string
		s    secret.Secret
		str  string
	}{
		{"set", secret.New(leakMarker), "[redacted]"},
		{"unset", secret.Secret{}, "[unset]"},
	} {
		for _, p := range pairs {
			t.Run(sub.name+p.verb, func(t *testing.T) {
				got := fmt.Sprintf(p.verb, sub.s)
				if want := fmt.Sprintf(p.via, sub.str); got != want {
					t.Errorf("Sprintf(%q) = %q, want %q", p.verb, got, want)
				}
				if strings.Contains(got, leakMarker) {
					t.Errorf("leak: %q", got)
				}
			})
		}
	}
	// Spec-literal examples.
	s := secret.New(leakMarker)
	for verb, want := range map[string]string{
		"%-12v|": "[redacted]  |", "%12s": "  [redacted]", "%q": `"[redacted]"`, "%.3s": "[re",
	} {
		if got := fmt.Sprintf(verb, s); got != want {
			t.Errorf("Sprintf(%q) = %q, want %q", verb, got, want)
		}
	}
	if got := fmt.Sprintf("%z", secret.Secret{}); got != "[unset]" {
		t.Errorf("%%z unset = %q", got)
	}
}

var allVerbs = []string{"%s", "%v", "%+v", "%#v", "%q", "%x", "%X", "%d", "%p", "%T", "%z", "%w",
	"%10s", "%-12v", "%.3s", "%08d", "% x", "%#x", "%c", "%U", "%t", "%e", "%o", "%b", "%20q"}

func TestSecret_FormattingNeverLeaks(t *testing.T) {
	s := secret.New(leakMarker)
	emb := embedsSecret{Name: "n", Secret: s}
	hold := holdsSecret{Name: "n", S: s, P: &s, u: s}
	deep := deepHolder{h: hold}
	type subject struct {
		name   string
		v      any
		jsonOK bool
	}
	subjects := []subject{
		{"Secret", s, true}, {"*Secret", &s, true},
		{"embedding", emb, true}, {"*embedding", &emb, true},
		{"holder", hold, true}, {"*holder", &hold, true},
		{"deep", deep, true}, {"*deep", &deep, true},
		{"slice", []secret.Secret{s}, true}, {"array", [1]secret.Secret{s}, true},
		{"map", map[string]secret.Secret{"k": s}, true},
		{"any", any(s), true}, {"reflect.Value", reflect.ValueOf(s), false},
	}
	outputs := map[string]string{}
	for _, sub := range subjects {
		for _, verb := range allVerbs {
			outputs[sub.name+verb] = fmt.Sprintf(verb, sub.v)
		}
		outputs[sub.name+"/sprint"] = fmt.Sprint(sub.v)
		outputs[sub.name+"/sprintln"] = fmt.Sprintln(sub.v)
		outputs[sub.name+"/errorf"] = fmt.Errorf("x: %v", sub.v).Error()
		verbW := "x: %w"
		outputs[sub.name+"/errorf-badw"] = fmt.Errorf(verbW, sub.v).Error()
		outputs[sub.name+"/errorf-w"] = fmt.Errorf("x: %w", fmt.Errorf("%v", sub.v)).Error()
		if sub.jsonOK {
			b, err := json.Marshal(sub.v)
			if err != nil {
				t.Errorf("json.Marshal(%s): %v", sub.name, err)
			}
			outputs[sub.name+"/json"] = string(b)
			b, err = json.MarshalIndent(sub.v, "", "  ")
			if err != nil {
				t.Errorf("json.MarshalIndent(%s): %v", sub.name, err)
			}
			outputs[sub.name+"/jsonindent"] = string(b)
		}
	}
	xb, err := xml.Marshal(hold)
	if err != nil {
		t.Errorf("xml.Marshal: %v", err)
	}
	outputs["xml"] = string(xb)

	for i, src := range []string{"{{.}}", "{{.S}}", `{{printf "%v" .S}}`} {
		tpl, err := template.New("t").Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, hold); err != nil {
			t.Errorf("template %q: %v", src, err)
		}
		outputs[fmt.Sprintf("template%d", i)] = buf.String()
	}

	slogSubjects := map[string]any{"s": s, "ptr": &s, "hold": hold, "holdptr": &hold, "str": s.String()}
	for _, hn := range []string{"json", "text"} {
		var buf bytes.Buffer
		var h slog.Handler
		if hn == "json" {
			h = slog.NewJSONHandler(&buf, nil)
		} else {
			h = slog.NewTextHandler(&buf, nil)
		}
		lg := slog.New(h)
		for k, v := range slogSubjects {
			lg.Info("x", slog.Any(k, v))
		}
		outputs["slog-"+hn] = buf.String()
	}

	for name, out := range outputs {
		if strings.Contains(out, leakMarker) {
			t.Errorf("%s leaks: %s", name, out)
		}
	}
	for _, verb := range []string{"%s", "%v", "%+v", "%#v", "%d", "%q"} {
		if got := fmt.Sprintf(verb, s); !strings.Contains(got, "redacted") {
			t.Errorf("Sprintf(%q) = %q, want the marker", verb, got)
		}
	}
}

func TestFormat_UnexportedField_ShowsAddressNotBytes(t *testing.T) {
	s := secret.New(leakMarker)
	h := holdsSecret{u: s}
	for _, verb := range []string{"%v", "%#v", "%+v"} {
		got := fmt.Sprintf(verb, h)
		if strings.Contains(got, leakMarker) {
			t.Errorf("%s leaks: %q", verb, got)
		}
		if !strings.Contains(got, "0x") {
			t.Errorf("%s = %q, want an address", verb, got)
		}
	}
	got := fmt.Sprintf("%p", s)
	if !strings.HasPrefix(got, "%!p(secret.Secret=") || strings.Contains(got, leakMarker) {
		t.Errorf("%%p = %q", got)
	}
}

func TestLogValue_PointerAndValue_Resolve(t *testing.T) {
	s := secret.New(leakMarker)
	for _, hn := range []string{"json", "text"} {
		for name, v := range map[string]any{"value": s, "pointer": &s} {
			t.Run(hn+"/"+name, func(t *testing.T) {
				var buf bytes.Buffer
				var h slog.Handler
				if hn == "json" {
					h = slog.NewJSONHandler(&buf, nil)
				} else {
					h = slog.NewTextHandler(&buf, nil)
				}
				slog.New(h).Info("m", slog.Any("k", v))
				out := buf.String()
				if !strings.Contains(out, secret.Redacted) || strings.Contains(out, leakMarker) {
					t.Errorf("output = %s", out)
				}
			})
		}
	}
}

func TestUnmarshalText_Verbatim(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"plain", "pw"},
		{"spaces", "  pw  "},
		{"tab", "a\tb"},
		{"unicode", "ünïcödé"},
		{"64KiB", strings.Repeat("z", 64<<10)},
		{"redacted prefix", "[redacted"},
		{"unset prefix", "[unset]x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s secret.Secret
			if err := s.UnmarshalText([]byte(tc.in)); err != nil {
				t.Fatalf("UnmarshalText: %v", err)
			}
			if s.Value() != tc.in {
				t.Errorf("Value mismatch (len %d, want %d)", len(s.Value()), len(tc.in))
			}
			if !s.IsSet() {
				t.Error("IsSet = false")
			}
		})
	}
	t.Run("empty nil", func(t *testing.T) {
		s := secret.New("old")
		if err := s.UnmarshalText(nil); err != nil {
			t.Fatal(err)
		}
		if s != (secret.Secret{}) {
			t.Error("empty text must yield Secret{}")
		}
	})
	t.Run("empty slice", func(t *testing.T) {
		s := secret.New("old")
		if err := s.UnmarshalText([]byte{}); err != nil {
			t.Fatal(err)
		}
		if s != (secret.Secret{}) {
			t.Error("empty text must yield Secret{}")
		}
	})
}

func TestUnmarshalText_CopiesInput(t *testing.T) {
	in := []byte("pw-original")
	var s secret.Secret
	if err := s.UnmarshalText(in); err != nil {
		t.Fatal(err)
	}
	for i := range in {
		in[i] = 'X'
	}
	if s.Value() != "pw-original" {
		t.Errorf("Value changed after input mutated: %q", s.Value())
	}
}

func TestSecret_NilPointerField_NoLeakNoCrash(t *testing.T) {
	h := holdsSecret{Name: "n"} // P is a nil *Secret
	if got := fmt.Sprintf("%v", h.P); got != "<nil>" {
		t.Errorf("fmt %%v of nil *Secret = %q, want <nil>", got)
	}
	b, err := json.Marshal(h.P)
	if err != nil || string(b) != "null" {
		t.Errorf("json of nil *Secret = %q, %v", b, err)
	}
	for _, hn := range []string{"json", "text"} {
		t.Run(hn, func(t *testing.T) {
			var buf bytes.Buffer
			var handler slog.Handler
			if hn == "json" {
				handler = slog.NewJSONHandler(&buf, nil)
			} else {
				handler = slog.NewTextHandler(&buf, nil)
			}
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic: %v", r)
				}
			}()
			slog.New(handler).Info("m", slog.Any("p", h.P), slog.Any("h", h))
			if strings.Contains(buf.String(), leakMarker) {
				t.Errorf("leak: %s", buf.String())
			}
		})
	}
}

func TestUnmarshalText_RedactedMarker_ReturnsInvalid(t *testing.T) {
	s := secret.New("keep")
	err := s.UnmarshalText([]byte(secret.Redacted))
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if s.Value() != "keep" {
		t.Error("receiver changed on failure")
	}
}

func TestUnmarshalText_UnsetMarker_ReturnsInvalid(t *testing.T) {
	s := secret.New("keep")
	err := s.UnmarshalText([]byte(secret.Unset))
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	if s.Value() != "keep" {
		t.Error("receiver changed on failure")
	}
}

func TestUnmarshalText_NilReceiver_ReturnsInternal(t *testing.T) {
	var s *secret.Secret
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("panic: %v", r)
			}
		}()
		err = s.UnmarshalText([]byte("pw"))
	}()
	if err == nil || !errs.Is(err, errs.Internal) {
		t.Fatalf("err = %v, want Internal", err)
	}
	// Markers on a nil receiver must not panic either.
	if err := s.UnmarshalText([]byte(secret.Redacted)); !errs.Is(err, errs.Internal) {
		t.Fatalf("marker on nil receiver: err = %v, want Internal", err)
	}
}

func TestSecret_JSONRoundTrip_FailsInvalid(t *testing.T) {
	for name, orig := range map[string]secret.Secret{"set": secret.New(leakMarker), "unset": {}} {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(holdsSecret{S: orig})
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(b, []byte(leakMarker)) {
				t.Fatalf("leak: %s", b)
			}
			for _, prefill := range []secret.Secret{{}, secret.New("keep")} {
				got := holdsSecret{S: prefill}
				err = json.Unmarshal(b, &got)
				if !errs.Is(err, errs.Invalid) {
					t.Fatalf("err = %v, want Invalid", err)
				}
				if got.S != prefill {
					t.Errorf("target changed: %q", got.S.Value())
				}
			}
		})
	}
}

func TestSecret_JSONNonString_TypeErrorNotCalled(t *testing.T) {
	for _, in := range []string{`{"S":123}`, `{"S":true}`, `{"S":[1]}`, `{"S":{"a":1}}`} {
		t.Run(in, func(t *testing.T) {
			got := holdsSecret{S: secret.New("keep")}
			err := json.Unmarshal([]byte(in), &got)
			var ute *json.UnmarshalTypeError
			if !errors.As(err, &ute) {
				t.Fatalf("err = %T %v, want *json.UnmarshalTypeError", err, err)
			}
			if errs.KindOf(err) != errs.Internal {
				t.Errorf("kind = %v, want Internal", errs.KindOf(err))
			}
			if got.S.Value() != "keep" {
				t.Error("UnmarshalText appears to have been called")
			}
		})
	}
}

func TestSecret_JSONDecode_StringAndMap(t *testing.T) {
	var h holdsSecret
	if err := json.Unmarshal([]byte(`{"S":"  pw  ","P":"x"}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.S.Value() != "  pw  " || h.P == nil || h.P.Value() != "x" {
		t.Errorf("decoded S=%q P=%v", h.S.Value(), h.P)
	}
	var m map[string]secret.Secret
	if err := json.Unmarshal([]byte(`{"k":"v1"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m["k"].Value() != "v1" {
		t.Errorf("map value = %q", m["k"].Value())
	}
	var esc holdsSecret
	if err := json.Unmarshal([]byte(`{"S":"aA\n"}`), &esc); err != nil || esc.S.Value() != "aA\n" {
		t.Errorf("escapes: %q %v", esc.S.Value(), err)
	}
}

func TestSecret_JSONDecode_NullLeavesUnchanged(t *testing.T) {
	keep := holdsSecret{S: secret.New("keep")}
	if err := json.Unmarshal([]byte(`{"S":null}`), &keep); err != nil {
		t.Fatal(err)
	}
	if keep.S.Value() != "keep" {
		t.Error("null changed the field")
	}
	var fresh holdsSecret
	if err := json.Unmarshal([]byte(`{"S":null}`), &fresh); err != nil || fresh.S.IsSet() {
		t.Errorf("fresh: %v set=%v", err, fresh.S.IsSet())
	}
	p := secret.New("x")
	h := holdsSecret{P: &p}
	if err := json.Unmarshal([]byte(`{"P":null}`), &h); err != nil || h.P != nil {
		t.Errorf("pointer: %v %v", err, h.P)
	}
}

func TestSecret_JSONDecode_EmptyStringIsUnset(t *testing.T) {
	h := holdsSecret{S: secret.New("old")}
	if err := json.Unmarshal([]byte(`{"S":""}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.S.IsSet() || h.S != (secret.Secret{}) {
		t.Error("want unset")
	}
}

func TestSecret_JSONEncode_OmitZeroVsOmitEmpty(t *testing.T) {
	type doc struct {
		Z secret.Secret `json:"z,omitzero"`
		E secret.Secret `json:"e,omitempty"`
	}
	b, err := json.Marshal(doc{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"e":"[unset]"`) || strings.Contains(string(b), `"z"`) {
		t.Errorf("json = %s", b)
	}
	b, err = json.Marshal(doc{Z: secret.New(leakMarker)})
	if err != nil || !strings.Contains(string(b), `"z":"[redacted]"`) || strings.Contains(string(b), leakMarker) {
		t.Errorf("set json = %s, %v", b, err)
	}
}

// failState is a fmt.State whose Write always fails.
type failState struct {
	wrote []byte
	flags string
	width int
	prec  int
	hasW  bool
	hasP  bool
}

func (f *failState) Write(b []byte) (int, error) {
	f.wrote = append(f.wrote, b...)
	return 0, errors.New("write failed")
}
func (f *failState) Width() (int, bool)     { return f.width, f.hasW }
func (f *failState) Precision() (int, bool) { return f.prec, f.hasP }
func (f *failState) Flag(c int) bool        { return strings.ContainsRune(f.flags, rune(c)) } //nolint:gosec // fmt flag bytes are ASCII

func TestFormat_WriterFails_NoPanicNoLeak(t *testing.T) {
	s := secret.New(leakMarker)
	for _, verb := range []rune{'v', 's', 'q', 'd', 'x', 'z', 'T', 'p'} {
		for _, st := range []*failState{
			{}, {flags: "+#"}, {flags: "-", width: 20, hasW: true}, {prec: 3, hasP: true},
		} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("verb %c: panic %v", verb, r)
					}
				}()
				s.Format(st, verb)
				if bytes.Contains(st.wrote, []byte(leakMarker)) {
					t.Errorf("verb %c: leak %q", verb, st.wrote)
				}
			}()
		}
	}
}

func TestSecret_Gob_EncodeFailsWithoutLeak(t *testing.T) {
	s := secret.New(leakMarker)
	for name, v := range map[string]any{"secret": s, "holder": holdsSecret{Name: "n", S: s, P: &s}} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			err := gob.NewEncoder(&buf).Encode(v)
			if err == nil {
				t.Error("Encode succeeded, want an error")
			}
			if bytes.Contains(buf.Bytes(), []byte(leakMarker)) {
				t.Error("leak in gob output")
			}
		})
	}
}

func TestSecret_ConcurrentReads(t *testing.T) {
	s := secret.New(leakMarker)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := s
			for range 100 {
				if c.Value() != leakMarker || !c.IsSet() || c.String() != secret.Redacted {
					t.Error("bad read")
					return
				}
				_ = fmt.Sprintf("%v %+v", c, &c)
				if b, err := c.MarshalText(); err != nil || string(b) != secret.Redacted {
					t.Errorf("MarshalText = %q, %v", b, err)
					return
				}
				_ = c.LogValue()
			}
		}()
	}
	wg.Wait()
}
