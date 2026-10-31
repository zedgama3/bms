package audit

import (
	"context"
	_ "embed" // the OpenAPI fragment
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Content types and the download name of the views export.
const (
	contentTypeNDJSON = "application/x-ndjson"
	viewsFilePrefix   = "views-"
	viewsFileSuffix   = ".jsonl"
)

// reader is the part of *Log the handlers call, so they are tested over a fake.
type reader interface {
	Search(ctx context.Context, q Query) (Page, error)
	Export(ctx context.Context, person model.PersonID, w io.Writer) (int, error)
}

// Routes lists GET /entries and GET /people/{person}/views. Handlers are thin: parse the
// query, call Search or Export, httpx.Fail on error. Cannot fail.
func (l *Log) Routes() []httpx.Route { return routesOver(l, l.logger) }

// OpenAPI returns the fragment for those routes. Cannot fail (embedded; a test proves it
// parses and names exactly the routes).
func (l *Log) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

func routesOver(r reader, logger *slog.Logger) []httpx.Route {
	return []httpx.Route{
		{Method: http.MethodGet, Pattern: "/entries", Handler: entriesHandler(r)},
		{Method: http.MethodGet, Pattern: "/people/{person}/views", Handler: viewsHandler(r, logger)},
	}
}

// pageBody is the JSON body of GET /entries.
type pageBody struct {
	Records []wireRecord `json:"records"`
	Next    string       `json:"next,omitempty"`
}

func entriesHandler(svc reader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, err := parseQuery(r.URL.Query())
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		page, err := svc.Search(r.Context(), q)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		body := pageBody{Records: make([]wireRecord, len(page.Records)), Next: string(page.Next)}
		for i, rec := range page.Records {
			body.Records[i] = toWire(rec)
		}
		if err := httpx.WriteJSON(w, http.StatusOK, body); err != nil && !errs.Is(err, errs.Canceled) {
			httpx.Fail(w, r, err)
		}
	})
}

// parseQuery reads the query string, reporting every bad parameter under "query.<name>".
func parseQuery(vals url.Values) (Query, error) {
	const op = "audit.parseQuery"
	var (
		q Query
		v = validate.Errors{Op: op}
	)
	id := func(name string) model.PersonID {
		s := vals.Get(name)
		if s == "" {
			return model.PersonID{}
		}
		p, err := model.ParseID[model.PersonID](s)
		if err != nil {
			v.Addf("query."+name, codeMalformed, "must be a UUID")
		}
		return p
	}
	for name, v2 := range vals {
		if len(v2) > 1 {
			v.Addf("query."+name, codeMalformed, "may be given once")
		}
	}
	q.Actor = id("actor")
	q.Subject = id("subject")
	q.Record.Kind = vals.Get("record_kind")
	if s := vals.Get("record_id"); s != "" {
		rid, err := model.ParseID[RecordID](s)
		if err != nil {
			v.Addf("query.record_id", codeMalformed, "must be a UUID")
		}
		q.Record.ID = rid
	}
	q.Area = vals.Get("area")
	q.Action = Action(vals.Get("action"))
	q.After = Cursor(vals.Get("after"))
	if s := vals.Get("superuser"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			v.Addf("query.superuser", codeMalformed, "must be true or false")
		}
		q.SuperuserOnly = b
	}
	if s := vals.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			v.Addf("query.limit", codeMalformed, "must be a whole number")
		}
		q.Limit = n
	}
	from, to := queryTime(&v, vals, "from"), queryTime(&v, vals, "to")
	if (vals.Get("from") != "") != (vals.Get("to") != "") {
		v.Addf("query.from", validate.CodeRequired, "from and to must be given together")
	}
	q.Within = model.TimeRange{Start: from, End: to}
	if err := v.Err(); err != nil {
		return Query{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	// The same rules Search applies, answered here under the parameter names.
	if fields := validate.Fields(q.Validate()); len(fields) > 0 {
		for _, fe := range fields {
			v.Add(errs.E("", errs.Invalid, &validate.FieldError{
				Field: "query." + queryParam(fe.Field), Code: fe.Code, Params: fe.Params, Reason: fe.Reason,
			}))
		}
		return Query{}, v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return q, nil
}

// queryParam names the query-string parameter behind a Query field.
func queryParam(field string) string {
	switch field {
	case "record":
		return "record_kind"
	case "within":
		return "from"
	default:
		return field
	}
}

func queryTime(v *validate.Errors, vals url.Values, name string) time.Time {
	s := vals.Get(name)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		v.Addf("query."+name, codeMalformed, "must be an RFC 3339 time")
	}
	return t
}

// streamWriter sets the download headers on the first write, so an error before any line
// can still be answered with a problem body.
type streamWriter struct {
	w        http.ResponseWriter
	filename string
	started  bool
}

func (s *streamWriter) start() {
	if s.started {
		return
	}
	s.started = true
	h := s.w.Header()
	h.Set("Content-Type", contentTypeNDJSON)
	h.Set("Content-Disposition", `attachment; filename="`+s.filename+`"`)
	s.w.WriteHeader(http.StatusOK)
}

func (s *streamWriter) Write(p []byte) (int, error) {
	s.start()
	n, err := s.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("write response: %w", err)
	}
	return n, nil
}

func viewsHandler(svc reader, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		person, err := model.ParseID[model.PersonID](r.PathValue("person"))
		if err != nil {
			httpx.Fail(w, r, fieldErr("audit.viewsHandler", "person", codeMalformed, "must be a UUID"))
			return
		}
		sw := &streamWriter{w: w, filename: viewsFilePrefix + person.String() + viewsFileSuffix}
		if _, err := svc.Export(r.Context(), person, sw); err != nil {
			switch {
			case !sw.started:
				httpx.Fail(w, r, err)
			case errs.Is(err, errs.Canceled):
				// The client went away; the audit.exported entry stays.
			default:
				// Lines are already on the wire, so no error body can follow. Abort the
				// response so the client sees a broken transfer, not a short file that
				// looks complete.
				logger.ErrorContext(r.Context(), "audit export failed after it began", slog.String("kind", errs.KindOf(err).String()))
				panic(http.ErrAbortHandler)
			}
			return
		}
		sw.start() // an export of nothing is still a file
	})
}
