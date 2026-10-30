package api

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

const (
	assetCache = "public, max-age=31536000, immutable"
	noCache    = "no-cache"
	indexName  = "index.html"

	// noIndexValue is the X-Robots-Tag of a demo instance (Options.NoIndex).
	noIndexValue = "noindex, nofollow"

	// passwordResetPath is the SPA route whose URL carries a one-time token in the query. Its
	// document response sends no referrer at all (api.md pipeline step 4, F60; D40).
	passwordResetPath = "/accounts/password-reset"
)

// serveUI serves the built web shell (decision 12): a file that exists is sent with a
// cache header by prefix; a path with no file and no extension is the SPA's and gets
// index.html; a path with an extension and no file is 404. Every text/html response
// carries the CSP, and the document for /accounts/password-reset also carries
// Referrer-Policy: no-referrer.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	// Every response of the UI handler carries the policy, whatever it holds: a type the
	// browser sniffs (extensionless) or renders as a document (.svg, .xhtml) must not
	// escape it, and a 404 body is not a place to find out.
	w.Header().Set("Content-Security-Policy", s.csp)
	if s.noIndex {
		w.Header().Set("X-Robots-Tag", noIndexValue)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeNotAllowed(w, "GET, HEAD")
		return
	}
	name, ok := uiName(r.URL.Path)
	if s.ui == nil || !ok {
		s.uiNotFound(w)
		return
	}
	info, err := fs.Stat(s.ui, name)
	switch {
	case err == nil && info.Mode().IsRegular():
	case path.Ext(name) != "":
		s.uiNotFound(w)
		return
	default: // an app path: the SPA's index.html
		name = indexName
		if info, err = fs.Stat(s.ui, name); err != nil || !info.Mode().IsRegular() {
			s.uiNotFound(w)
			return
		}
		if path.Clean(r.URL.Path) == passwordResetPath {
			// Replaces the pipeline's strict-origin-when-cross-origin for this one document,
			// so the token URL is never sent as a referrer, even if a link is added later.
			w.Header().Set("Referrer-Policy", "no-referrer")
		}
	}
	s.sendFile(w, r, name, info)
}

// uiName maps a request path to an fs.FS name. ok is false for a path that cannot name a
// file: not rooted, or holding a NUL.
func uiName(p string) (string, bool) {
	if p == "" || p[0] != '/' || strings.ContainsRune(p, 0) {
		return "", false
	}
	name := strings.TrimPrefix(path.Clean(p), "/")
	if name == "" {
		name = indexName
	}
	return name, fs.ValidPath(name)
}

func (s *Server) uiNotFound(w http.ResponseWriter) {
	if err := writeProblem(w, httpx.Problem{Status: http.StatusNotFound, Code: httpx.CodeNotFound}, nil); err != nil {
		return // the client is gone; the wrapper kept the error
	}
}

func (s *Server) sendFile(w http.ResponseWriter, r *http.Request, name string, info fs.FileInfo) {
	f, err := s.ui.Open(name)
	if err != nil {
		s.uiNotFound(w)
		return
	}
	defer s.closeUI(r.Context(), f)
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		data, rerr := io.ReadAll(f)
		if rerr != nil {
			s.log.WarnContext(r.Context(), "could not read a UI file", slog.String("error.kind", errs.KindOf(rerr).String()))
			s.uiNotFound(w)
			return
		}
		rs = bytes.NewReader(data)
	}
	h := w.Header()
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype != "" {
		h.Set("Content-Type", ctype)
	}
	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", assetCache)
	} else {
		h.Set("Cache-Control", noCache)
	}
	http.ServeContent(w, r, name, info.ModTime(), rs)
}

func (s *Server) closeUI(ctx context.Context, f fs.File) {
	if err := f.Close(); err != nil {
		s.log.WarnContext(ctx, "could not close a UI file", slog.String("error.kind", errs.KindOf(err).String()))
	}
}
