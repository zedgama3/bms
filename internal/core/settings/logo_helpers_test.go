package settings_test

// Helpers shared by the logo tests (settings.md, amendment 2026-10-21, F37 to F54). The
// images are generated or hand-built from invented bytes; nothing here is a real person's.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/files/filestest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

// ---- images ----------------------------------------------------------------------------

// pngOf is a small valid PNG whose pixels depend on shade, so two logos differ in bytes.
func pngOf(t testing.TB, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, color.RGBA{R: shade, G: uint8(x * 40 % 256), B: uint8(y * 40 % 256), A: 255}) //nolint:gosec // x and y are below 4
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return b.Bytes()
}

// padded returns head followed by zeros up to n bytes in all.
func padded(head []byte, n int) []byte {
	out := make([]byte, n)
	copy(out, head)
	return out
}

// ---- readers ---------------------------------------------------------------------------

// countingBody serves data and counts the bytes and the calls handed out.
type countingBody struct {
	r     io.Reader
	bytes atomic.Int64
	reads atomic.Int64
}

func newCountingBody(data []byte) *countingBody { return &countingBody{r: bytes.NewReader(data)} }

func (c *countingBody) Read(p []byte) (int, error) {
	c.reads.Add(1)
	n, err := c.r.Read(p)
	c.bytes.Add(int64(n))
	return n, err //nolint:wrapcheck // test double
}

// ---- a files.Store that records and can fail on demand ---------------------------------

// spyStore wraps a store and counts the calls the logo use cases make, with hooks to fail
// one of them.
type spyStore struct {
	files.Store

	mu        sync.Mutex
	stages    int
	attaches  int
	discards  int
	opens     int
	attachErr error
	openErr   error
}

func (s *spyStore) Stage(ctx context.Context, up files.Upload) (files.Staged, error) {
	s.mu.Lock()
	s.stages++
	s.mu.Unlock()
	return s.Store.Stage(ctx, up) //nolint:wrapcheck // test double
}

func (s *spyStore) Attach(ctx context.Context, st files.Staged, a files.Attachment) (files.File, error) {
	s.mu.Lock()
	s.attaches++
	err := s.attachErr
	s.mu.Unlock()
	if err != nil {
		return files.File{}, err
	}
	return s.Store.Attach(ctx, st, a) //nolint:wrapcheck // test double
}

func (s *spyStore) Discard(ctx context.Context, st files.Staged) error {
	s.mu.Lock()
	s.discards++
	s.mu.Unlock()
	return s.Store.Discard(ctx, st) //nolint:wrapcheck // test double
}

func (s *spyStore) Open(ctx context.Context, id files.FileID) (files.File, files.Content, error) {
	s.mu.Lock()
	s.opens++
	err := s.openErr
	s.mu.Unlock()
	if err != nil {
		return files.File{}, nil, err
	}
	return s.Store.Open(ctx, id) //nolint:wrapcheck // test double
}

func (s *spyStore) setOpenErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openErr = err
}

func (s *spyStore) counts() (stages, attaches, discards int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stages, s.attaches, s.discards
}

// ---- the unit rig with a files store ----------------------------------------------------

// logoUnit is a unit rig whose Options.Files is a spy over filestest.NewFake("settings").
type logoUnit struct {
	*unit
	fs    *filestest.Fake
	spy   *spyStore
	admin model.PersonID // holds settings.manage
}

func newLogoUnit(t *testing.T) *logoUnit {
	t.Helper()
	fs := filestest.NewFake("settings")
	spy := &spyStore{Store: fs}
	u := newUnit(t, func(o *settings.Options) { o.Files = spy })
	return &logoUnit{unit: u, fs: fs, spy: spy, admin: u.person(settings.PermissionManage)}
}

func (u *logoUnit) actx() context.Context { return asPerson(context.Background(), u.admin) }

// seedLogo puts data on LogoRef directly through the fake store (a logo that already
// exists), inside the fake database's Write transaction.
func (u *logoUnit) seedLogo(t *testing.T, name string, data []byte) files.File {
	t.Helper()
	var f files.File
	ctx := context.Background()
	st, err := u.fs.Stage(ctx, files.Upload{Name: name, Accept: files.Brand, Body: bytes.NewReader(data)})
	wantNoErr(t, err)
	wantNoErr(t, u.fake.Transact(ctx, db.Write, func(ctx context.Context) error {
		var aerr error
		f, aerr = u.fs.Attach(ctx, st, files.Attachment{Record: settings.LogoRef, Level: model.SensitivityPublic})
		return aerr //nolint:wrapcheck // test double
	}))
	return f
}

// ---- HTTP ------------------------------------------------------------------------------

// multipartOf builds a multipart/form-data body: text fields first, then (when file is not
// nil) the file part named "file" with the given part content type ("" = none declared).
func multipartOf(t testing.TB, fields [][2]string, fileName, partType string, file []byte) (body []byte, contentType string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, kv := range fields {
		if err := mw.WriteField(kv[0], kv[1]); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if file != nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="`+fileName+`"`)
		if partType != "" {
			h.Set("Content-Type", partType)
		}
		pw, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("CreatePart: %v", err)
		}
		if _, err := pw.Write(file); err != nil {
			t.Fatalf("part write: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return b.Bytes(), mw.FormDataContentType()
}

// send serves one request through the rig's mux with a failer installed, as the api shell
// does.
func (r *rig) send(ctx context.Context, method, path string, body []byte, contentType string, hdr ...string) (*httptest.ResponseRecorder, *failRec) {
	r.t.Helper()
	f := &failRec{}
	ctx = httpx.WithFailer(ctx, f)
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req := httptest.NewRequestWithContext(ctx, method, "/api/settings"+path, rd)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	r.mux().ServeHTTP(w, req)
	return w, f
}

func logoRoute(t testing.TB, svc *settings.Service, method string) settings.Route {
	t.Helper()
	for _, rt := range svc.Routes() {
		if rt.Method == method && rt.Pattern == "/logo" {
			return rt
		}
	}
	t.Fatalf("no %s /logo route", method)
	return settings.Route{}
}

// closeContent closes c (nil is fine) and fails the test when Close fails.
func closeContent(t testing.TB, c files.Content) {
	t.Helper()
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		t.Errorf("closing the logo content: %v", err)
	}
}
