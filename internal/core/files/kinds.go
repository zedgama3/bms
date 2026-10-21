package files

import (
	"bytes"
	"strconv"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Kind is one accepted file type (the catalogue in files.md, "Types"). The zero Kind is
// invalid.
type Kind uint8

// The kinds accepted in M4: images and PDF. No SVG (Q-FILES3) and no office formats.
const (
	PNG Kind = iota + 1
	JPEG
	GIF
	WebP
	PDF
)

type kindInfo struct {
	token       string
	contentType string
	extension   string
}

var kindTable = [...]kindInfo{
	PNG:  {"png", "image/png", ".png"},
	JPEG: {"jpeg", "image/jpeg", ".jpg"},
	GIF:  {"gif", "image/gif", ".gif"},
	WebP: {"webp", "image/webp", ".webp"},
	PDF:  {"pdf", "application/pdf", ".pdf"},
}

func (k Kind) valid() bool { return k >= PNG && k <= PDF }

// String is the token: "png", "jpeg", "gif", "webp", "pdf", or "Kind(<n>)". Cannot fail.
func (k Kind) String() string {
	if k.valid() {
		return kindTable[k].token
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// ContentType is the served Content-Type; "" for an invalid Kind.
func (k Kind) ContentType() string {
	if k.valid() {
		return kindTable[k].contentType
	}
	return ""
}

// Extension is the forced file extension with its dot: ".png", ".jpg", ".gif", ".webp",
// ".pdf"; "" for an invalid Kind.
func (k Kind) Extension() string {
	if k.valid() {
		return kindTable[k].extension
	}
	return ""
}

// Inline reports whether a browser may show the file in place (Content-Disposition inline):
// true for every M4 kind (decision 12).
func (k Kind) Inline() bool { return k.valid() }

// Validate fails errs.Invalid (field "kind", code "one_of") for the zero or an unknown Kind.
func (k Kind) Validate() error {
	if k.valid() {
		return nil
	}
	return fieldErr("files.Kind.Validate", errs.Invalid, "kind", validate.CodeOneOf, nil, "must be one of: png, jpeg, gif, webp, pdf")
}

// kindOf returns the Kind served as contentType.
func kindOf(contentType string) (Kind, bool) {
	for k := PNG; k <= PDF; k++ {
		if kindTable[k].contentType == contentType {
			return k, true
		}
	}
	return 0, false
}

// Accept is the set of kinds a caller takes. An empty Accept is Invalid.
type Accept []Kind

// The common sets.
var (
	Images    = Accept{PNG, JPEG, GIF, WebP}
	Documents = Accept{PDF}
	Brand     = Accept{PNG, JPEG, WebP} // the logo; SVG waits on Q-FILES3
)

// Has reports whether k is in the set.
func (a Accept) Has(k Kind) bool {
	for _, x := range a {
		if x == k {
			return true
		}
	}
	return false
}

// Validate fails errs.Invalid (field "accept", code "required" for an empty set, "one_of"
// for an unknown kind).
func (a Accept) Validate() error {
	const op = "files.Accept.Validate"
	if len(a) == 0 {
		return fieldErr(op, errs.Invalid, "accept", validate.CodeRequired, nil, "must name at least one kind")
	}
	for _, k := range a {
		if !k.valid() {
			return fieldErr(op, errs.Invalid, "accept", validate.CodeOneOf, nil, "must name known kinds")
		}
	}
	return nil
}

// Detect recognises a kind from the first SniffBytes of a file. It cannot fail: an
// unrecognised head answers (0, false). HTML, SVG, XML, text, zip-based formats, HEIC and a
// PDF with anything before its "%PDF-" are all unrecognised.
func Detect(head []byte) (Kind, bool) {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return PNG, true
	case bytes.HasPrefix(head, []byte("\xff\xd8\xff")):
		return JPEG, true
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return GIF, true
	case len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return WebP, true
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return PDF, true
	}
	return 0, false
}
