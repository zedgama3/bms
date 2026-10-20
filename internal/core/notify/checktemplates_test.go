package notify_test

import (
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestCheckTemplates_Table(t *testing.T) {
	good := "Subject: Hi {{.Site.Name}}\nVars: word\n\nSaid {{.Vars.word}}"
	tests := []struct {
		name     string
		owner    string
		defaults fstest.MapFS
		nilFS    bool
		wantErr  bool
	}{
		{"nil defaults pass", "hello", nil, true, false},
		{"empty tree passes", "hello", fstest.MapFS{}, false, false},
		{"en and es pass", "hello", fstest.MapFS{
			"greeted.en.tmpl": {Data: []byte(good)}, "greeted.es.tmpl": {Data: []byte(good)}}, false, false},
		{"missing en", "hello", fstest.MapFS{"greeted.es.tmpl": {Data: []byte(good)}}, false, true},
		{"no subject header", "hello", fstest.MapFS{"greeted.en.tmpl": {Data: []byte("body only")}}, false, true},
		{"unparsable body", "hello", fstest.MapFS{"greeted.en.tmpl": {Data: []byte("Subject: x\n\n{{.Open")}}, false, true},
		{"bad file name", "hello", fstest.MapFS{"greeted.tmpl": {Data: []byte(good)}}, false, true},
		{"bad owner", "Hello", fstest.MapFS{"greeted.en.tmpl": {Data: []byte(good)}}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.nilFS {
				err = notify.CheckTemplates(tc.owner, nil)
			} else {
				err = notify.CheckTemplates(tc.owner, tc.defaults)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("CheckTemplates = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v, want Invalid", errs.KindOf(err))
			}
		})
	}
}
