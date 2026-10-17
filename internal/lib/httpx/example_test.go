package httpx_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
)

type greetingRequest struct {
	Name string `json:"name"`
}

// A handler decodes the body, calls its service and answers; on any error it hands the
// error to Fail and returns.
func Example() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req greetingRequest
		if err := httpx.ReadJSON(r, &req); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := validate.NonEmpty("name", req.Name); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if err := httpx.WriteJSON(w, http.StatusOK, map[string]string{"greeting": "hello " + req.Name}); err != nil {
			return // the client is gone; the shell already recorded it
		}
	})

	for _, body := range []string{`{"name":"Ada"}`, `{"name":""}`} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/greet", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		fmt.Println(rec.Code, rec.Header().Get("Content-Type"), strings.TrimSpace(rec.Body.String()))
	}
	// Output:
	// 200 application/json {"greeting":"hello Ada"}
	// 400 application/problem+json {"status":400,"code":"invalid","fields":[{"field":"name","code":"required","reason":"must not be empty"}]}
}

// StatusOf is the one table from an errs kind to an HTTP status.
func ExampleStatusOf() {
	fmt.Println(httpx.StatusOf(errs.NotFound), httpx.StatusOf(errs.Unavailable), httpx.StatusOf(errs.Internal))
	// Output: 404 503 500
}
