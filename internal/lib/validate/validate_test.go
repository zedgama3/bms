package validate_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// checkInvalid asserts err is nil or Invalid wrapping a FieldError naming field.
func checkInvalid(t *testing.T, err error, wantInvalid bool, field string) {
	t.Helper()
	if !wantInvalid {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		return
	}
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	var fe *validate.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("no *FieldError in %v", err)
	}
	if fe.Field != field {
		t.Errorf("Field = %q, want %q", fe.Field, field)
	}
	if fe.Reason == "" {
		t.Error("Reason is empty")
	}
}

func TestFieldError_Error(t *testing.T) {
	e := &validate.FieldError{Field: "db.url", Reason: "must not be empty"}
	if got := e.Error(); got != "db.url: must not be empty" {
		t.Errorf("Error = %q", got)
	}
}

func TestNonEmpty_Blank_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		invalid bool
	}{
		{"empty", "", true},
		{"spaces", "   ", true},
		{"tabs and newlines", "\t\n \r", true},
		{"text", "x", false},
		{"padded text", "  x  ", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checkInvalid(t, validate.NonEmpty("a.b", tc.value), tc.invalid, "a.b")
		})
	}
}

func TestRange_OutOfBounds_ReturnsInvalid(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		tests := []struct {
			v       int
			invalid bool
		}{{0, true}, {1, false}, {500, false}, {1000, false}, {1001, true}, {-5, true}}
		for _, tc := range tests {
			t.Run(fmt.Sprint(tc.v), func(t *testing.T) {
				checkInvalid(t, validate.Range("max_connections", tc.v, 1, 1000), tc.invalid, "max_connections")
			})
		}
	})
	t.Run("float", func(t *testing.T) {
		checkInvalid(t, validate.Range("f", 0.5, 0.0, 1.0), false, "f")
		checkInvalid(t, validate.Range("f", 1.0001, 0.0, 1.0), true, "f")
		checkInvalid(t, validate.Range("f", -0.0001, 0.0, 1.0), true, "f")
	})
	t.Run("duration", func(t *testing.T) {
		checkInvalid(t, validate.Range("d", time.Second, time.Second, time.Minute), false, "d")
		checkInvalid(t, validate.Range("d", time.Minute, time.Second, time.Minute), false, "d")
		checkInvalid(t, validate.Range("d", time.Hour, time.Second, time.Minute), true, "d")
		checkInvalid(t, validate.Range("d", time.Millisecond, time.Second, time.Minute), true, "d")
	})
	t.Run("string", func(t *testing.T) {
		checkInvalid(t, validate.Range("s", "b", "a", "c"), false, "s")
		checkInvalid(t, validate.Range("s", "z", "a", "c"), true, "s")
	})
}

func TestPositive_NonPositive_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		d       time.Duration
		invalid bool
	}{{0, true}, {-1, true}, {-time.Hour, true}, {1, false}, {time.Hour, false}}
	for _, tc := range tests {
		t.Run(tc.d.String(), func(t *testing.T) {
			checkInvalid(t, validate.Positive("interval", tc.d), tc.invalid, "interval")
		})
	}
}

func TestOneOf_NotAllowed_ReturnsInvalid(t *testing.T) {
	allowed := []string{"local", "oidc"}
	tests := []struct {
		value   string
		invalid bool
	}{
		{"local", false}, {"oidc", false},
		{"", true}, {"Local", true}, {"postgres ", true}, {"saml", true}, {"sql", true},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			checkInvalid(t, validate.OneOf("provider", tc.value, allowed...), tc.invalid, "provider")
		})
	}
}

func TestOneOf_NoAllowed_ReturnsInternal(t *testing.T) {
	if err := validate.OneOf("provider", "x"); !errs.Is(err, errs.Internal) {
		t.Fatalf("err = %v, want Internal", err)
	}
	if err := validate.OneOf("provider", ""); !errs.Is(err, errs.Internal) {
		t.Fatalf("empty value: err = %v, want Internal", err)
	}
}

