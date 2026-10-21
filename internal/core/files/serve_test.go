package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/model"
)

// Serve (files.md decision 12, "Serving", F42, F43). It needs no database and no store: it
// gets a File and a Content, as Open would hand them to an owner's route.

type content struct {
	*bytes.Reader
	closed bool
}

func (c *content) Close() error { c.closed = true; return nil }

func serveFixture(level model.Sensitivity, ct, name string, data []byte) (files.File, *content) {
	f := files.File{
		ID: model.NewID[files.FileID](), Record: qualRef(), Level: level, Name: name,
		ContentType: ct, Size: int64(len(data)), SHA256: sha256.Sum256(data),
		CreatedAt: time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC),
	}
	return f, &content{Reader: bytes.NewReader(data)}
}

func doServe(f files.File, c files.Content, mut ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/files/x", nil)
	for _, m := range mut {
		m(req)
	}
	rec := httptest.NewRecorder()
	files.Serve(rec, req, f, c)
	return rec
}

func TestServe_Headers(t *testing.T) {
	pdf := pdfDoc("")
	png := plainPNG(t)
	tests := []struct {
		name  string
		level model.Sensitivity
		ct    string
		data  []byte
	}{
		{"public png", model.SensitivityPublic, "image/png", png},
		{"internal png", model.SensitivityInternal, "image/png", png},
		{"personal pdf", model.SensitivityPersonal, "application/pdf", pdf},
		{"sensitive pdf", model.SensitivitySensitive, "application/pdf", pdf},
		{"sensitive jpeg", model.SensitivitySensitive, "image/jpeg", plainJPEG(t)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, c := serveFixture(tc.level, tc.ct, "file.bin", tc.data)
			rec := doServe(f, c)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			h := rec.Header()
			if got := h.Get("Content-Type"); got != tc.ct {
				t.Errorf("Content-Type = %q, want %q", got, tc.ct)
			}
			if got := h.Get("Content-Security-Policy"); got != "default-src 'none'" {
				t.Errorf("Content-Security-Policy = %q", got)
			}
			if got := h.Get("Cache-Control"); got != "" {
				t.Errorf("Cache-Control = %q; Serve sets none at any level (decision 25)", got)
			}
			sum := sha256.Sum256(tc.data)
			if got := h.Get("ETag"); got != `"`+hex.EncodeToString(sum[:])+`"` {
				t.Errorf("ETag = %q, want the quoted SHA-256", got)
			}
			if got := h.Get("Last-Modified"); got != f.CreatedAt.UTC().Format(http.TimeFormat) {
				t.Errorf("Last-Modified = %q, want CreatedAt", got)
			}
			if got := h.Get("Content-Length"); got != strconv.Itoa(len(tc.data)) {
				t.Errorf("Content-Length = %q", got)
			}
			if !bytes.Equal(rec.Body.Bytes(), tc.data) {
				t.Error("body differs")
			}
			if h.Get("Location") != "" {
				t.Error("Serve must never redirect to a store URL")
			}
			for k := range h {
				if strings.HasPrefix(strings.ToLower(k), "access-control-") {
					t.Errorf("header %s set; files never adds CORS (api D6)", k)
				}
			}
			if c.closed {
				t.Error("Serve closed the Content; the caller closes it")
			}
		})
	}
}

func TestServe_ContentDisposition_InlineWithSafeFilename(t *testing.T) {
	names := []string{
		"certificate.pdf",
		`Zoë "Q" Quillfeather; cert.pdf`,
		"naïve – résumé.pdf",
		"semi;colon,comma.pdf",
		"日本語.pdf",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f, c := serveFixture(model.SensitivityPersonal, "application/pdf", name, pdfDoc(""))
			rec := doServe(f, c)
			cd := rec.Header().Get("Content-Disposition")
			if cd == "" {
				t.Fatal("no Content-Disposition")
			}
			if strings.ContainsAny(cd, "\r\n") {
				t.Fatalf("Content-Disposition holds a line break: %q", cd)
			}
			disp, params, err := mime.ParseMediaType(cd)
			if err != nil {
				t.Fatalf("ParseMediaType(%q): %v", cd, err)
			}
			if disp != "inline" {
				t.Errorf("disposition = %q, want inline for a PDF", disp)
			}
			if got := params["filename"]; got != name {
				// ParseMediaType prefers filename* when present; either way it must round-trip.
				t.Errorf("filename = %q, want %q", got, name)
			}
			if !strings.Contains(cd, "filename*=UTF-8''") && !isASCII(name) {
				t.Errorf("no RFC 5987 filename* for a non-ASCII name: %q", cd)
			}
			// An ASCII fallback is always offered.
			if !strings.Contains(strings.ReplaceAll(cd, "filename*=", ""), "filename=") {
				t.Errorf("no ASCII filename= fallback: %q", cd)
			}
		})
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func TestServe_ImagesAndPDF_AreInline(t *testing.T) {
	for _, k := range []struct{ ct, name string }{{"image/png", "a.png"}, {"image/jpeg", "a.jpg"}, {"image/gif", "a.gif"}, {"image/webp", "a.webp"}, {"application/pdf", "a.pdf"}} {
		f, c := serveFixture(model.SensitivityInternal, k.ct, k.name, []byte("data"))
		disp, _, err := mime.ParseMediaType(doServe(f, c).Header().Get("Content-Disposition"))
		if err != nil || disp != "inline" {
			t.Errorf("%s: disposition = %q, %v; want inline", k.ct, disp, err)
		}
	}
}

