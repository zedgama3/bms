package httpx

import (
	"net/http"
	"time"
)

// Route is a route a core area or module exposes, relative to its set's prefix. Public
// routes need no session; every other route needs one, and checks its permission inside
// the use case. MaxBodyBytes and Timeout raise the shell's defaults (zero keeps them).
//
// Route holds no rules of its own: module.CoreRoutes converts it to a module.Route and
// validates the result, so a core package declares routes without importing core/module.
type Route struct {
	Method       string        // "GET", "POST", "PUT", "PATCH" or "DELETE"
	Pattern      string        // net/http ServeMux path syntax, starting with "/", relative to the set's prefix
	Public       bool          // true: reachable without a session; the use case checks who is calling
	Handler      http.Handler  // must not be nil
	MaxBodyBytes int64         // raises the request body cap of this route; zero keeps the shell's
	Timeout      time.Duration // raises the request deadline of this route; zero keeps the shell's
}
