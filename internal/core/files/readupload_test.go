package files_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

// part is one multipart part: a text field when file is false.
type part struct {
	name, filename, ctype string
	body                  []byte
	file                  bool
}

func text(name, value string) part { return part{name: name, body: []byte(value)} }

func filePart(name, filename, ctype string, body []byte) part {
	return part{name: name, filename: filename, ctype: ctype, body: body, file: true}
}

func multipartRequest(t testing.TB, parts ...part) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		if p.file {
			h.Set("Content-Disposition", `form-data; name="`+p.name+`"; filename="`+p.filename+`"`)
			if p.ctype != "" {
				h.Set("Content-Type", p.ctype)
			}
		} else {
			h.Set("Content-Disposition", `form-data; name="`+p.name+`"`)
		}
		pw, err := w.CreatePart(h)
		if err != nil {
			t.Fatalf("CreatePart: %v", err)
		}
		if _, err := pw.Write(p.body); err != nil {
			t.Fatalf("part write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestReadUpload_FieldsThenFile_ReturnsBoth(t *testing.T) {
	body := plainPNG(t)
	req := multipartRequest(t,
		text("kind", "first-aid"),
		text("note", "ünïcode ok"),
		filePart("file", "photo.png", "image/png", body))
	mp, err := files.ReadUpload(req, "file")
	wantNoErr(t, err)
	if mp.Fields["kind"] != "first-aid" || mp.Fields["note"] != "ünïcode ok" || len(mp.Fields) != 2 {
		t.Errorf("Fields = %v", mp.Fields)
	}
	if mp.Name != "photo.png" {
		t.Errorf("Name = %q", mp.Name)
	}
	if mp.DeclaredType != "image/png" {
		t.Errorf("DeclaredType = %q", mp.DeclaredType)
	}
	got, err := io.ReadAll(mp.Body)
	wantNoErr(t, err)
	if !bytes.Equal(got, body) {
		t.Errorf("file stream differs (%d vs %d bytes)", len(got), len(body))
	}
}

func TestReadUpload_FileFirst_ReturnsEmptyFields(t *testing.T) {
	req := multipartRequest(t, filePart("file", "a.pdf", "application/pdf", pdfDoc("")))
	mp, err := files.ReadUpload(req, "file")
	wantNoErr(t, err)
	if len(mp.Fields) != 0 {
		t.Errorf("Fields = %v, want none", mp.Fields)
	}
}

// The declared type is a hint only: Stage decides from the bytes.
func TestReadUpload_DeclaredTypeIsNotTrusted(t *testing.T) {
	r := newRig(t)
	req := multipartRequest(t, filePart("file", "innocent.png", "image/png", refusedBodies["html named png"]))
	mp, err := files.ReadUpload(req, "file")
	wantNoErr(t, err)
	if mp.DeclaredType != "image/png" {
		t.Fatalf("DeclaredType = %q", mp.DeclaredType)
	}
	_, err = r.vol.Stage(ctxT(t), files.Upload{Name: mp.Name, Accept: files.Images, Body: mp.Body})
	wantField(t, err, errs.Invalid, "file", "type")
}

// F38
func TestReadUpload_NotMultipart_ReturnsUnsupportedMediaType(t *testing.T) {
	tests := map[string]*http.Request{
		"json":         httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/u", strings.NewReader(`{}`)),
		"no body type": httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/u", strings.NewReader("x")),
		"form urlenc":  httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/u", strings.NewReader("a=b")),
		"no boundary":  httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/u", strings.NewReader("x")),
	}
	tests["json"].Header.Set("Content-Type", "application/json")
	tests["form urlenc"].Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tests["no boundary"].Header.Set("Content-Type", "multipart/form-data")
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := files.ReadUpload(req, "file")
			wantKind(t, err, errs.Invalid)
			if !errors.Is(err, httpx.ErrUnsupportedMediaType) {
				t.Errorf("err %v does not wrap httpx.ErrUnsupportedMediaType", err)
			}
		})
	}
}

