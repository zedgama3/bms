package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/api"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// errRig mounts one route, GET /api/alpha/e, that fails with whatever *cur holds.
func errRig(t *testing.T, cur *error, access module.Access, mut ...func(*api.Options)) *rig {
	t.Helper()
	set := setOf(t, "alpha", route("GET", "/e", access, func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, *cur)
	}))
	return newRig(t, append([]func(*api.Options){withRoutes(set)}, mut...)...)
}

func TestMapper_KindTable(t *testing.T) {
	tests := []struct {
		kind   errs.Kind
		status int
		code   string
	}{
		{errs.Invalid, 400, "invalid"},
		{errs.Unauthenticated, 401, "unauthenticated"},
		{errs.Permission, 403, "permission"},
		{errs.NotFound, 404, "not_found"},
		{errs.Conflict, 409, "conflict"},
		{errs.Unavailable, 503, "unavailable"},
		{errs.Timeout, 504, "timeout"},
		{errs.Unsupported, 501, "unsupported"},
		{errs.Internal, 500, "internal"},
	}
	var cur error
	r := errRig(t, &cur, module.AccessPublic)
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			cur = errs.E("alpha.op", tc.kind, errors.New("inner"))
			rec := r.do("GET", "/api/alpha/e")
			p := wantProblem(t, rec, tc.status, tc.code)
			if p.Ref == "" || telemetry.Ref(p.Ref).Validate() != nil {
				t.Errorf("ref = %q", p.Ref)
			}
			if rec.Header().Get("X-Trace-Id") == "" {
				t.Error("no X-Trace-Id")
			}
			if (tc.kind == errs.Unavailable) != (rec.Header().Get("Retry-After") == "5") {
				t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
			}
		})
	}
	t.Run("unclassified", func(t *testing.T) {
		cur = errors.New("plain")
		wantProblem(t, r.do("GET", "/api/alpha/e"), 500, "internal")
	})
	t.Run("wrapped keeps kind", func(t *testing.T) {
		cur = fmt.Errorf("more context: %w", errs.E("op", errs.Conflict, errors.New("dup")))
		wantProblem(t, r.do("GET", "/api/alpha/e"), 409, "conflict")
	})
	t.Run("nil", func(t *testing.T) {
		cur = nil
		wantProblem(t, r.do("GET", "/api/alpha/e"), 500, "internal")
	})
}

func TestMapper_Sentinels(t *testing.T) {
	var cur error
	r := errRig(t, &cur, module.AccessPublic)
	t.Run("read only", func(t *testing.T) {
		cur = fmt.Errorf("save: %w", errs.E("db.Exec", errs.Unavailable, db.ErrReadOnly))
		wantProblem(t, r.do("GET", "/api/alpha/e"), 503, httpx.CodeReadOnly)
	})
	t.Run("read only bare", func(t *testing.T) {
		cur = db.ErrReadOnly
		wantProblem(t, r.do("GET", "/api/alpha/e"), 503, httpx.CodeReadOnly)
	})
	t.Run("rate limited", func(t *testing.T) {
		cur = errs.E("intake.Accept", errs.Unavailable, intake.ErrRateLimited)
		rec := r.do("GET", "/api/alpha/e")
		wantProblem(t, rec, 429, httpx.CodeRateLimited)
		if got := rec.Header().Get("Retry-After"); got != "60" {
			t.Errorf("Retry-After = %q, want 60", got)
		}
	})
	t.Run("unsupported media type", func(t *testing.T) {
		cur = errs.E("x", errs.Invalid, httpx.ErrUnsupportedMediaType)
		wantProblem(t, r.do("GET", "/api/alpha/e"), 415, httpx.CodeUnsupportedMediaType)
	})
	t.Run("max bytes", func(t *testing.T) {
		cur = fmt.Errorf("read: %w", &http.MaxBytesError{Limit: 5})
		wantProblem(t, r.do("GET", "/api/alpha/e"), 413, httpx.CodePayloadTooLarge)
	})
}

func TestMapper_ReadOnly_503ReadOnlyCode(t *testing.T) {
	cur := error(errs.E("op", errs.Unavailable, db.ErrReadOnly))
	r := errRig(t, &cur, module.AccessPublic)
	r.db.set(nil, true)
	rec := r.do("GET", "/api/alpha/e")
	wantProblem(t, rec, 503, "read_only")
	if rec.Header().Get("X-Read-Only") != "true" {
		t.Error("no X-Read-Only on the refusal")
	}
}

