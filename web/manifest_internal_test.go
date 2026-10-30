package web

import (
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestManifestFrom_Table(t *testing.T) {
	const ok = `{"screens":[{"pattern":"/","set":"shell"},{"pattern":"/volunteers/shifts/{id}","set":"volunteers"}],` +
		`"actions":[{"name":"shift.filter","set":"volunteers"}],"future":true}`
	for _, tc := range []struct {
		name     string
		file     string // "" means the file is absent
		wantKind errs.Kind
		wantErr  bool
		screens  int
		actions  int
	}{
		{name: "valid", file: ok, screens: 2, actions: 1},
		{name: "empty lists", file: `{"screens":[],"actions":[]}`},
		{name: "missing", wantErr: true, wantKind: errs.NotFound},
		{name: "not json", file: `{"screens":`, wantErr: true, wantKind: errs.Invalid},
		{name: "trailing data", file: `{} {}`, wantErr: true, wantKind: errs.Invalid},
		{name: "pattern without slash", file: `{"screens":[{"pattern":"people","set":"people"}]}`, wantErr: true, wantKind: errs.Invalid},
		{name: "pattern with a space", file: `{"screens":[{"pattern":"/a b","set":"x"}]}`, wantErr: true, wantKind: errs.Invalid},
		{name: "set not a token", file: `{"screens":[{"pattern":"/a","set":"a b"}]}`, wantErr: true, wantKind: errs.Invalid},
		{name: "action not a token", file: `{"actions":[{"name":"a b","set":"x"}]}`, wantErr: true, wantKind: errs.Invalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fstest.MapFS{}
			if tc.file != "" {
				root[manifestName] = &fstest.MapFile{Data: []byte(tc.file)}
			}
			m, err := manifestFrom("web.ClientManifest", root)
			if tc.wantErr {
				if !errs.Is(err, tc.wantKind) {
					t.Fatalf("err = %v, want kind %v", err, tc.wantKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(m.Screens) != tc.screens || len(m.Actions) != tc.actions {
				t.Errorf("manifest = %+v", m)
			}
		})
	}
}
