package httpx_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

const marker = "ZQ9MARKER7"

func newReq(t *testing.T, method string, body io.Reader) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), method, "/x", body)
}

func jsonReq(t *testing.T, body string) *http.Request {
	t.Helper()
	r := newReq(t, http.MethodPost, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) httpx.Problem {
	t.Helper()
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a problem: %v (%q)", err, rec.Body.String())
	}
	return p
}

func TestStatusOf_Table(t *testing.T) {
	tests := []struct {
		kind errs.Kind
		want int
	}{
		{errs.Invalid, 400},
		{errs.Unauthenticated, 401},
		{errs.Permission, 403},
		{errs.NotFound, 404},
		{errs.Conflict, 409},
		{errs.Unavailable, 503},
		{errs.Timeout, 504},
		{errs.Canceled, 503},
		{errs.Unsupported, 501},
		{errs.Internal, 500},
		{errs.Kind(200), 500},
	}
	for _, tc := range tests {
		t.Run(tc.kind.String(), func(t *testing.T) {
			if got := httpx.StatusOf(tc.kind); got != tc.want {
				t.Fatalf("StatusOf(%v) = %d, want %d", tc.kind, got, tc.want)
			}
		})
	}
}

func TestCodes_Pinned(t *testing.T) {
	got := map[string]string{
		httpx.CodeReadOnly:             "read_only",
		httpx.CodeRateLimited:          "rate_limited",
		httpx.CodeShuttingDown:         "shutting_down",
		httpx.CodePayloadTooLarge:      "payload_too_large",
		httpx.CodeUnsupportedMediaType: "unsupported_media_type",
		httpx.CodeCrossOrigin:          "cross_origin",
		httpx.CodeMisdirected:          "misdirected",
		httpx.CodeMethodNotAllowed:     "method_not_allowed",
		httpx.CodeNotFound:             "not_found",
	}
	for have, want := range got {
		if have != want {
			t.Errorf("code %q, want %q", have, want)
		}
	}
	if httpx.ContentTypeProblem != "application/problem+json" {
		t.Errorf("ContentTypeProblem = %q", httpx.ContentTypeProblem)
	}
}

func TestProblem_WireShape(t *testing.T) {
	full, err := json.Marshal(httpx.Problem{
		Status: 400, Code: "invalid", Ref: "AB12-CD34",
		Fields: []httpx.Field{{Field: "age", Code: "range", Params: map[string]string{"min": "1"}, Reason: "r"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(full, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"status", "code", "ref", "fields"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, full)
		}
	}
	fields, ok := m["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("fields = %v", m["fields"])
	}
	f, ok := fields[0].(map[string]any)
	if !ok {
		t.Fatalf("field = %v", fields[0])
	}
	for _, k := range []string{"field", "code", "params", "reason"} {
		if _, ok := f[k]; !ok {
			t.Errorf("missing field key %q", k)
		}
	}

	bare, err := json.Marshal(httpx.Problem{Status: 404, Code: "not_found"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "ref") || strings.Contains(string(bare), "fields") {
		t.Errorf("empty ref and fields must be omitted: %s", bare)
	}
	noParams, err := json.Marshal(httpx.Field{Field: "a", Code: "required", Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(noParams), "params") {
		t.Errorf("empty params must be omitted: %s", noParams)
	}
}

func TestFail_WithoutFailer_WritesProblemFromTable(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"invalid", errs.E("op", errs.Invalid, errors.New("x")), 400, "invalid"},
		{"unauthenticated", errs.E("op", errs.Unauthenticated, errors.New("x")), 401, "unauthenticated"},
		{"permission", errs.E("op", errs.Permission, errors.New("x")), 403, "permission"},
		{"not found", errs.E("op", errs.NotFound, errors.New("x")), 404, "not_found"},
		{"conflict", errs.E("op", errs.Conflict, errors.New("x")), 409, "conflict"},
		{"unavailable", errs.E("op", errs.Unavailable, errors.New("x")), 503, "unavailable"},
		{"timeout", errs.E("op", errs.Timeout, errors.New("x")), 504, "timeout"},
		{"canceled", errs.E("op", errs.Canceled, errors.New("x")), 503, "canceled"},
		{"unsupported", errs.E("op", errs.Unsupported, errors.New("x")), 501, "unsupported"},
		{"internal", errs.E("op", errs.Internal, errors.New("x")), 500, "internal"},
		{"unclassified", errors.New("plain"), 500, "internal"},
		{"max bytes", &http.MaxBytesError{Limit: 10}, 413, "payload_too_large"},
		{"max bytes wrapped", errs.E("op", errs.Internal, &http.MaxBytesError{Limit: 10}), 413, "payload_too_large"},
		{"unsupported media type", errs.E("op", errs.Invalid, httpx.ErrUnsupportedMediaType), 415, "unsupported_media_type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httpx.Fail(rec, newReq(t, http.MethodGet, nil), tc.err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != httpx.ContentTypeProblem {
				t.Fatalf("Content-Type = %q", ct)
			}
			p := decodeProblem(t, rec)
			if p.Status != tc.wantStatus || p.Code != tc.wantCode {
				t.Fatalf("problem = %+v", p)
			}
			if p.Ref != "" {
				t.Fatalf("fallback must not set a ref, got %q", p.Ref)
			}
		})
	}
}

