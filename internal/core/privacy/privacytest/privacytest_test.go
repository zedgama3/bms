package privacytest_test

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
)

// recT is a testing.TB that records failures instead of failing the real test. FailNow
// and Fatal* stop the calling goroutine like the real one.
type recT struct {
	testing.TB
	mu     sync.Mutex
	failed bool
	msgs   []string
}

func (r *recT) Helper() {}
func (r *recT) record(args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
	r.msgs = append(r.msgs, "failure")
	_ = args
}
func (r *recT) Error(args ...any)         { r.record(args...) }
func (r *recT) Errorf(f string, _ ...any) { r.record(f) }
func (r *recT) Fail()                     { r.record() }
func (r *recT) FailNow()                  { r.record(); runtime.Goexit() }
func (r *recT) Fatal(args ...any)         { r.record(args...); runtime.Goexit() }
func (r *recT) Fatalf(f string, _ ...any) { r.record(f); runtime.Goexit() }
func (r *recT) Log(_ ...any)              {}
func (r *recT) Logf(_ string, _ ...any)   {}

// run calls f with a recording T and reports whether it failed.
func run(f func(t testing.TB)) bool {
	r := &recT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(r)
	}()
	<-done
	return r.failed
}

func TestAssertHidden_FindsNested(t *testing.T) { // F38
	tests := []struct {
		name  string
		body  string
		names []string
		fails bool
	}{
		{"absent", `{"id":1,"name":"x"}`, []string{"phone"}, false},
		{"top level present", `{"id":1,"phone":"1"}`, []string{"phone"}, true},
		{"nested object", `{"id":1,"lead":{"phone":"1"}}`, []string{"phone"}, true},
		{"inside array of objects", `{"entries":[{"id":1},{"id":2,"phone":null}]}`, []string{"phone"}, true},
		{"array of arrays", `{"x":[[{"phone":"1"}]]}`, []string{"phone"}, true},
		{"top level array body", `[{"phone":"1"}]`, []string{"phone"}, true},
		{"deep", `{"a":{"b":{"c":{"phone":""}}}}`, []string{"phone"}, true},
		{"as a value, not a key", `{"label":"phone","list":["phone"]}`, []string{"phone"}, false},
		{"one of several names", `{"a":{"dx":1}}`, []string{"phone", "dx"}, true},
		{"none of several names", `{"a":{"id":1}}`, []string{"phone", "dx"}, false},
		{"no names", `{"phone":1}`, nil, false},
		{"empty object", `{}`, []string{"phone"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := run(func(tb testing.TB) { privacytest.AssertHidden(tb, []byte(tc.body), tc.names...) })
			if got != tc.fails {
				t.Fatalf("failed = %v, want %v", got, tc.fails)
			}
		})
	}
}

func TestAssertHidden_NotJSON_Fails(t *testing.T) {
	for _, body := range []string{``, `not json`, `{"a":`} {
		if !run(func(tb testing.TB) { privacytest.AssertHidden(tb, []byte(body), "phone") }) {
			t.Errorf("body %q: a body that is not JSON cannot show a field is hidden", body)
		}
	}
}

func TestAssertPresent_TopLevelOnly(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		names []string
		fails bool
	}{
		{"present", `{"id":1,"phone":"1"}`, []string{"id", "phone"}, false},
		{"absent", `{"id":1}`, []string{"phone"}, true},
		{"only nested", `{"lead":{"phone":"1"}}`, []string{"phone"}, true},
		{"one of several missing", `{"id":1}`, []string{"id", "phone"}, true},
		{"present with empty value", `{"phone":""}`, []string{"phone"}, false},
		{"no names", `{}`, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := run(func(tb testing.TB) { privacytest.AssertPresent(tb, []byte(tc.body), tc.names...) })
			if got != tc.fails {
				t.Fatalf("failed = %v, want %v", got, tc.fails)
			}
		})
	}
	if !run(func(tb testing.TB) { privacytest.AssertPresent(tb, []byte("nope"), "a") }) {
		t.Error("a body that is not JSON must fail AssertPresent")
	}
}

func TestAssertHidden_MessageNamesFieldNotBody(t *testing.T) {
	// The helper must not echo the body (it may hold personal data) in its failure output.
	var out []string
	r := &msgT{out: &out}
	done := make(chan struct{})
	go func() {
		defer close(done)
		privacytest.AssertHidden(r, []byte(`{"phone":"marker-quillfeather-0773"}`), "phone")
	}()
	<-done
	if !r.failed {
		t.Fatal("want a failure")
	}
	if strings.Contains(strings.Join(out, "\n"), "marker-quillfeather-0773") {
		t.Error("the failure message echoes a value from the body")
	}
}

type msgT struct {
	testing.TB
	out    *[]string
	failed bool
}

func (m *msgT) Helper()                   {}
func (m *msgT) add(s string)              { m.failed = true; *m.out = append(*m.out, s) }
func (m *msgT) Error(a ...any)            { m.add(fmtAny(a...)) }
func (m *msgT) Errorf(f string, a ...any) { m.add(fmtf(f, a...)) }
func (m *msgT) Fatal(a ...any)            { m.add(fmtAny(a...)); runtime.Goexit() }
func (m *msgT) Fatalf(f string, a ...any) { m.add(fmtf(f, a...)); runtime.Goexit() }
func (m *msgT) Fail()                     { m.failed = true }
func (m *msgT) FailNow()                  { m.failed = true; runtime.Goexit() }
func (m *msgT) Log(_ ...any)              {}
func (m *msgT) Logf(_ string, _ ...any)   {}

func fmtAny(a ...any) string         { return fmt.Sprint(a...) }
func fmtf(f string, a ...any) string { return fmt.Sprintf(f, a...) }