func TestURL_Malformed_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		invalid bool
	}{
		{"https", "https://example.com", false},
		{"with path and port", "http://example.com:8080/a?b=c", false},
		{"ip", "http://10.0.0.1/x", false},
		{"upper scheme", "HTTPS://example.com", false},
		{"empty", "", true},
		{"no scheme", "example.com", true},
		{"host:port looks like scheme", "localhost:8080", true},
		{"relative path", "/just/a/path", true},
		{"scheme only", "https://", true},
		{"empty hostname with port", "http://:8080/hook", true},
		{"no host", "https:///path", true},
		{"missing scheme name", "://example.com", true},
		{"bad escape", "http://exa%zzmple.com", true},
		{"space in host", "http://exa mple.com", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checkInvalid(t, validate.URL("issuer", tc.value), tc.invalid, "issuer")
		})
	}
}

func TestURL_SchemeNotAllowed_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		schemes []string
		invalid bool
	}{
		{"allowed", "https://x.com", []string{"http", "https"}, false},
		{"case-insensitive value", "HTTPS://x.com", []string{"https"}, false},
		{"not allowed", "ftp://x.com", []string{"http", "https"}, true},
		{"none listed allows any", "ftp://x.com", nil, false},
		{"prefix not enough", "https://x.com", []string{"http"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checkInvalid(t, validate.URL("webhook", tc.value, tc.schemes...), tc.invalid, "webhook")
		})
	}
}

func TestHostPort_Malformed_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		value   string
		invalid bool
	}{
		{"db.example.com:1433", false},
		{"localhost:1", false},
		{"10.0.0.1:65535", false},
		{"[::1]:5432", false},
		{"[2001:db8::1]:80", false},
		{"", true},
		{"host", true},
		{"host:", true},
		{":80", true},
		{"host:0", true},
		{"host:65536", true},
		{"host:-1", true},
		{"host:abc", true},
		{"host:https", true},
		{"host:80 ", true},
		{"[::1]", true},
		{"::1:80", true},
		{"a:b:80", true},
		{"[]:80", true},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			checkInvalid(t, validate.HostPort("smtp.host", tc.value), tc.invalid, "smtp.host")
		})
	}
}

func TestListenAddr_BadPort_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		value   string
		invalid bool
	}{
		{":8080", false},
		{"0.0.0.0:80", false},
		{"localhost:8080", false},
		{"[::]:443", false},
		{":65535", false},
		{"", true},
		{"8080", true},
		{"host", true},
		{":", true},
		{":0", true},
		{":65536", true},
		{":http", true},
		{":-1", true},
		{"host:", true},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			checkInvalid(t, validate.ListenAddr("listen", tc.value), tc.invalid, "listen")
		})
	}
}

func TestReason_StringChecks_DoNotEchoValue(t *testing.T) {
	const secret = "s3cr3t-hunter2"
	errsToCheck := map[string]error{
		"NonEmpty":     validate.NonEmpty("f", " \t"), // blank: nothing to echo, still must not panic
		"OneOf":        validate.OneOf("f", secret, "a", "b"),
		"URL scheme":   validate.URL("f", "ftp://u:"+secret+"@host", "https"),
		"URL bad":      validate.URL("f", secret),
		"HostPort":     validate.HostPort("f", secret),
		"HostPort2":    validate.HostPort("f", "host:"+secret),
		"Listen":       validate.ListenAddr("f", secret),
		"Listen2":      validate.ListenAddr("f", ":"+secret),
		"Range string": validate.Range("db.password", secret, "a", "c"),
	}
	for name, err := range errsToCheck {
		t.Run(name, func(t *testing.T) {
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error text echoes the input: %q", err.Error())
			}
			for _, fe := range validate.Fields(err) {
				if strings.Contains(fe.Reason, secret) {
					t.Errorf("Reason echoes the input: %q", fe.Reason)
				}
			}
		})
	}
}

func TestErrorsErr_Empty_ReturnsNil(t *testing.T) {
	var e validate.Errors
	if err := e.Err(); err != nil {
		t.Errorf("Err = %v, want nil", err)
	}
	e.Add(nil)
	if err := e.Err(); err != nil {
		t.Errorf("Err after Add(nil) = %v, want nil", err)
	}
}