func TestFail_WithoutFailer_InvalidCarriesFieldsInOrder(t *testing.T) {
	acc := validate.Errors{Op: "test"}
	acc.Add(validate.NonEmpty("name", " "))
	acc.Add(validate.Range("age", 9, 1, 3))
	acc.Addf("custom", "", "plain reason")
	rec := httptest.NewRecorder()
	httpx.Fail(rec, newReq(t, http.MethodGet, nil), acc.Err())
	p := decodeProblem(t, rec)
	if p.Code != "invalid" || len(p.Fields) != 3 {
		t.Fatalf("problem = %+v", p)
	}
	want := []string{"name", "age", "custom"}
	for i, f := range p.Fields {
		if f.Field != want[i] || f.Reason == "" {
			t.Errorf("field %d = %+v", i, f)
		}
	}
	if p.Fields[0].Code != "required" || p.Fields[0].Params != nil {
		t.Errorf("name field = %+v", p.Fields[0])
	}
	if p.Fields[1].Code != "range" || p.Fields[1].Params["min"] != "1" || p.Fields[1].Params["max"] != "3" {
		t.Errorf("age field = %+v", p.Fields[1])
	}
	if p.Fields[2].Code != "" {
		t.Errorf("custom field code = %q, want empty", p.Fields[2].Code)
	}
}

// api D31: the fallback also names the fields of a Conflict, and only when it carries some.
func TestFail_WithoutFailer_ConflictFields(t *testing.T) {
	acc := validate.Errors{Op: "test"}
	acc.Addf("email", "taken", "already in use")
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"with a field error", errs.E("test.signup", errs.Conflict, acc.Err()), 1},
		{"plain", errs.E("test.signup", errs.Conflict, errors.New(marker)), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httpx.Fail(rec, newReq(t, http.MethodGet, nil), tc.err)
			p := decodeProblem(t, rec)
			if rec.Code != 409 || p.Code != "conflict" || len(p.Fields) != tc.want {
				t.Fatalf("status %d, problem = %+v", rec.Code, p)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if _, has := raw["fields"]; has != (tc.want > 0) {
				t.Errorf("fields key present = %v", has)
			}
			if tc.want == 1 && (p.Fields[0].Field != "email" || p.Fields[0].Code != "taken") {
				t.Errorf("field = %+v", p.Fields[0])
			}
			if strings.Contains(rec.Body.String(), marker) {
				t.Error("error text in the body")
			}
		})
	}
}

func TestFail_WithoutFailer_NeverCarriesErrorText(t *testing.T) {
	for _, k := range []errs.Kind{errs.Internal, errs.Unavailable, errs.Permission, errs.NotFound} {
		rec := httptest.NewRecorder()
		httpx.Fail(rec, newReq(t, http.MethodGet, nil),
			errs.E("op.secret", k, errors.New("dial tcp "+marker+": refused")))
		body := rec.Body.String()
		if strings.Contains(body, marker) || strings.Contains(body, "op.secret") {
			t.Errorf("%v: body leaks error text: %s", k, body)
		}
	}
}

type recFailer struct {
	calls int
	err   error
}

func (f *recFailer) Fail(w http.ResponseWriter, _ *http.Request, err error) {
	f.calls++
	f.err = err
	w.WriteHeader(http.StatusTeapot)
}

