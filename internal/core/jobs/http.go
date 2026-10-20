package jobs

import (
	_ "embed" // the OpenAPI fragment
	"net/http"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed openapi.json
var openAPIFragment []byte

// Routes returns the routes of the jobs set, relative to /api/jobs. Every route needs a
// signed-in person; the permission is checked inside the use case. Handlers are thin: parse,
// call the runner, httpx.Fail on error. Cannot fail.
func (r *Runner) Routes() []Route {
	return []Route{
		{Method: http.MethodGet, Pattern: "/", Handler: r.statusHandler()},
		{Method: http.MethodPost, Pattern: "/registered/{name}/run", Handler: r.runNowHandler()},
		{Method: http.MethodPost, Pattern: "/queue/{job}/retry", Handler: r.retryHandler()},
		{Method: http.MethodPost, Pattern: "/queue/{job}/cancel", Handler: r.cancelHandler()},
	}
}

// OpenAPI returns the embedded fragment for those routes. Cannot fail.
func (*Runner) OpenAPI() []byte { return append([]byte(nil), openAPIFragment...) }

// ---- wire forms --------------------------------------------------------------------------

type wireRecord struct {
	ID         JobID     `json:"id"`
	Name       Name      `json:"name"`
	Key        string    `json:"key,omitempty"`
	Status     Status    `json:"status"`
	Attempts   int       `json:"attempts"`
	RunAt      time.Time `json:"run_at"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	LastError  string    `json:"last_error,omitempty"`
	Instance   string    `json:"instance,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
}

func wireOf(r Record) wireRecord {
	return wireRecord(r)
}

type wireRegistered struct {
	Name           Name        `json:"name"`
	Owner          string      `json:"owner"`
	EverySeconds   int64       `json:"every_seconds"`
	TimeoutSeconds int64       `json:"timeout_seconds"`
	Last           *wireRecord `json:"last,omitempty"`
	NextDue        time.Time   `json:"next_due,omitzero"`
}

type wireStatus struct {
	ReadOnly   bool             `json:"read_only"`
	Registered []wireRegistered `json:"registered"`
	Counts     map[Status]int   `json:"counts"`
	Recent     []wireRecord     `json:"recent"`
}

func wireStatusOf(v StatusView) wireStatus {
	out := wireStatus{
		ReadOnly: v.ReadOnly, Counts: v.Counts,
		Registered: make([]wireRegistered, len(v.Registered)), Recent: make([]wireRecord, len(v.Recent)),
	}
	for i, g := range v.Registered {
		w := wireRegistered{
			Name: g.Name, Owner: g.Owner, EverySeconds: int64(g.Every / time.Second),
			TimeoutSeconds: int64(g.Timeout / time.Second), NextDue: g.NextDue,
		}
		if g.Last != nil {
			rec := wireOf(*g.Last)
			w.Last = &rec
		}
		out.Registered[i] = w
	}
	for i, rec := range v.Recent {
		out.Recent[i] = wireOf(rec)
	}
	return out
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil && !errs.Is(err, errs.Canceled) {
		httpx.Fail(w, r, err)
	}
}

// ---- handlers ----------------------------------------------------------------------------

func (r *Runner) statusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		v, err := r.Status(req.Context())
		if err != nil {
			httpx.Fail(w, req, err)
			return
		}
		writeJSON(w, req, http.StatusOK, wireStatusOf(v))
	})
}

func (r *Runner) runNowHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// The name is checked against the grammar inside RunNow before any lookup.
		id, err := r.RunNow(req.Context(), Name(req.PathValue("name")))
		if err != nil {
			httpx.Fail(w, req, err)
			return
		}
		writeJSON(w, req, http.StatusAccepted, struct {
			ID JobID `json:"id"`
		}{id})
	})
}

// jobID reads {job} from the path; a malformed ID is errs.Invalid.
func jobID(req *http.Request) (JobID, error) {
	id, err := model.ParseID[JobID](req.PathValue("job"))
	if err != nil {
		return JobID{}, err //nolint:wrapcheck // ParseID returns errs.Invalid
	}
	return id, nil
}

func (r *Runner) retryHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id, err := jobID(req)
		if err == nil {
			err = r.Retry(req.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, req, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (r *Runner) cancelHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id, err := jobID(req)
		if err == nil {
			err = r.Cancel(req.Context(), id)
		}
		if err != nil {
			httpx.Fail(w, req, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
