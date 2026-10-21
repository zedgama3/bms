package files

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Multipart is one parsed upload request: the text fields that came before the file part, and
// the file part as a stream. The file part must be last.
type Multipart struct {
	Fields       map[string]string // at most MaxFields, each at most MaxFieldBytes
	Name         string            // the part's file name as sent
	DeclaredType string            // the part's Content-Type as sent; a hint, never trusted
	Body         io.Reader         // the file part; valid until the request body is closed
}

// maxFieldNameBytes bounds a text field's name.
const maxFieldNameBytes = 64

// ReadUpload parses a multipart/form-data request whose file part is named field. Text
// parts before the file part are collected into Fields; the file part is returned as a
// stream and parsing stops there. A text part after the file is errs.Invalid (field "file",
// code "order"), reported by the stream's last Read. It fails errs.Invalid wrapping
// httpx.ErrUnsupportedMediaType when the request is not multipart/form-data; errs.Invalid
// "file"/"required" with no file part; "fields"/"too_many" past MaxFields text parts;
// "<field>"/"too_long" for a text value over MaxFieldBytes; *http.MaxBytesError (wrapped,
// errors.As finds it) when the body is over the route's cap; errs.Timeout and errs.Canceled
// when the request's context ends.
func ReadUpload(r *http.Request, field string) (*Multipart, error) {
	const op = "files.ReadUpload"
	if field == "" {
		return nil, fieldErr(op, errs.Invalid, "field", codeRequired, nil, "the name of the file part must be set")
	}
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, errs.E(op, errs.Invalid, httpx.ErrUnsupportedMediaType)
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	fields := map[string]string{}
	for {
		part, err := mr.NextPart()
		switch {
		case errors.Is(err, io.EOF):
			return nil, fieldErr(op, errs.Invalid, "file", codeRequired, nil, "choose a file")
		case err != nil:
			return nil, readErr(op, r, err)
		}
		name := part.FormName()
		if name == field && part.FileName() != "" {
			return &Multipart{
				Fields:       fields,
				Name:         part.FileName(),
				DeclaredType: part.Header.Get("Content-Type"),
				Body:         &orderReader{op: op, part: part, mr: mr, r: r},
			}, nil
		}
		if part.FileName() != "" {
			continue // another file part: not ours, and NextPart drains it
		}
		if len(fields) >= MaxFields {
			return nil, fieldErr(op, errs.Invalid, "fields", codeTooMany, maxParam(MaxFields), "too many fields")
		}
		if len(name) == 0 || len(name) > maxFieldNameBytes || !utf8.ValidString(name) {
			return nil, fieldErr(op, errs.Invalid, "fields", codeMalformed, nil, "a field has no usable name")
		}
		val, err := io.ReadAll(io.LimitReader(part, MaxFieldBytes+1))
		if err != nil {
			return nil, readErr(op, r, err)
		}
		if len(val) > MaxFieldBytes {
			return nil, fieldErr(op, errs.Invalid, name, codeTooLong, maxParam(MaxFieldBytes), "is too long")
		}
		fields[name] = string(val)
	}
}

// readErr classifies a failure to read the request body: the route's cap (unchanged for
// errors.As), the request's context, and otherwise a body that is not a well-formed
// multipart document.
func readErr(op string, r *http.Request, err error) error {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return errs.E(op, errs.Invalid, fmt.Errorf("the request body is too large: %w", err))
	case r.Context().Err() != nil:
		return errs.E(op, errs.KindOf(r.Context().Err()), r.Context().Err())
	}
	if k, ok := ctxKind(err); ok {
		return errs.E(op, k, err)
	}
	v := validate.Errors{Op: op}
	v.Addf("file", codeMalformed, "the upload is not a well-formed multipart document")
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// orderReader is the file part. When the part ends it looks for another: a part after the
// file means the client put a field after it, which cannot be honoured.
type orderReader struct {
	op   string
	part *multipart.Part
	mr   *multipart.Reader
	r    *http.Request
	done bool
}

func (o *orderReader) Read(p []byte) (int, error) {
	n, err := o.part.Read(p)
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return n, readErr(o.op, o.r, err)
		}
		return n, nil
	}
	if o.done {
		return n, io.EOF
	}
	o.done = true
	next, nerr := o.mr.NextPart()
	switch {
	case errors.Is(nerr, io.EOF):
		return n, io.EOF
	case nerr != nil:
		return n, readErr(o.op, o.r, nerr)
	}
	if cerr := next.Close(); cerr != nil {
		return n, readErr(o.op, o.r, cerr)
	}
	return n, fieldErr(o.op, errs.Invalid, "file", codeOrder, nil, "the file must be the last part of the upload")
}