func TestErrorsErr_ManyProblems_JoinsAllAsInvalid(t *testing.T) {
	var e validate.Errors
	e.Add(validate.NonEmpty("a", ""))
	e.Add(nil)
	e.Addf("b", "", "must be %d or more, got %d", 3, 1)
	e.Add(validate.Positive("c", -time.Second))
	for i := 0; i < 9; i++ {
		e.Addf(fmt.Sprintf("f%d", i), "", "bad %d", i)
	}
	err := e.Err()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	fs := validate.Fields(err)
	if len(fs) != 12 {
		t.Fatalf("Fields = %d, want 12", len(fs))
	}
	if fs[0].Field != "a" || fs[1].Field != "b" || fs[2].Field != "c" || fs[3].Field != "f0" || fs[11].Field != "f8" {
		t.Errorf("order wrong: %v", fieldNames(fs))
	}
	if fs[1].Reason != "must be 3 or more, got 1" {
		t.Errorf("Addf Reason = %q", fs[1].Reason)
	}
}

func TestErrorsAddf_RecordsInvalidFieldError(t *testing.T) {
	var e validate.Errors
	e.Addf("x.y", "", "bad")
	err := e.Err()
	checkInvalid(t, err, true, "x.y")
}

func TestErrorsErr_OpDefaultAndCustom(t *testing.T) {
	var e validate.Errors
	e.Addf("a", "", "bad")
	var ee *errs.Error
	if !errors.As(e.Err(), &ee) || ee.Op != "validate" {
		t.Errorf("default Op = %+v, want validate", ee)
	}
	e2 := validate.Errors{Op: "config.Validate"}
	e2.Addf("a", "", "bad")
	ee = nil
	if !errors.As(e2.Err(), &ee) || ee.Op != "config.Validate" {
		t.Errorf("custom Op = %+v", ee)
	}
}

func TestErrorsErr_KeepsInnerKindsAndIdentity(t *testing.T) {
	problem := errs.E("x", errs.Unavailable, errors.New("down"))
	var e validate.Errors
	e.Add(problem)
	e.Addf("a", "", "bad")
	err := e.Err()
	if !errs.Is(err, errs.Invalid) {
		t.Errorf("outer kind = %v, want Invalid", errs.KindOf(err))
	}
	if !errors.Is(err, problem) {
		t.Error("errors.Is cannot find the individual problem through the join")
	}
}

func TestFields_JoinedAndWrapped_ReturnsAllInOrder(t *testing.T) {
	a := validate.NonEmpty("a", "")
	b := validate.Positive("b", 0)
	c := validate.Range("c", 9, 1, 2)
	d := validate.OneOf("d", "z", "y")
	tree := errors.Join(
		fmt.Errorf("ctx: %w", a),
		errors.Join(b, errors.New("plain")),
		fmt.Errorf("wrap2: %w", fmt.Errorf("wrap1: %w", c)),
		d,
	)
	got := fieldNames(validate.Fields(tree))
	want := []string{"a", "b", "c", "d"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Fields = %v, want %v", got, want)
	}
	// A bare *FieldError is found too.
	fe := &validate.FieldError{Field: "raw", Reason: "r"}
	if fs := validate.Fields(fmt.Errorf("w: %w", fe)); len(fs) != 1 || fs[0] != fe {
		t.Errorf("bare FieldError not found: %v", fs)
	}
}

func TestFields_NilOrUnmatched_ReturnsEmpty(t *testing.T) {
	if got := validate.Fields(nil); len(got) != 0 {
		t.Errorf("nil: %v", got)
	}
	if got := validate.Fields(errors.New("x")); len(got) != 0 {
		t.Errorf("unmatched: %v", got)
	}
	if got := validate.Fields(errors.Join(errors.New("x"), errs.E("op", errs.Internal, errors.New("y")))); len(got) != 0 {
		t.Errorf("joined unmatched: %v", got)
	}
}

func fieldNames(fs []*validate.FieldError) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Field
	}
	return out
}

// fieldOf extracts the single *FieldError from err, failing the test if it is not Invalid.
func fieldOf(t *testing.T, err error) *validate.FieldError {
	t.Helper()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v, want Invalid", err)
	}
	var fe *validate.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("no *FieldError in %v", err)
	}
	return fe
}

