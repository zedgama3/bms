package app

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/model"
)

// testRegistryOf returns a fresh registry holding one do-nothing module per name.
func testRegistryOf(t *testing.T, names ...string) *module.Registry {
	t.Helper()
	r := module.NewRegistry()
	for _, n := range names {
		err := r.Add(module.Manifest{Name: n}, func(context.Context, module.Deps) (module.Module, error) {
			return nil, nil //nolint:nilnil // never built in these tests
		})
		if err != nil {
			t.Fatalf("add %s: %v", n, err)
		}
	}
	return r
}

// ---- a recording module ----

// recorder collects the order in which the fakes were called.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(ev string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// recModule is a module with one public GET /ping route that records Start and Stop.
type recModule struct {
	name string
	rec  *recorder
}

func (m recModule) Start(context.Context) error { m.rec.add("start:" + m.name); return nil }
func (m recModule) Stop(context.Context) error  { m.rec.add("stop:" + m.name); return nil }

func (recModule) ExportPerson(context.Context, model.PersonID) (module.Export, error) {
	return module.Export{}, nil
}

func (recModule) DeletePerson(context.Context, model.PersonID) (module.Deletion, error) {
	return module.Deletion{}, nil
}

func (recModule) Routes() []module.Route { return []module.Route{pingRoute()} }
func (recModule) OpenAPI() []byte        { return pingFragment }

func pingRoute() module.Route {
	return module.Route{
		Method: "GET", Pattern: "/ping", Access: module.AccessPublic,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if _, err := w.Write([]byte("pong")); err != nil {
				return
			}
		}),
	}
}

var pingFragment = []byte(`{"paths":{"/ping":{"get":{"responses":{"200":{"description":"ok"}}}}}}`)

// recRegistry returns a fresh registry holding one recModule per name.
func recRegistry(t *testing.T, rec *recorder, names ...string) *module.Registry {
	t.Helper()
	r := module.NewRegistry()
	for _, n := range names {
		if err := r.Add(module.Manifest{Name: n}, func(context.Context, module.Deps) (module.Module, error) {
			return recModule{name: n, rec: rec}, nil
		}); err != nil {
			t.Fatalf("add %s: %v", n, err)
		}
	}
	return r
}
