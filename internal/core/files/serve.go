package files

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Serve writes f's bytes with safe headers, honouring Range, If-Range, If-None-Match and
// If-Modified-Since through http.ServeContent. It never fails: a bad range is a 416 and a
// client that is gone ends the write.
//
// Headers: Content-Type is the sniffed type of the file (the api shell sends
// X-Content-Type-Options: nosniff); Content-Disposition is inline with the
// file name for a kind a browser may show in place and attachment otherwise;
// Content-Security-Policy is default-src 'none'; ETag is the SHA-256 in quotes and
// Last-Modified the upload time. Serve sets no Cache-Control: the api shell's no-store stays
// on every /api/ response, so a personal file is never cached whatever its level. The write
// deadline is extended to ServeTimeout so a slow link outlives the shell's request deadline.
// Serve does not close c.
func Serve(w http.ResponseWriter, r *http.Request, f File, c Content) {
	h := w.Header()
	ct := f.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct)
	h.Set("Content-Security-Policy", "default-src 'none'")
	disposition := "attachment"
	if k, ok := kindOf(ct); ok && k.Inline() {
		disposition = "inline"
	}
	h.Set("Content-Disposition", contentDisposition(disposition, f.Name))
	if f.SHA256 != ([32]byte{}) {
		h.Set("ETag", `"`+hexOf(f.SHA256)+`"`)
	}
	// A connection that cannot take a deadline (a test recorder, some HTTP/2 paths) keeps the
	// shell's own; one that is already gone cannot be written to, so there is nothing to send.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(ServeTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return
	}
	http.ServeContent(w, r, "", f.CreatedAt, c)
}

// contentDisposition renders "<disposition>; filename=<ascii>; filename*=UTF-8”<escaped>".
func contentDisposition(disposition, name string) string {
	v := mime.FormatMediaType(disposition, map[string]string{"filename": asciiName(name)})
	if v == "" { // cannot happen for an ASCII token, but never send an empty header
		v = disposition
	}
	return v + "; filename*=UTF-8''" + rfc5987(name)
}

// asciiName is the fallback name: anything outside printable ASCII, quotes and backslashes
// become an underscore.
func asciiName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == '/':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "file"
	}
	return b.String()
}

// rfc5987 percent-encodes name as an RFC 5987 ext-value: attr-chars stay, every other byte
// of the UTF-8 encoding becomes %XX.
func rfc5987(name string) string {
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "_")
	}
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
