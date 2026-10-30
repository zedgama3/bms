package app_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Amendment J-W1 (jobs.md W1.4, F63, F64): every registered job has a title in the English
// catalogues. The names come from `bms check`'s jobs section, the path that builds the
// runner and every compiled-in module against db.None() and calls each Start.

var jobLine = regexp.MustCompile(`^  (\S+)  owner (\S+)  `)

type jobEntry struct{ name, owner string }

// jobsSection returns the jobs `check` printed and the lines that say a module's Start failed.
func jobsSection(out string) (jobs []jobEntry, startFailures []string, found bool) {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if l != "jobs" {
			continue
		}
		found = true
		for _, l := range lines[i+1:] {
			if !strings.HasPrefix(l, "  ") {
				break
			}
			if m := jobLine.FindStringSubmatch(l); m != nil {
				jobs = append(jobs, jobEntry{name: m[1], owner: m[2]})
				continue
			}
			startFailures = append(startFailures, strings.TrimSpace(l))
		}
		break
	}
	return jobs, startFailures, found
}

// jobTitleProblems returns what is wrong with the check output against the catalogue: a
// missing jobs section, a Start that failed (the test cannot vouch for that module's names),
// and every missing owner or job title key. zq* owners are the app tests' invented modules.
func jobTitleProblems(out string, cat map[string]string) []string {
	jobs, failures, found := jobsSection(out)
	var problems []string
	if !found {
		return []string{"the check output has no jobs section"}
	}
	for _, f := range failures {
		problems = append(problems, "a module's Start failed: "+f)
	}
	if len(jobs) == 0 {
		problems = append(problems, "the jobs section lists no job")
	}
	seen := map[string]bool{}
	for _, j := range jobs {
		if strings.HasPrefix(j.owner, "zq") {
			continue
		}
		ident := strings.TrimPrefix(j.name, j.owner+".")
		for _, k := range []string{"module." + j.owner + ".title", "module." + j.owner + ".job." + ident + ".title"} {
			if _, ok := cat[k]; !ok && !seen[k] {
				seen[k] = true
				problems = append(problems, "missing catalogue key "+k)
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// englishCatalogue is the union of the shell's en catalogue and every
// web/src/{modules,core}/*/messages/en.json.
func englishCatalogue(t *testing.T, root string) map[string]string {
	t.Helper()
	paths := []string{filepath.Join(root, "web", "src", "shell", "messages", "en.json")}
	for _, dir := range []string{"modules", "core"} {
		m, err := filepath.Glob(filepath.Join(root, "web", "src", dir, "*", "messages", "en.json"))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, m...)
	}
	cat := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // fixed repository paths
		if err != nil {
			t.Fatalf("catalogue %s: %v", p, err)
		}
		var one map[string]string
		if err := json.Unmarshal(b, &one); err != nil {
			t.Fatalf("catalogue %s is not flat JSON: %v", p, err)
		}
		for k, v := range one {
			cat[k] = v
		}
	}
	return cat
}

// checkOutput runs `bms check` against a database nothing listens on and returns stdout.
func checkOutput(t *testing.T) string {
	t.Helper()
	env := []string{
		fmt.Sprintf("BMS_DB_URL=postgres://zquser:pw@127.0.0.1:%d/zqdb?sslmode=disable", freePort(t)),
		"BMS_SESSION_KEY=" + sessionKey,
		"BMS_SETTINGS_SEALING_KEY=" + sealingKey,
		"BMS_DATA_DIR=" + t.TempDir(),
		"BMS_LOG_FORMAT=json",
		"BMS_DB_CONNECT_TIMEOUT=2s",
	}
	return run(t.Context(), t, env, "check").out
}

// F63: a missing title is a build failure. Over the real tree.
func TestMessageCatalogues_EnglishCoversJobs(t *testing.T) {
	resetProbe(t)
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "web", "src")); err != nil {
		t.Skip("web/src is absent")
	}
	out := checkOutput(t)
	jobs, _, _ := jobsSection(out)
	// The core's own names are always among them.
	have := map[string]bool{}
	for _, j := range jobs {
		have[j.name] = true
	}
	for _, n := range []string{"audit.purge", "jobs.purge", "signals.redeliver", "files.reap", "files.sweep-orphans"} {
		if !have[n] {
			t.Errorf("the jobs section lacks the core job %s:\n%s", n, out)
		}
	}
	for _, p := range jobTitleProblems(out, englishCatalogue(t, root)) {
		t.Error(p)
	}
}

// The checker itself, over a fixture catalogue that lacks keys (F63).
func TestMessageCatalogues_FixtureMissingJobKey_Fails(t *testing.T) {
	out := "result: x\njobs\n  fixture.thing  owner fixture  every 1h0m0s\n  fixture.other-thing  owner fixture  on demand\n"
	cat := map[string]string{"module.fixture.title": "Fixture", "module.fixture.job.thing.title": "A thing"}
	got := jobTitleProblems(out, cat)
	if len(got) != 1 || !strings.Contains(got[0], "module.fixture.job.other-thing.title") {
		t.Errorf("problems = %v, want exactly the missing other-thing title", got)
	}
	cat["module.fixture.job.other-thing.title"] = "Another"
	if got := jobTitleProblems(out, cat); len(got) != 0 {
		t.Errorf("problems = %v, want none", got)
	}
	delete(cat, "module.fixture.title")
	if got := jobTitleProblems(out, cat); len(got) != 1 || !strings.Contains(got[0], "module.fixture.title") {
		t.Errorf("problems = %v, want the missing owner title", got)
	}
}

// F64: a module whose Start fails makes the test fail, because its names are unknown.
func TestMessageCatalogues_FixtureStartFails_Fails(t *testing.T) {
	resetProbe(t)
	ctl.Lock()
	ctl.startErr = kindErr(errs.Unavailable)
	ctl.Unlock()
	out := checkOutput(t)
	_, failures, found := jobsSection(out)
	if !found {
		t.Fatalf("the check output has no jobs section:\n%s", out)
	}
	if len(failures) == 0 {
		t.Fatalf("a failing Start left no line in the jobs section:\n%s", out)
	}
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "web", "src")); err != nil {
		t.Skip("web/src is absent")
	}
	problems := jobTitleProblems(out, englishCatalogue(t, root))
	var named bool
	for _, p := range problems {
		if strings.Contains(p, "Start failed") {
			named = true
		}
	}
	if !named {
		t.Errorf("a failing Start is not a problem: %v", problems)
	}
}
