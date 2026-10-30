package web

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Tests for web.ClientManifest (telemetry Amendment T1, F72).

const goodManifest = `{"screens":[{"pattern":"/","set":"shell"},{"pattern":"/volunteers/shifts/{id}","set":"volunteers"}],` +
	`"actions":[{"name":"shift.filter","set":"volunteers"}]}`

func TestClientManifest_MissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	_, err := manifestFrom("web.ClientManifest", fstest.MapFS{"index.html": {Data: []byte("x")}})
	if !errs.Is(err, errs.NotFound) {
		t.Fatalf("kind = %v (%v), want NotFound", errs.KindOf(err), err)
	}
}

func TestClientManifest_ReadError_ClassifiesInternal(t *testing.T) {
	t.Parallel()
	_, err := manifestFrom("web.ClientManifest", failingFS{})
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("kind = %v (%v), want Internal", errs.KindOf(err), err)
	}
}

func TestClientManifest_Malformed_ReturnsInvalid(t *testing.T) {
	t.Parallel()
	cases := map[string]string{ //nolint:gosec // G101: malformed-manifest inputs, not credentials
		"not json":                   `{"screens": [`,
		"wrong shape":                `{"screens":"/"}`,
		"empty file":                 ``,
		"screen without slash":       `{"screens":[{"pattern":"volunteers","set":"v"}],"actions":[]}`,
		"screen pattern empty":       `{"screens":[{"pattern":"","set":"v"}],"actions":[]}`,
		"screen pattern with query":  `{"screens":[{"pattern":"/a?x=1","set":"v"}],"actions":[]}`,
		"screen pattern with space":  `{"screens":[{"pattern":"/a b","set":"v"}],"actions":[]}`,
		"screen pattern is e-mail":   `{"screens":[{"pattern":"/zq9@marker.example","set":"v"}],"actions":[]}`,
		"screen set not a token":     `{"screens":[{"pattern":"/","set":"a b"}],"actions":[]}`,
		"screen set empty":           `{"screens":[{"pattern":"/","set":""}],"actions":[]}`,
		"action not a token":         `{"screens":[],"actions":[{"name":"shift filter","set":"v"}]}`,
		"action empty":               `{"screens":[],"actions":[{"name":"","set":"v"}]}`,
		"action set not a token":     `{"screens":[],"actions":[{"name":"a.b","set":"x?y"}]}`,
		"one bad among good screens": `{"screens":[{"pattern":"/","set":"s"},{"pattern":"/a b","set":"s"}],"actions":[]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := manifestFrom("web.ClientManifest", fstest.MapFS{"client-manifest.json": {Data: []byte(body)}})
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v (%v), want Invalid", errs.KindOf(err), err)
			}
			if strings.Contains(err.Error(), "zq9@marker.example") {
				t.Error("the error echoes the offending value")
			}
		})
	}
}

func TestClientManifest_RoundTrip(t *testing.T) {
	t.Parallel()
	m, err := manifestFrom("web.ClientManifest", fstest.MapFS{"client-manifest.json": {Data: []byte(goodManifest)}})
	if err != nil {
		t.Fatalf("manifestFrom: %v", err)
	}
	if len(m.Screens) != 2 || m.Screens[1].Pattern != "/volunteers/shifts/{id}" || m.Screens[1].Set != "volunteers" ||
		len(m.Actions) != 1 || m.Actions[0].Name != "shift.filter" || m.Actions[0].Set != "volunteers" {
		t.Fatalf("manifest = %+v", m)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var again Manifest
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if len(again.Screens) != 2 || len(again.Actions) != 1 || again.Screens[0] != m.Screens[0] || again.Actions[0] != m.Actions[0] {
		t.Errorf("round trip changed the manifest: %+v", again)
	}
}

func TestClientManifest_EmptyListsAreValid(t *testing.T) {
	t.Parallel()
	m, err := manifestFrom("web.ClientManifest", fstest.MapFS{"client-manifest.json": {Data: []byte(`{"screens":[],"actions":[]}`)}})
	if err != nil || len(m.Screens) != 0 || len(m.Actions) != 0 {
		t.Fatalf("m=%+v err=%v", m, err)
	}
}

func TestClientManifest_UnknownFieldsIgnored(t *testing.T) {
	t.Parallel()
	body := `{"screens":[{"pattern":"/","set":"shell","future":1}],"actions":[],"extra":true}`
	if _, err := manifestFrom("web.ClientManifest", fstest.MapFS{"client-manifest.json": {Data: []byte(body)}}); err != nil {
		t.Fatalf("err = %v", err)
	}
}

// TestClientManifest_MatchesBuild runs over the real embedded dist. A checkout without a
// build (or an older build) has no manifest: that is NotFound, never another kind.
func TestClientManifest_MatchesBuild(t *testing.T) {
	t.Parallel()
	m, err := ClientManifest()
	if err != nil {
		if !errs.Is(err, errs.NotFound) {
			t.Fatalf("ClientManifest: kind %v (%v)", errs.KindOf(err), err)
		}
		t.Skip("no client-manifest.json in dist: run `make web-build`")
	}
	if len(m.Screens) == 0 {
		t.Error("a built manifest lists no screens")
	}
	seen := map[string]bool{}
	for _, s := range m.Screens {
		if seen[s.Pattern] {
			t.Errorf("duplicate screen %q", s.Pattern)
		}
		seen[s.Pattern] = true
	}
}