func TestMapper_RateLimited_429RetryAfter(t *testing.T) {
	cur := error(intake.ErrRateLimited)
	r := errRig(t, &cur, module.AccessPublic)
	rec := r.do("GET", "/api/alpha/e")
	wantProblem(t, rec, 429, "rate_limited")
	if rec.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
}

func TestMapper_UnavailableRetryAfter(t *testing.T) {
	cur := error(errs.E("op", errs.Unavailable, errors.New("down")))
	r := errRig(t, &cur, module.AccessPublic)
	if got := r.do("GET", "/api/alpha/e").Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want 5", got)
	}
}

func TestMapper_Timeout_504(t *testing.T) {
	cur := error(errs.E("op", errs.Timeout, context.DeadlineExceeded))
	r := errRig(t, &cur, module.AccessPublic)
	wantProblem(t, r.do("GET", "/api/alpha/e"), 504, "timeout")
}

func TestMapper_FieldsFromValidateErrors(t *testing.T) {
	acc := validate.Errors{Op: "alpha.create"}
	acc.Add(validate.NonEmpty("name", ""))
	acc.Add(validate.Range("shift.hours", 30, 1, 24))
	acc.Addf("custom", "", "needs attention")
	cur := error(acc.Err())
	r := errRig(t, &cur, module.AccessPublic)
	rec := r.do("GET", "/api/alpha/e")
	p := wantProblem(t, rec, 400, "invalid")
	if len(p.Fields) != 3 {
		t.Fatalf("fields = %+v", p.Fields)
	}
	f := p.Fields
	if f[0].Field != "name" || f[0].Code != "required" || f[0].Reason == "" || f[0].Params != nil {
		t.Errorf("field 0 = %+v", f[0])
	}
	if f[1].Field != "shift.hours" || f[1].Code != "range" || f[1].Params["min"] != "1" || f[1].Params["max"] != "24" || f[1].Reason == "" {
		t.Errorf("field 1 = %+v", f[1])
	}
	if f[2].Field != "custom" || f[2].Code != "" || f[2].Reason != "needs attention" {
		t.Errorf("field 2 = %+v", f[2])
	}
	raw := arr(t, decodeMap(t, rec.Body.Bytes())["fields"])
	if _, has := obj(t, raw[0])["params"]; has {
		t.Error("params must be absent when the check set none")
	}
	if _, has := obj(t, raw[1])["params"]; !has {
		t.Error("params missing for range")
	}
}

func TestMapper_FieldsOnlyForInvalidAndConflict(t *testing.T) {
	acc := validate.Errors{}
	acc.Addf("x", "required", "r")
	// A permission error that happens to carry a FieldError must not leak it.
	cur := error(errs.E("op", errs.Permission, acc.Err()))
	r := errRig(t, &cur, module.AccessPublic)
	if p := wantProblem(t, r.do("GET", "/api/alpha/e"), 403, "permission"); len(p.Fields) != 0 {
		t.Fatalf("fields on a non-invalid problem: %+v", p.Fields)
	}
}

// F40, D31: a Conflict carrying field errors names its fields, as Invalid does.
func TestMapper_Conflict_WithFieldErrors_EmitsFields(t *testing.T) {
	acc := validate.Errors{Op: "alpha.signup"}
	acc.Addf("email", "taken", "already in use")
	cur := error(errs.E("alpha.signup", errs.Conflict, acc.Err()))
	r := errRig(t, &cur, module.AccessPublic)
	rec := r.do("GET", "/api/alpha/e")
	p := wantProblem(t, rec, 409, "conflict")
	if len(p.Fields) != 1 || p.Fields[0].Field != "email" || p.Fields[0].Code != "taken" {
		t.Fatalf("fields = %+v", p.Fields)
	}
	if _, has := decodeMap(t, rec.Body.Bytes())["fields"]; !has {
		t.Error("no fields key in the body")
	}
}