func TestFail_WithFailer_Delegates(t *testing.T) {
	f := &recFailer{}
	r := newReq(t, http.MethodGet, nil)
	r = r.WithContext(httpx.WithFailer(r.Context(), f))
	rec := httptest.NewRecorder()
	want := errs.E("op", errs.Conflict, errors.New("x"))
	httpx.Fail(rec, r, want)
	if f.calls != 1 || !errors.Is(f.err, want) {
		t.Fatalf("calls = %d, err = %v", f.calls, f.err)
	}
	if rec.Code != http.StatusTeapot || rec.Body.Len() != 0 {
		t.Fatalf("Fail wrote on its own: %d %q", rec.Code, rec.Body.String())
	}
}

func TestFail_NilErr_500(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.Fail(rec, newReq(t, http.MethodGet, nil), nil)
	if rec.Code != 500 {
		t.Fatalf("status = %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "internal" || p.Status != 500 {
		t.Fatalf("problem = %+v", p)
	}
}

type payload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestReadJSON_HappyPath_IgnoresUnknownFields(t *testing.T) {
	var dst payload
	err := httpx.ReadJSON(jsonReq(t, `{"name":"Ada","count":3,"extra":{"a":[1]}}`), &dst)
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if dst.Name != "Ada" || dst.Count != 3 {
		t.Fatalf("dst = %+v", dst)
	}
}

func TestReadJSON_ContentTypeWithParams(t *testing.T) {
	for _, ct := range []string{"application/json; charset=utf-8", "Application/JSON", "application/json;charset=UTF-8"} {
		r := newReq(t, http.MethodPost, strings.NewReader(`{"name":"Ada"}`))
		r.Header.Set("Content-Type", ct)
		var dst payload
		if err := httpx.ReadJSON(r, &dst); err != nil {
			t.Errorf("%q: %v", ct, err)
		}
	}
}

func TestReadJSON_NotJSONContentType_ReturnsUnsupportedMediaType(t *testing.T) {
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/jsonx", "multipart/form-data; boundary=x"} {
		t.Run(ct, func(t *testing.T) {
			r := newReq(t, http.MethodPost, strings.NewReader(`{"name":"Ada"}`))
			if ct != "" {
				r.Header.Set("Content-Type", ct)
			}
			var dst payload
			err := httpx.ReadJSON(r, &dst)
			if !errs.Is(err, errs.Invalid) || !errors.Is(err, httpx.ErrUnsupportedMediaType) {
				t.Fatalf("err = %v", err)
			}
			rec := httptest.NewRecorder()
			httpx.Fail(rec, r, err)
			if rec.Code != 415 {
				t.Fatalf("Fail status = %d, want 415", rec.Code)
			}
		})
	}
}

