package regional

import (
	"strings"
	"testing"
)

func TestInternalRenderTheme_GuardsEachScheme(t *testing.T) {
	const darkGuard, lightGuard = "@media (prefers-color-scheme: dark) { :root {", "@media not (prefers-color-scheme: dark) { :root {"
	t.Run("none", func(t *testing.T) {
		if css := renderTheme("", ""); css != "" {
			t.Errorf("css = %q", css)
		}
	})
	t.Run("light only leaves the shipped dark tokens alone", func(t *testing.T) {
		css := renderTheme("#9a3412", "")
		if !strings.HasPrefix(css, lightGuard) || strings.Contains(css, darkGuard) || strings.Count(css, ":root") != 1 {
			t.Errorf("css = %q", css)
		}
	})
	t.Run("dark only leaves the shipped light tokens alone", func(t *testing.T) {
		css := renderTheme("", "#fb923c")
		if !strings.HasPrefix(css, darkGuard) || strings.Contains(css, lightGuard) || strings.Count(css, ":root") != 1 {
			t.Errorf("css = %q", css)
		}
	})
	t.Run("both", func(t *testing.T) {
		css := renderTheme("#9a3412", "#fb923c")
		li, di := strings.Index(css, lightGuard), strings.Index(css, darkGuard)
		if li != 0 || di <= li || strings.Count(css, ":root") != 2 {
			t.Errorf("css = %q", css)
		}
		if strings.Index(css, "#9a3412") > di || strings.LastIndex(css, "#fb923c") < di {
			t.Errorf("a colour sits under the wrong guard: %q", css)
		}
	})
}