// A Conflict without field errors has no fields key, and wrapping with %w keeps them.
func TestMapper_Conflict_Table(t *testing.T) {
	acc := validate.Errors{}
	acc.Addf("email", "taken", "r")
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"plain conflict", errs.E("op", errs.Conflict, errors.New("dup")), 0},
		{"fields", errs.E("op", errs.Conflict, acc.Err()), 1},
		{"fields wrapped with %w", fmt.Errorf("ctx: %w", errs.E("op", errs.Conflict, acc.Err())), 1},
	}
	var cur error
	r := errRig(t, &cur, module.AccessPublic)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cur = tc.err
			rec := r.do("GET", "/api/alpha/e")
			p := wantProblem(t, rec, 409, "conflict")
			if len(p.Fields) != tc.want {
				t.Fatalf("fields = %+v, want %d", p.Fields, tc.want)
			}
			if _, has := decodeMap(t, rec.Body.Bytes())["fields"]; has != (tc.want > 0) {
				t.Errorf("fields key present = %v", has)
			}
			if strings.Contains(rec.Body.String(), "dup") {
				t.Error("error text in the body")
			}
		})
	}
}

func TestMapper_BodyNeverCarriesErrorText(t *testing.T) {
	var cur error
	r := errRig(t, &cur, module.AccessPublic)
	for _, k := range []errs.Kind{errs.Internal, errs.Unavailable, errs.Permission, errs.NotFound, errs.Invalid, errs.Conflict, errs.Timeout} {
		cur = errs.E("alpha.secretop", k, fmt.Errorf("dial tcp %s %s: refused", marker, emailMarker))
		rec := r.do("GET", "/api/alpha/e")
		all := rec.Body.String() + fmt.Sprint(rec.Header())
		for _, leak := range []string{marker, emailMarker, "secretop", "dial tcp", "refused"} {
			if strings.Contains(all, leak) {
				t.Errorf("%v: response leaks %q: %s", k, leak, all)
			}
		}
	}
}

func TestMapper_ErrorMessage_5xxOnlyAndScrubbed(t *testing.T) {
	var cur error
	r := errRig(t, &cur, module.AccessPublic)
	cur = errs.E("alpha.load", errs.Internal, fmt.Errorf("lookup for %s failed", emailMarker))
	r.do("GET", "/api/alpha/e")
	recs := errorRecords(r.cap)
	if len(recs) != 1 {
		t.Fatalf("records = %+v", recs)
	}
	if recs[0].Attrs["error.message"] == "" {
		t.Error("a 5xx record must carry error.message")
	}
	if r.cap.Contains(emailMarker) {
		t.Error("an unscrubbed e-mail reached the log")
	}

	cur = errs.E("alpha.load", errs.Invalid, fmt.Errorf("value %s is wrong", marker))
	r.do("GET", "/api/alpha/e")
	recs = errorRecords(r.cap)
	if len(recs) != 2 {
		t.Fatalf("records = %+v", recs)
	}
	if v, has := recs[1].Attrs["error.message"]; has {
		t.Errorf("a 4xx record carries error.message %q", v)
	}
	for _, l := range r.cap.Records() {
		if l.Message == marker || strings.Contains(l.Message, marker) {
			t.Errorf("a 4xx message reached the log: %+v", l)
		}
		for k, v := range l.Attrs {
			if strings.Contains(v, marker) {
				t.Errorf("a 4xx message reached log attr %s", k)
			}
		}
	}
}

