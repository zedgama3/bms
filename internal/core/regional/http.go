package regional

import (
	_ "embed" // the OpenAPI fragment
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

//go:embed openapi.json
var openAPIFragment []byte

// setCaching is the one place the caching of GET /public and GET /theme.css is decided: a
// strong ETag derived from settings' revision alone (a logo change bumps it too), so a write
// moves it and a changed document never matches. No handler sets Cache-Control: the
// /api/ no-store rule stays on both routes, and a 304 to a matching If-None-Match saves the
// body.
func setCaching(h http.Header, etag string) { h.Set("ETag", etag) }

// notModified reports whether the request's If-None-Match names etag (a list of tags, weak
// ones, or "*").
func notModified(r *http.Request, etag string) bool {
	for _, field := range r.Header.Values("If-None-Match") {
		for _, tag := range strings.Split(field, ",") {
			tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
			if tag == "*" || tag == etag {
				return true
			}
		}
	}
	return false
}

// Routes returns the routes of the regional set, relative to /api/regional. GET /public,
// GET /theme.css and GET /logo are public (no session); PUT /logo and DELETE /logo need a
// signed-in person, and the permission (settings.manage) is checked inside the use case.
// PUT /logo takes at most 2*MaxLogoBytes of request body (the field error of a file over
// MaxLogoBytes wins for anything near the cap) and files.UploadTimeout; GET /logo gets
// files.ServeTimeout. Handlers are thin: parse, call the service, httpx.Fail on error.
// Cannot fail.
func (s *Service) Routes() []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Pattern: "/public", Public: true, Handler: s.publicHandler()},
		{Method: http.MethodGet, Pattern: "/theme.css", Public: true, Handler: s.themeHandler()},
		{Method: http.MethodGet, Pattern: "/logo", Public: true, Handler: s.getLogoHandler(), Timeout: files.ServeTimeout},
		{
			Method: http.MethodPut, Pattern: "/logo", Handler: s.putLogoHandler(),
			MaxBodyBytes: 2 * MaxLogoBytes, Timeout: files.UploadTimeout,
		},
		{Method: http.MethodDelete, Pattern: "/logo", Handler: s.deleteLogoHandler()},
	}
}

// OpenAPI returns the embedded fragment for those routes. Cannot fail (a test proves it
// parses and names exactly the routes).
func (*Service) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

// writeJSON writes v and leaves a transport error to the shell (it is already remembered
// on the span).
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

func (s *Service) publicHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, etag, err := s.publicAt(r.Context())
		if err != nil {
			httpx.Fail(w, r, wrap("regional.publicHandler", err))
			return
		}
		setCaching(w.Header(), etag)
		if notModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, r, http.StatusOK, p)
	})
}

func (s *Service) themeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		css, etag, err := s.themeAt(r.Context())
		if err != nil {
			httpx.Fail(w, r, wrap("regional.themeHandler", err))
			return
		}
		setCaching(w.Header(), etag)
		if notModified(r, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/css; charset=utf-8")
		h.Set("Content-Length", strconv.Itoa(len(css)))
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(css)); err != nil {
			return // the client is gone; the shell remembers the write error
		}
	})
}

// getLogoHandler serves the logo. The ?v= query the shell adds only versions the URL.
func (s *Service) getLogoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, c, err := s.OpenLogo(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		files.Serve(w, r, f, c)
		if err := c.Close(); err != nil {
			s.logger.WarnContext(r.Context(), "the logo could not be closed",
				slog.String("error.kind", errs.KindOf(err).String()))
		}
	})
}

// putLogoHandler reads the multipart upload (the file part is named "file") and replaces
// the logo with it.
func (s *Service) putLogoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Permission first: a person who may not change the logo gets Permission before
		// the request body is parsed or a byte of the file is read.
		if err := s.preflightSetLogo(r.Context(), "regional.putLogo"); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		mp, err := files.ReadUpload(r, fieldFile)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		res, err := s.SetLogo(r.Context(), logoName, mp.Body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, res)
	})
}

func (s *Service) deleteLogoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, err := s.RemoveLogo(r.Context())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusOK, res)
	})
}
