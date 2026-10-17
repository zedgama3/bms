package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

const bodyField = "body"

// ReadJSON decodes r's body into dst. The body is already capped by the shell
// (Options.MaxBodyBytes); a handler that wants a tighter cap wraps r.Body with
// http.MaxBytesReader first. Content-Type must be application/json (parameters allowed);
// otherwise it fails with errs.Invalid wrapping ErrUnsupportedMediaType. Malformed JSON, a
// wrong type, or trailing data fails with errs.Invalid wrapping a validate.FieldError whose
// Field is "body" or the JSON path the decoder reports, cut at the first segment that is
// not a struct field of dst's type (a map key or a slice index), so a client-chosen key
// never reaches a span or a log; its Code is "malformed" (no Params) and the Reason never
// echoes the body. An empty body is Invalid with field "body", code "required". Unknown
// fields are ignored. A *http.MaxBytesError from the capped body is returned unchanged so
// the mapper answers 413. A read that fails because the connection's read deadline passed
// (a net.Error with Timeout() true, or os.ErrDeadlineExceeded) is errs.Timeout; a read that
// fails because r.Context() is done takes the ctx's kind; any other read error (the client
// went away mid-body) is errs.Canceled. dst must be a non-nil pointer, else errs.Internal.
func ReadJSON(r *http.Request, dst any) error {
	const op = "httpx.ReadJSON"
	if rv := reflect.ValueOf(dst); dst == nil || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errs.E(op, errs.Internal, errors.New("destination must be a non-nil pointer"))
	}
	if !isJSON(r.Header.Get("Content-Type")) {
		return errs.E(op, errs.Invalid, ErrUnsupportedMediaType)
	}
	if err := r.Context().Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	var raw []byte
	if r.Body != nil {
		var err error
		raw, err = io.ReadAll(r.Body)
		if err != nil {
			return readError(r.Context(), op, err)
		}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return invalidBody(op, bodyField, validate.CodeRequired, "a JSON body is required")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return decodeError(op, dst, raw, err)
	}
	return nil
}

func isJSON(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "application/json"
}

func readError(ctx context.Context, op string, err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return err //nolint:wrapcheck // returned unchanged on purpose: the mapper answers 413 from its type
	}
	var ne net.Error
	if errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return errs.E(op, errs.Timeout, err)
	}
	if cerr := ctx.Err(); cerr != nil {
		return errs.E(op, errs.KindOf(cerr), err)
	}
	return errs.E(op, errs.Canceled, err)
}

func invalidBody(op, field, code, reason string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Reason: reason})
}

// decodeError turns a decoder error into an Invalid with a FieldError that names a path
// and a rule but never the input.
func decodeError(op string, dst any, raw []byte, err error) error {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	var invalidDst *json.InvalidUnmarshalError
	switch {
	case errors.As(err, &typeErr):
		field := bodyField
		if p := structPath(reflect.TypeOf(dst), typeErr.Field); p != "" {
			field += "." + p
		}
		return invalidBody(op, field, validate.CodeMalformed, "must be "+jsonWord(typeErr.Type))
	case errors.As(err, &syntaxErr):
		return invalidBody(op, bodyField, validate.CodeMalformed, "is not valid JSON")
	case errors.As(err, &invalidDst):
		return errs.E(op, errs.Internal, errors.New("destination cannot be decoded into"))
	default:
		// An UnmarshalJSON or UnmarshalText of a field refused its value. Its error can
		// quote the input, so it is dropped; the field is found by decoding each one alone.
		field := bodyField
		if segs := locateFailure(reflect.TypeOf(dst), raw); len(segs) > 0 {
			field += "." + strings.Join(segs, ".")
		}
		return invalidBody(op, field, validate.CodeMalformed, "has an invalid value")
	}
}