// No inline HTML, ever: the stored type is whatever Open says, and the CSP stops
// a browser from running a mislabelled file.
func TestServe_HTMLContent_IsNeverServedAsHTML(t *testing.T) {
	f, c := serveFixture(model.SensitivityInternal, "image/png", "evil.png", refusedBodies["html named png"])
	rec := doServe(f, c)
	h := rec.Header()
	if strings.Contains(strings.ToLower(h.Get("Content-Type")), "html") {
		t.Errorf("Content-Type = %q", h.Get("Content-Type"))
	}
	if h.Get("Content-Security-Policy") != "default-src 'none'" {
		t.Errorf("missing CSP: %v", h)
	}
}

func TestServe_ConditionalRequests(t *testing.T) {
	data := pdfDoc("")
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`

	t.Run("If-None-Match matching gives 304 with no body", func(t *testing.T) {
		f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", data)
		rec := doServe(f, c, func(r *http.Request) { r.Header.Set("If-None-Match", etag) })
		if rec.Code != http.StatusNotModified {
			t.Fatalf("status = %d, want 304", rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("304 carried %d body bytes", rec.Body.Len())
		}
	})
	t.Run("If-None-Match other gives 200", func(t *testing.T) {
		f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", data)
		rec := doServe(f, c, func(r *http.Request) { r.Header.Set("If-None-Match", `"deadbeef"`) })
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
	t.Run("If-Modified-Since at CreatedAt gives 304", func(t *testing.T) {
		f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", data)
		since := f.CreatedAt.UTC().Format(http.TimeFormat)
		rec := doServe(f, c, func(r *http.Request) { r.Header.Set("If-Modified-Since", since) })
		if rec.Code != http.StatusNotModified {
			t.Fatalf("status = %d, want 304", rec.Code)
		}
	})
}

func TestServe_Range_Returns206(t *testing.T) {
	data := pdfOfSize(t, 4096)
	f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", data)
	rec := doServe(f, c, func(r *http.Request) { r.Header.Set("Range", "bytes=10-19") })
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 10-19/4096" {
		t.Errorf("Content-Range = %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), data[10:20]) {
		t.Error("range body differs")
	}
	if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("ETag") == "" {
		t.Error("a 206 lost its safety headers")
	}
}

func TestServe_SuffixRange_Returns206(t *testing.T) {
	data := pdfOfSize(t, 4096)
	f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", data)
	rec := doServe(f, c, func(r *http.Request) { r.Header.Set("Range", "bytes=-16") })
	if rec.Code != http.StatusPartialContent || !bytes.Equal(rec.Body.Bytes(), data[len(data)-16:]) {
		t.Errorf("status %d, body %d bytes", rec.Code, rec.Body.Len())
	}
}

// F42
func TestServe_BadRange_416(t *testing.T) {
	data := pdfOfSize(t, 4096)
	f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", data)
	rec := doServe(f, c, func(r *http.Request) { r.Header.Set("Range", "bytes=999999-") })
	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes */4096" {
		t.Errorf("Content-Range = %q", got)
	}
}

func TestServe_HeadRequest_NoBody(t *testing.T) {
	f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", pdfDoc(""))
	rec := doServe(f, c, func(r *http.Request) { r.Method = http.MethodHead })
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD: status %d, %d body bytes", rec.Code, rec.Body.Len())
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Error("HEAD lost Content-Length")
	}
}

// writeDeadlineRecorder is a ResponseWriter a http.ResponseController can set a write
// deadline on.
type writeDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *writeDeadlineRecorder) SetWriteDeadline(t time.Time) error {
	w.deadlines = append(w.deadlines, t)
	return nil
}

func TestServe_ExtendsTheWriteDeadlineToServeTimeout(t *testing.T) {
	f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", pdfDoc(""))
	w := &writeDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now()
	files.Serve(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/f", nil), f, c)
	after := time.Now()
	if len(w.deadlines) == 0 {
		t.Fatal("Serve did not extend the write deadline (a phone on a venue link outlives the shell's 30 s)")
	}
	d := w.deadlines[len(w.deadlines)-1]
	if d.Before(before.Add(files.ServeTimeout-time.Second)) || d.After(after.Add(files.ServeTimeout+time.Second)) {
		t.Errorf("deadline = %v, want now + %v", d, files.ServeTimeout)
	}
}

func TestServe_WriterWithoutDeadlineSupport_StillServes(t *testing.T) {
	f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", pdfDoc(""))
	rec := doServe(f, c) // httptest.ResponseRecorder has no SetWriteDeadline
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; Serve never fails", rec.Code)
	}
}

// failingWriter accepts a few bytes and then reports the client gone.
type failingWriter struct {
	header     http.Header
	status     int
	okBudget   int
	written    int
	callsAfter int
	failedOnce bool
}

func (w *failingWriter) Header() http.Header  { return w.header }
func (w *failingWriter) WriteHeader(code int) { w.status = code }
func (w *failingWriter) Write(p []byte) (int, error) {
	if w.failedOnce {
		w.callsAfter++
		return 0, errors.New("client gone")
	}
	if w.written+len(p) > w.okBudget {
		n := w.okBudget - w.written
		w.written += n
		w.failedOnce = true
		return n, errors.New("client gone")
	}
	w.written += len(p)
	return len(p), nil
}

// F43
func TestServe_ClientGone_Stops(t *testing.T) {
	big := pdfOfSize(t, 4<<20)
	f, c := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", big)
	w := &failingWriter{header: http.Header{}, okBudget: 1024}
	done := make(chan struct{})
	go func() {
		defer close(done)
		files.Serve(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/f", nil), f, c)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the client went away")
	}
	if w.callsAfter != 0 {
		t.Errorf("Serve kept writing %d times after a write failed", w.callsAfter)
	}
	if c.closed {
		t.Error("Serve closed the Content")
	}
}

// Serve of a body the reader cannot finish does not panic.
type brokenContent struct{}

func (brokenContent) Read([]byte) (int, error)       { return 0, errors.New("disk error") }
func (brokenContent) Seek(int64, int) (int64, error) { return 0, errors.New("disk error") }
func (brokenContent) Close() error                   { return nil }

func TestServe_UnreadableContent_DoesNotPanic(t *testing.T) {
	f, _ := serveFixture(model.SensitivityInternal, "application/pdf", "a.pdf", pdfDoc(""))
	var rec *httptest.ResponseRecorder
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Serve panicked: %v", r)
			}
		}()
		rec = doServe(f, brokenContent{})
	}()
	_ = io.Discard
	_ = rec
}

// Decision 25: Serve sets no Cache-Control; the recorded response carries only what the caller
// set (the shell's no-store stands).
func TestServe_SetsNoCacheControl(t *testing.T) {
	for _, lvl := range []model.Sensitivity{model.SensitivityPublic, model.SensitivityInternal, model.SensitivityPersonal, model.SensitivitySensitive} {
		f, c := serveFixture(lvl, "application/pdf", "a.pdf", pdfDoc(""))
		rec := doServe(f, c)
		if _, set := rec.Header()["Cache-Control"]; set {
			t.Errorf("level %v: Serve set Cache-Control %q", lvl, rec.Header().Get("Cache-Control"))
		}
		// What the caller set beforehand is left alone.
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/f", nil)
		f2, c2 := serveFixture(lvl, "application/pdf", "a.pdf", pdfDoc(""))
		w := httptest.NewRecorder()
		w.Header().Set("Cache-Control", "no-store")
		files.Serve(w, req, f2, c2)
		if got := w.Header().Values("Cache-Control"); len(got) != 1 || got[0] != "no-store" {
			t.Errorf("level %v: Cache-Control = %v, want the caller's no-store untouched", lvl, got)
		}
	}
}