func TestReadJSON_Malformed_ReturnsInvalidWithoutEcho(t *testing.T) {
	tests := []struct{ name, body string }{
		{"not json", `{not json ` + marker + `}`},
		{"wrong type", `{"count":"` + marker + `"}`},
		{"trailing data", `{"name":"a"} {"name":"` + marker + `"}`},
		{"wrong top-level", `["` + marker + `"]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var dst payload
			err := httpx.ReadJSON(jsonReq(t, tc.body), &dst)
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("err = %v, want Invalid", err)
			}
			if errors.Is(err, httpx.ErrUnsupportedMediaType) {
				t.Fatal("must not be 415")
			}
			fes := validate.Fields(err)
			if len(fes) == 0 {
				t.Fatal("no FieldError")
			}
			for _, fe := range fes {
				if fe.Field != "body" && !strings.HasPrefix(fe.Field, "body.") {
					t.Errorf("field = %q", fe.Field)
				}
				if fe.Code != "malformed" || fe.Params != nil {
					t.Errorf("code/params = %q %v", fe.Code, fe.Params)
				}
				if strings.Contains(fe.Reason, marker) || strings.Contains(fe.Field, marker) {
					t.Errorf("FieldError echoes the body: %+v", fe)
				}
			}
			if strings.Contains(err.Error(), marker) {
				t.Errorf("error text echoes the body: %v", err)
			}
		})
	}
}

func TestReadJSON_MapKeyNotInFieldPath(t *testing.T) {
	var dst struct {
		Prefs map[string]int `json:"prefs"`
	}
	err := httpx.ReadJSON(jsonReq(t, `{"prefs":{"`+marker+`":"x"}}`), &dst)
	fes := validate.Fields(err)
	if !errs.Is(err, errs.Invalid) || len(fes) != 1 {
		t.Fatalf("err = %v", err)
	}
	if fes[0].Field != "body.prefs" {
		t.Fatalf("field = %q, want body.prefs", fes[0].Field)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("error text carries the map key: %v", err)
	}
}

func TestReadJSON_SliceIndexNotInFieldPath(t *testing.T) {
	var dst struct {
		Items []int `json:"items"`
	}
	err := httpx.ReadJSON(jsonReq(t, `{"items":[1,"x"]}`), &dst)
	fes := validate.Fields(err)
	if len(fes) != 1 || fes[0].Field != "body.items" {
		t.Fatalf("fields = %+v (err %v)", fes, err)
	}
}

func TestReadJSON_EmptyBody_ReturnsInvalid(t *testing.T) {
	for name, body := range map[string]io.Reader{"empty": strings.NewReader(""), "nil": nil, "no body": http.NoBody} {
		t.Run(name, func(t *testing.T) {
			r := newReq(t, http.MethodPost, body)
			r.Header.Set("Content-Type", "application/json")
			var dst payload
			err := httpx.ReadJSON(r, &dst)
			fes := validate.Fields(err)
			if !errs.Is(err, errs.Invalid) || len(fes) != 1 {
				t.Fatalf("err = %v", err)
			}
			if fes[0].Field != "body" || fes[0].Code != "required" {
				t.Fatalf("field = %+v", fes[0])
			}
		})
	}
}

func TestReadJSON_OverCap_ReturnsMaxBytesError(t *testing.T) {
	rec := httptest.NewRecorder()
	r := jsonReq(t, `{"name":"`+strings.Repeat("a", 100)+`"}`)
	r.Body = http.MaxBytesReader(rec, r.Body, 16)
	var dst payload
	err := httpx.ReadJSON(r, &dst)
	var mbe *http.MaxBytesError
	if !errors.As(err, &mbe) {
		t.Fatalf("err = %v, want *http.MaxBytesError", err)
	}
	out := httptest.NewRecorder()
	httpx.Fail(out, r, err)
	if out.Code != 413 {
		t.Fatalf("Fail status = %d", out.Code)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

type failingBody struct {
	prefix string
	err    error
	read   bool
}

func (b *failingBody) Read(p []byte) (int, error) {
	if !b.read && b.prefix != "" {
		b.read = true
		return copy(p, b.prefix), nil
	}
	return 0, b.err
}

func (*failingBody) Close() error { return nil }

func TestReadJSON_ReadDeadline_ReturnsTimeout(t *testing.T) {
	for name, cause := range map[string]error{"net.Error": timeoutError{}, "os deadline": os.ErrDeadlineExceeded} {
		t.Run(name, func(t *testing.T) {
			r := newReq(t, http.MethodPost, nil)
			r.Header.Set("Content-Type", "application/json")
			r.Body = &failingBody{prefix: `{"name":`, err: cause}
			var dst payload
			if err := httpx.ReadJSON(r, &dst); !errs.Is(err, errs.Timeout) {
				t.Fatalf("err = %v, want Timeout", err)
			}
		})
	}
}

func TestReadJSON_ContextCanceled_ReturnsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := newReq(t, http.MethodPost, nil).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Body = &failingBody{err: errors.New("read aborted")}
	var dst payload
	if err := httpx.ReadJSON(r, &dst); !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
}

func TestReadJSON_ContextDeadline_ReturnsTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()
	<-ctx.Done()
	r := newReq(t, http.MethodPost, nil).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Body = &failingBody{err: errors.New("read aborted")}
	var dst payload
	if err := httpx.ReadJSON(r, &dst); !errs.Is(err, errs.Timeout) {
		t.Fatalf("err = %v, want Timeout", err)
	}
}

func TestReadJSON_UnexpectedEOF_ReturnsCanceled(t *testing.T) {
	for name, cause := range map[string]error{"unexpected EOF": io.ErrUnexpectedEOF, "reset": errors.New("connection reset by peer")} {
		t.Run(name, func(t *testing.T) {
			r := newReq(t, http.MethodPost, nil)
			r.Header.Set("Content-Type", "application/json")
			r.Body = &failingBody{prefix: `{"name":"A`, err: cause}
			var dst payload
			if err := httpx.ReadJSON(r, &dst); !errs.Is(err, errs.Canceled) {
				t.Fatalf("err = %v, want Canceled", err)
			}
		})
	}
}

func TestReadJSON_BadDst_ReturnsInternal(t *testing.T) {
	var nilPtr *payload
	tests := []struct {
		name string
		dst  any
	}{
		{"nil", nil},
		{"not a pointer", payload{}},
		{"typed nil pointer", nilPtr},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := httpx.ReadJSON(jsonReq(t, `{"name":"a"}`), tc.dst)
			if !errs.Is(err, errs.Internal) {
				t.Fatalf("err = %v, want Internal", err)
			}
		})
	}
}

