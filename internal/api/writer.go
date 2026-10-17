package api

import (
	"errors"
	"net"
	"net/http"
	"os"
)

// devCookieName is the one cookie that may omit Secure (decision 24).
const devCookieName = "bms_session_dev"

// respWriter wraps the connection's ResponseWriter for one request. It remembers the
// status, whether the header went out, the bytes written and the first write error, and it
// runs the cookie guard (decision 7) at the moment the header is about to go out. It
// implements Unwrap so http.ResponseController reaches the connection.
type respWriter struct {
	http.ResponseWriter
	s *Server

	scheme      string
	status      int
	wroteHeader bool
	bytes       int
	writeErr    error
	checked     bool              // the cookie guard already ran
	discard     bool              // the guard replaced the response: drop what the handler writes
	failCookie  func(name string) // set by the request: answers 500 on the raw writer
}

func (w *respWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Header returns the header map to fill before WriteHeader.
func (w *respWriter) Header() http.Header { return w.ResponseWriter.Header() }

// WriteHeader runs the cookie guard, then sends the status.
func (w *respWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code) // informational: not the final header
		return
	}
	if w.wroteHeader {
		return
	}
	if !w.guard() {
		return // the guard answered 500 instead
	}
	w.sendHeader(code)
}

// Write sends p, writing the implicit 200 header through the guard first.
func (w *respWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		if !w.guard() {
			return len(p), nil
		}
		w.sendHeader(http.StatusOK)
	}
	if w.discard {
		return len(p), nil
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
	return n, err //nolint:wrapcheck // the caller sees the transport's own error; WriteJSON classifies it
}

// Flush implements http.Flusher; the implicit 200 goes through the guard first.
func (w *respWriter) Flush() {
	if !w.wroteHeader {
		if !w.guard() {
			return
		}
		w.sendHeader(http.StatusOK)
	}
	if err := http.NewResponseController(w.ResponseWriter).Flush(); err != nil {
		w.noteWriteError(err)
	}
}

// markSent records a header the guard's answer wrote straight to the connection.
func (w *respWriter) markSent(code int) {
	w.wroteHeader = true
	w.status = code
}

func (w *respWriter) sendHeader(code int) {
	if w.s.draining.Load() {
		w.Header().Set("Connection", "close")
	}
	w.wroteHeader = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// guard runs the cookie guard once. It reports true when the response may go on.
// On a violation it removes the cookies, writes 500 internal through failCookie and marks
// the rest of the handler's output to be dropped.
func (w *respWriter) guard() bool {
	if w.checked {
		return !w.discard
	}
	w.checked = true
	name, bad := cookieViolation(w.Header(), w.s.opts.AllowPlaintext, w.scheme)
	if !bad {
		return true
	}
	w.Header().Del("Set-Cookie")
	if w.failCookie != nil {
		w.failCookie(name)
	}
	w.discard = true // what the handler writes from here on is dropped
	return false
}

// finish runs the guard for a handler that returned having written nothing: net/http
// would then send 200 with the headers as they stand.
func (w *respWriter) finish() {
	if w.wroteHeader || w.checked {
		return
	}
	w.guard()
}

func (w *respWriter) noteWriteError(err error) {
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
}

// cookieViolation reports the first Set-Cookie in h that breaks the rules of decision 7
// (Secure, HttpOnly, SameSite=Lax, Path=/, no Domain) and decision 24 (the development
// cookie is refused unless AllowPlaintext, and may omit Secure only then and on scheme http). name is the cookie's
// name, never its value; "(unparseable)" when it did not parse.
func cookieViolation(h http.Header, allowPlaintext bool, scheme string) (name string, bad bool) {
	for _, line := range h.Values("Set-Cookie") {
		c, err := http.ParseSetCookie(line)
		if err != nil {
			return "(unparseable)", true
		}
		if c.Name == devCookieName && !allowPlaintext {
			return c.Name, true // production uses only the __Host- cookie
		}
		secureOK := c.Secure || (c.Name == devCookieName && allowPlaintext && scheme == "http")
		if !secureOK || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" {
			return c.Name, true
		}
	}
	return "", false
}

// isTimeout reports whether err is a deadline: a net.Error timeout or os.ErrDeadlineExceeded.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}