// F39
func TestReadUpload_NoFilePart_ReturnsInvalidRequired(t *testing.T) {
	tests := map[string]*http.Request{
		"only fields":          multipartRequest(t, text("a", "b")),
		"empty form":           multipartRequest(t),
		"wrong part name":      multipartRequest(t, filePart("attachment", "a.png", "image/png", plainPNG(t))),
		"text part named file": multipartRequest(t, text("file", "not a file")),
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := files.ReadUpload(req, "file")
			wantField(t, err, errs.Invalid, "file", "required")
		})
	}
}

// F40
func TestReadUpload_FieldLimits_ReturnsInvalid(t *testing.T) {
	t.Run("MaxFields fields are fine", func(t *testing.T) {
		var parts []part
		for i := range files.MaxFields {
			parts = append(parts, text("f"+itoa(int64(i)), "v"))
		}
		parts = append(parts, filePart("file", "a.pdf", "", pdfDoc("")))
		mp, err := files.ReadUpload(multipartRequest(t, parts...), "file")
		wantNoErr(t, err)
		if len(mp.Fields) != files.MaxFields {
			t.Errorf("Fields = %d", len(mp.Fields))
		}
	})
	t.Run("too many fields", func(t *testing.T) {
		var parts []part
		for i := 0; i <= files.MaxFields; i++ {
			parts = append(parts, text("f"+itoa(int64(i)), "v"))
		}
		parts = append(parts, filePart("file", "a.pdf", "", pdfDoc("")))
		_, err := files.ReadUpload(multipartRequest(t, parts...), "file")
		fe := fieldErr(t, err, errs.Invalid, "fields", "too_many")
		if fe.Params["max"] != itoa(files.MaxFields) {
			t.Errorf("max param = %q, want %d", fe.Params["max"], files.MaxFields)
		}
	})
	t.Run("field of MaxFieldBytes is fine", func(t *testing.T) {
		_, err := files.ReadUpload(multipartRequest(t,
			text("note", strings.Repeat("n", files.MaxFieldBytes)),
			filePart("file", "a.pdf", "", pdfDoc(""))), "file")
		wantNoErr(t, err)
	})
	t.Run("field too long", func(t *testing.T) {
		_, err := files.ReadUpload(multipartRequest(t,
			text("note", strings.Repeat("n", files.MaxFieldBytes+1)),
			filePart("file", "a.pdf", "", pdfDoc(""))), "file")
		fe := fieldErr(t, err, errs.Invalid, "note", "too_long")
		if fe.Params["max"] != itoa(files.MaxFieldBytes) {
			t.Errorf("max param = %q, want %d", fe.Params["max"], files.MaxFieldBytes)
		}
	})
}

// F41: the shell's MaxBytesReader cap surfaces unchanged.
func TestReadUpload_OverRouteCap_Returns413(t *testing.T) {
	const limit = 4 << 10
	big := pdfOfSize(t, 64<<10)
	req := multipartRequest(t,
		text("kind", "x"),
		filePart("file", "big.pdf", "application/pdf", big))
	rec := httptest.NewRecorder()
	req.Body = http.MaxBytesReader(rec, req.Body, limit)

	mp, err := files.ReadUpload(req, "file")
	if err == nil {
		// The limit may bite while the file streams instead of while the headers are parsed.
		_, err = io.ReadAll(mp.Body)
	}
	var mbe *http.MaxBytesError
	if !errors.As(err, &mbe) {
		t.Fatalf("err = %v, want *http.MaxBytesError (the api answers 413)", err)
	}
	if mbe.Limit != limit {
		t.Errorf("Limit = %d, want %d", mbe.Limit, limit)
	}
}

func TestReadUpload_CanceledRequest_ReturnsCanceled(t *testing.T) {
	req := multipartRequest(t, filePart("file", "a.pdf", "", pdfDoc("")))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req = req.WithContext(ctx)
	mp, err := files.ReadUpload(req, "file")
	if err == nil {
		_, err = io.ReadAll(mp.Body)
	}
	// A body already buffered may still be read in full; what must never happen is a success
	// that hides the cancellation from a following Stage (covered in stage_test.go).
	if err != nil && !errs.Is(err, errs.Canceled) && !errs.Is(err, errs.Invalid) {
		t.Errorf("err kind = %v", errs.KindOf(err))
	}
}