func TestMapper_LogLevel_Table(t *testing.T) {
	person := newPerson()
	tests := []struct {
		name   string
		err    error
		access module.Access
		auth   api.Authenticator
		route  string
		want   [2]int // wantLevel, wantStatus
	}{
		{"5xx error", errs.E("op", errs.Internal, errors.New("x")), module.AccessPublic, nil, "", [2]int{int(slogError), 500}},
		{"5xx anonymous still error", errs.E("op", errs.Unavailable, errors.New("x")), module.AccessPublic, nil, "", [2]int{int(slogError), 503}},
		{"signed-in 4xx warn", errs.E("op", errs.Permission, errors.New("x")), module.AccessSignedIn, signedIn(person), "", [2]int{int(slogWarn), 403}},
		{"signed-in 404 warn", errs.E("op", errs.NotFound, errors.New("x")), module.AccessPublic, signedIn(person), "", [2]int{int(slogWarn), 404}},
		{"anonymous 4xx debug", errs.E("op", errs.NotFound, errors.New("x")), module.AccessPublic, nil, "", [2]int{int(slogDebug), 404}},
		{"anonymous invalid debug", errs.E("op", errs.Invalid, errors.New("x")), module.AccessPublic, nil, "", [2]int{int(slogDebug), 400}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cur := tc.err
			var mut []func(*api.Options)
			if tc.auth != nil {
				mut = append(mut, withAuth(tc.auth))
			}
			r := errRig(t, &cur, tc.access, mut...)
			rec := r.do("GET", "/api/alpha/e")
			if rec.Code != tc.want[1] {
				t.Fatalf("status = %d", rec.Code)
			}
			recs := errorRecords(r.cap)
			if len(recs) != 1 {
				t.Fatalf("want one record per error response, got %+v", recs)
			}
			if int(recs[0].Level) != tc.want[0] {
				t.Errorf("level = %v, want %v", recs[0].Level, tc.want[0])
			}
		})
	}
}

func TestMapper_ErrorLogRecordFields(t *testing.T) {
	person := newPerson()
	cur := error(errs.E("alpha.load", errs.Internal, errors.New("broke")))
	r := errRig(t, &cur, module.AccessSignedIn, withAuth(signedIn(person)))
	rec := r.do("GET", "/api/alpha/e")
	p := wantProblem(t, rec, 500, "internal")
	recs := errorRecords(r.cap)
	if len(recs) != 1 {
		t.Fatalf("records = %+v", recs)
	}
	a := recs[0].Attrs
	want := map[string]string{
		"error_ref":           p.Ref,
		"error.kind":          "internal",
		"op":                  "alpha.load",
		"http.route":          "/api/alpha/e",
		"http.request.method": "GET",
		"status":              "500",
		"person_id":           person.String(),
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("attr %s = %q, want %q", k, a[k], v)
		}
	}
	if recs[0].TraceID == "" && a["trace_id"] == "" {
		t.Error("no trace_id")
	}
	if a["error.message"] == "" {
		t.Error("5xx without error.message")
	}
}

func TestMapper_CanceledClientGone_WritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	set := setOf(t, "alpha", route("GET", "/c", module.AccessPublic, func(w http.ResponseWriter, r *http.Request) {
		cancel()
		httpx.Fail(w, r, errs.E("op", errs.Canceled, context.Canceled))
	}))
	r := newRig(t, withRoutes(set))
	req := newRequest(t, "GET", "/api/alpha/c").WithContext(ctx)
	rec := newRecorder()
	r.server().Handler().ServeHTTP(rec, req)
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Fatalf("something was written: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	if s, ok := spanNamed(r.cap, "GET /api/alpha/c"); !ok || s.Failed {
		t.Errorf("span = %+v, want ended and not failed", s)
	}
	for _, l := range r.cap.Records() {
		if l.Level > slogDebug {
			t.Errorf("client-gone logged above debug: %+v", l)
		}
	}
}

func TestMapper_CanceledWhileDraining_503ShuttingDown(t *testing.T) {
	cur := error(errs.E("op", errs.Canceled, context.Canceled))
	r := errRig(t, &cur, module.AccessPublic)
	srv := r.server()
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	rec := r.do("GET", "/api/alpha/e")
	wantProblem(t, rec, 503, httpx.CodeShuttingDown)
	if !strings.EqualFold(rec.Header().Get("Connection"), "close") {
		t.Errorf("Connection = %q, want close", rec.Header().Get("Connection"))
	}
}

func TestFail_AfterHeader_LogsOnly(t *testing.T) {
	set := setOf(t, "alpha", route("GET", "/late", module.AccessPublic, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("partial")); err != nil {
			return
		}
		httpx.Fail(w, r, errs.E("alpha.late", errs.Internal, errors.New("too late")))
	}))
	r := newRig(t, withRoutes(set))
	rec := r.do("GET", "/api/alpha/late")
	if rec.Code != 200 || rec.Body.String() != "partial" {
		t.Fatalf("response changed: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if n := len(recordsWith(r.cap, slogError)); n != 1 {
		t.Errorf("error records = %d, want 1", n)
	}
}