// Covers the pending lib-core amendment: each check sets Code and Params.
func TestChecks_SetCodeAndParams_Table(t *testing.T) {
	const secret = "s3cret-value"
	tests := []struct {
		name       string
		err        error
		checked    string // string input that must not appear in any param value
		wantCode   string
		wantParams map[string]string // nil means Params must be empty
	}{
		{"NonEmpty", validate.NonEmpty("f", "  "), "  ", "required", nil},
		{"Range ints", validate.Range("f", 0, 1, 1000), "", "range", map[string]string{"min": "1", "max": "1000"}},
		{"Range floats", validate.Range("f", 0.5, 1.0, 2.5), "", "range", map[string]string{"min": "1", "max": "2.5"}},
		{"Range durations", validate.Range("f", time.Second, 2*time.Second, time.Minute), "", "range", map[string]string{"min": "2s", "max": "1m0s"}},
		{"Positive", validate.Positive("f", -time.Second), "", "positive", nil},
		{"OneOf", validate.OneOf("f", secret, "local", "oidc"), secret, "one_of", map[string]string{"options": "local, oidc"}},
		{"OneOf single option", validate.OneOf("f", secret, "only"), secret, "one_of", map[string]string{"options": "only"}},
		{"URL with schemes", validate.URL("f", secret, "http", "https"), secret, "url", map[string]string{"schemes": "http, https"}},
		{"URL scheme not allowed", validate.URL("f", "ftp://example.test/x", "https"), "ftp://example.test/x", "url", map[string]string{"schemes": "https"}},
		{"URL no schemes given", validate.URL("f", secret), secret, "url", nil},
		{"HostPort", validate.HostPort("f", secret), secret, "host_port", nil},
		{"ListenAddr", validate.ListenAddr("f", secret), secret, "host_port", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fe := fieldOf(t, tc.err)
			if fe.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", fe.Code, tc.wantCode)
			}
			if len(tc.wantParams) == 0 {
				if len(fe.Params) != 0 {
					t.Errorf("Params = %v, want none", fe.Params)
				}
			} else if len(fe.Params) != len(tc.wantParams) {
				t.Errorf("Params = %v, want %v", fe.Params, tc.wantParams)
			}
			for k, want := range tc.wantParams {
				if got := fe.Params[k]; got != want {
					t.Errorf("Params[%q] = %q, want %q", k, got, want)
				}
			}
			for k, v := range fe.Params {
				if tc.checked != "" && v == tc.checked {
					t.Errorf("Params[%q] equals the checked value", k)
				}
			}
			if strings.Contains(fe.Error(), fe.Code) && fe.Code != "" && strings.HasPrefix(fe.Error(), fe.Code) {
				t.Errorf("Error() = %q should not lead with the code", fe.Error())
			}
		})
	}
}

// Valid inputs return nil, so no code is involved.
func TestChecks_ValidInputs_ReturnNil(t *testing.T) {
	errsList := []error{
		validate.NonEmpty("f", "x"),
		validate.Range("f", 5, 1, 10),
		validate.Positive("f", time.Second),
		validate.OneOf("f", "a", "a", "b"),
		validate.URL("f", "https://example.test", "https"),
		validate.HostPort("f", "example.test:80"),
		validate.ListenAddr("f", ":8080"),
	}
	for i, err := range errsList {
		if err != nil {
			t.Errorf("check %d: err = %v, want nil", i, err)
		}
	}
}

func TestCodes_Pinned(t *testing.T) {
	tests := []struct{ name, got, want string }{
		{"CodeRequired", validate.CodeRequired, "required"},
		{"CodeRange", validate.CodeRange, "range"},
		{"CodePositive", validate.CodePositive, "positive"},
		{"CodeOneOf", validate.CodeOneOf, "one_of"},
		{"CodeURL", validate.CodeURL, "url"},
		{"CodeHostPort", validate.CodeHostPort, "host_port"},
		{"CodeMalformed", validate.CodeMalformed, "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestAddf_SetsCodeNoParams(t *testing.T) {
	tests := []struct {
		name, code string
	}{
		{"builtin code", validate.CodeRange},
		{"caller's own code", "overlap"},
		{"empty code", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var e validate.Errors
			e.Addf("shift.window", tc.code, "must not overlap %d other", 2)
			err := e.Err()
			fe := fieldOf(t, err)
			if fe.Field != "shift.window" {
				t.Errorf("Field = %q", fe.Field)
			}
			if fe.Code != tc.code {
				t.Errorf("Code = %q, want %q", fe.Code, tc.code)
			}
			if len(fe.Params) != 0 {
				t.Errorf("Params = %v, want none", fe.Params)
			}
			if fe.Reason != "must not overlap 2 other" {
				t.Errorf("Reason = %q", fe.Reason)
			}
			if got := fe.Error(); got != "shift.window: must not overlap 2 other" {
				t.Errorf("Error() = %q, want code absent", got)
			}
		})
	}
}
