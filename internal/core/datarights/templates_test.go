package datarights_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/datarights"
	"github.com/zedgama3/bms/internal/core/notify"
)

// notify.Register would refuse these files, so check them the way Register does.
func TestTemplates_PassNotifyChecks(t *testing.T) {
	if err := notify.CheckTemplates("datarights", datarights.Templates); err != nil {
		t.Fatalf("CheckTemplates: %v", err)
	}
}

func TestTemplates_DeletionRequested_EnAndEsOnly_NamesNobody(t *testing.T) {
	entries, err := fs.ReadDir(datarights.Templates, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		raw, err := fs.ReadFile(datarights.Templates, e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		// The notice has no Vars and does not address the recipient by name; it must
		// never grow a field about the requester (decision 27).
		for _, bad := range []string{".Vars", ".Recipient", "Vars:"} {
			if strings.Contains(string(raw), bad) {
				t.Errorf("%s uses %q", e.Name(), bad)
			}
		}
		if !strings.Contains(string(raw), "{{.Link}}") {
			t.Errorf("%s has no link to the requests page", e.Name())
		}
	}
	slices.Sort(names)
	if want := []string{"deletion-requested.en.tmpl", "deletion-requested.es.tmpl"}; !slices.Equal(names, want) {
		t.Errorf("template files = %v, want %v", names, want)
	}
}
