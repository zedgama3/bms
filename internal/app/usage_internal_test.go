package app

import (
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/web"
)

func TestClientManifest_MapsScreensActionsAndSets(t *testing.T) {
	old := webManifest
	t.Cleanup(func() { webManifest = old })
	webManifest = func() (web.Manifest, error) {
		return web.Manifest{
			Screens: []web.ScreenEntry{{Pattern: "/", Set: "shell"}, {Pattern: "/volunteers/shifts", Set: "volunteers"}},
			Actions: []web.ActionEntry{{Name: "shift.filter", Set: "volunteers"}},
		}, nil
	}
	l, err := clientManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(l.screens) != 2 || len(l.actions) != 1 || l.modules["/volunteers/shifts"] != "volunteers" {
		t.Errorf("lists = %+v", l)
	}
}

func TestClientManifest_Missing_ReturnsEmptyListsAndTheError(t *testing.T) {
	old := webManifest
	t.Cleanup(func() { webManifest = old })
	webManifest = func() (web.Manifest, error) {
		return web.Manifest{}, errs.E("web.ClientManifest", errs.NotFound, errors.New("missing"))
	}
	l, err := clientManifest()
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("err = %v, want NotFound", err)
	}
	if len(l.screens)+len(l.actions) != 0 || l.modules != nil {
		t.Errorf("lists = %+v, want empty", l)
	}
}