// locateFailure returns the struct-field path (JSON names) of the first field of t whose
// value in raw does not decode, descending through struct fields only. A slice, array or
// map field ends the path at that field, so a client-chosen key or index never appears.
func locateFailure(t reflect.Type, raw []byte) []string {
	t = derefPointers(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	for _, f := range jsonFields(t) {
		val, ok := lookupKey(obj, f.name)
		if !ok {
			continue
		}
		if json.Unmarshal(val, reflect.New(f.typ).Interface()) != nil {
			return append([]string{f.name}, locateFailure(f.typ, val)...)
		}
	}
	return nil
}

type jsonField struct {
	name string
	typ  reflect.Type
}

// jsonFields lists t's decodable fields by JSON name, with promoted fields of embedded
// structs.
func jsonFields(t reflect.Type) []jsonField {
	var out []jsonField
	for i := range t.NumField() {
		f := t.Field(i)
		name, embedded := jsonFieldName(f)
		if embedded {
			if ft := derefPointers(f.Type); ft != nil && ft.Kind() == reflect.Struct {
				out = append(out, jsonFields(ft)...)
			}
			continue
		}
		if name != "" {
			out = append(out, jsonField{name: name, typ: f.Type})
		}
	}
	return out
}

// lookupKey finds name in obj the way encoding/json does: exactly, else ignoring case.
func lookupKey(obj map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	if v, ok := obj[name]; ok {
		return v, true
	}
	for k, v := range obj {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return nil, false
}

func derefPointers(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// jsonWord names the JSON type a Go type decodes from, for the reason text.
func jsonWord(t reflect.Type) string {
	if t == nil {
		return "of another type"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() { //nolint:exhaustive // the rest are not JSON-decodable targets
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "a number of the expected kind and range"
	case reflect.Struct, reflect.Map:
		return "an object"
	case reflect.Slice, reflect.Array:
		return "an array"
	default:
		return "of another type"
	}
}

// structPath keeps the leading segments of the decoder's dotted path that are struct
// fields of t (matched by JSON name), and drops the first segment that is not, with
// everything after it. A map key or a slice index is client-chosen and must not leave
// this package.
func structPath(t reflect.Type, path string) string {
	if path == "" {
		return ""
	}
	var kept []string
	for seg := range strings.SplitSeq(path, ".") {
		t = derefType(t)
		if t == nil || t.Kind() != reflect.Struct {
			break
		}
		ft, ok := fieldByJSONName(t, seg)
		if !ok {
			break
		}
		kept = append(kept, seg)
		t = ft
	}
	return strings.Join(kept, ".")
}

// derefType strips pointers, and steps into a slice or array's element so that "list.n"
// keeps "list" and then "n" (the index itself never appears in the decoder's path).
func derefType(t reflect.Type) reflect.Type {
	for t != nil && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		t = t.Elem()
	}
	return t
}

func fieldByJSONName(t reflect.Type, name string) (reflect.Type, bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		jsonName, embedded := jsonFieldName(f)
		if embedded {
			if ft := derefType(f.Type); ft != nil && ft.Kind() == reflect.Struct {
				if got, ok := fieldByJSONName(ft, name); ok {
					return got, true
				}
			}
			continue
		}
		if jsonName != "" && jsonName == name {
			return f.Type, true
		}
	}
	return nil, false
}

// jsonFieldName returns the field's JSON key, or "" for a field json ignores. embedded is
// true for an anonymous struct field without a name tag, whose fields are promoted.
func jsonFieldName(f reflect.StructField) (name string, embedded bool) {
	tag, hasTag := f.Tag.Lookup("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ = strings.Cut(tag, ",")
	if f.Anonymous && name == "" {
		return "", true
	}
	if !f.IsExported() {
		return "", false
	}
	if !hasTag || name == "" {
		return f.Name, false
	}
	return name, false
}

// WriteJSON encodes v into a buffer, then writes status, Content-Type application/json
// and the bytes in one Write; the shell has already set Cache-Control: no-store on API
// responses. It fails with errs.Internal if v cannot be encoded (nothing is written then)
// and with errs.Canceled wrapping the write error when the Write fails for any reason (the
// client is gone or the connection's write deadline passed). The shell's response-writer
// wrapper remembers that first write error, so the span is marked and the caller logs
// nothing. Status 204 and 304 write no body whatever v is.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	const op = "httpx.WriteJSON"
	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return nil
	}
	body, err := json.Marshal(v)
	if err != nil {
		return errs.E(op, errs.Internal, fmt.Errorf("encode response: %w", err))
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		return errs.E(op, errs.Canceled, fmt.Errorf("write response: %w", err))
	}
	return nil
}