func TestWriteJSON_HappyPath(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := httpx.WriteJSON(rec, http.StatusCreated, payload{Name: "Ada", Count: 2}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var got payload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Name != "Ada" || got.Count != 2 {
		t.Fatalf("body = %q (%v)", rec.Body.String(), err)
	}
}

func TestWriteJSON_204NoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := httpx.WriteJSON(rec, http.StatusNoContent, nil); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if rec.Code != 204 || rec.Body.Len() != 0 {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestWriteJSON_Unencodable_ReturnsInternalWritesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	err := httpx.WriteJSON(rec, http.StatusOK, map[string]any{"c": make(chan int)})
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("err = %v, want Internal", err)
	}
	if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" || rec.Flushed {
		t.Fatalf("something was written: %q %v", rec.Body.String(), rec.Header())
	}
	// The handler can still fail cleanly afterwards.
	httpx.Fail(rec, newReq(t, http.MethodGet, nil), err)
	if rec.Code != 500 {
		t.Fatalf("Fail after unencodable: status %d", rec.Code)
	}
}

type failWriter struct {
	h    http.Header
	err  error
	code int
}

func (w *failWriter) Header() http.Header {
	if w.h == nil {
		w.h = http.Header{}
	}
	return w.h
}
func (w *failWriter) WriteHeader(c int)         { w.code = c }
func (w *failWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteJSON_WriteFails_ReturnsCanceled(t *testing.T) {
	cause := errors.New("broken pipe")
	err := httpx.WriteJSON(&failWriter{err: cause}, http.StatusOK, payload{Name: "a"})
	if !errs.Is(err, errs.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("err does not wrap the write error: %v", err)
	}
}

func TestCtxAccessors_RoundTripAndAbsent(t *testing.T) {
	ctx := t.Context()
	if _, ok := httpx.ClientAddrFrom(ctx); ok {
		t.Error("ClientAddrFrom on a bare ctx must report false")
	}
	if s, ok := httpx.SchemeFrom(ctx); ok || s != "" {
		t.Errorf("SchemeFrom on a bare ctx = %q, %v", s, ok)
	}
	want := netip.MustParseAddr("203.0.113.9")
	ctx = httpx.WithScheme(httpx.WithClientAddr(ctx, want), "https")
	if got, ok := httpx.ClientAddrFrom(ctx); !ok || got != want {
		t.Errorf("ClientAddrFrom = %v, %v", got, ok)
	}
	if got, ok := httpx.SchemeFrom(ctx); !ok || got != "https" {
		t.Errorf("SchemeFrom = %q, %v", got, ok)
	}
}

type textFailer struct{}

func (*textFailer) UnmarshalText([]byte) error {
	return errors.New("bad value " + marker)
}

func TestReadJSON_UnmarshalTextFails_FieldPathMalformedNoEcho(t *testing.T) {
	var dst struct {
		ID textFailer `json:"id"`
	}
	err := httpx.ReadJSON(jsonReq(t, `{"id":"`+marker+`"}`), &dst)
	fes := validate.Fields(err)
	if !errs.Is(err, errs.Invalid) || len(fes) != 1 {
		t.Fatalf("err = %v", err)
	}
	if fes[0].Field != "body.id" || fes[0].Code != "malformed" || fes[0].Params != nil {
		t.Fatalf("field = %+v", fes[0])
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(fes[0].Reason, marker) {
		t.Fatalf("input or cause echoed: %v", err)
	}
}
