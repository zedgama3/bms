package regional_test

import (
	"strings"
	"testing"
)

// The stylesheet from the profile's two colours, over a settings.Reader fake (moved from
// settings: TestThemeCSS_Rendering_Unit, which also set the colours through Set; the Public
// document's theme flag is checked in public_integration_test.go).
func TestThemeCSS_Rendering_Unit(t *testing.T) {
	r := newRig(t)
	ctx := t.Context()
	theme := func() string {
		t.Helper()
		css, err := r.svc.ThemeCSS(ctx)
		if err != nil {
			t.Fatalf("ThemeCSS: %v", err)
		}
		return css
	}
	if css := theme(); css != "" {
		t.Fatalf("default theme = %q; want empty", css)
	}

	r.rd.Set("color_primary", "#9a3412")
	css := theme()
	if !strings.HasPrefix(css, "@media not (prefers-color-scheme: dark) { :root {") || strings.Contains(css, "@media (prefers-color-scheme: dark)") {
		t.Fatalf("light only: css = %q", css)
	}
	if !strings.Contains(css, "--primary: #9a3412;") || !strings.Contains(css, "--primary-fg: #ffffff;") {
		t.Errorf("light only: css = %q", css)
	}

	r.rd.Set("color_primary_dark", "#f97316")
	css = theme()
	if strings.Count(css, ":root") != 2 || strings.Count(css, "@media (prefers-color-scheme: dark)") != 1 ||
		strings.Count(css, "@media not (prefers-color-scheme: dark)") != 1 {
		t.Fatalf("both: css = %q", css)
	}
	if !strings.Contains(css, "--primary-fg: #1a1a1a;") {
		t.Errorf("dark rule should use the dark foreground: %q", css)
	}

	r.rd.Set("color_primary", "")
	css = theme()
	if strings.Count(css, "--primary:") != 1 || !strings.Contains(css, "prefers-color-scheme: dark") || strings.Contains(css, "#9a3412") {
		t.Errorf("dark only: css = %q", css)
	}

	r.rd.Set("color_primary_dark", "")
	if css = theme(); css != "" {
		t.Errorf("reset to empty: css = %q", css)
	}
}
