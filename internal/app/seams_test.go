package app

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const seamDoc = "../../plan/07-abstractions.md"

// seamCandidates parses the non-test .go files in dir and returns the names of the
// late-bound candidates: a struct with a *assembly field, any type named lazy*, and a
// top-level function taking or receiving *assembly, returning a named (non-builtin)
// type or a func type, with a function literal that references the assembly. assembly and process are
// excluded by name.
func seamCandidates(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, s := range d.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok || ts.Name.Name == "assembly" || ts.Name.Name == "process" {
						continue
					}
					if strings.HasPrefix(ts.Name.Name, "lazy") || hasAssemblyField(ts.Type) {
						out[ts.Name.Name] = kindType
					}
				}
			case *ast.FuncDecl:
				if closureOverAssembly(d) {
					out[funcKey(d)] = kindClosure
				}
			}
		}
	}
	return out
}

// Candidate kinds: a type, or a function whose literal captures the assembly.
const (
	kindType    = "type"
	kindClosure = "closure"
)

// funcKey names a function in the seam table: the bare name for a top-level function,
// and "Recv.name" (for example "assembly.demoChannelFactory") for a method.
func funcKey(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	t := d.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + d.Name.Name
	}
	return d.Name.Name
}

func isAssemblyPtr(e ast.Expr) bool {
	s, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == "assembly"
}

func hasAssemblyField(e ast.Expr) bool {
	st, ok := e.(*ast.StructType)
	if !ok {
		return false
	}
	for _, f := range st.Fields.List {
		if isAssemblyPtr(f.Type) {
			return true
		}
	}
	return false
}

var builtinTypes = map[string]bool{"error": true, "any": true, "string": true, "bool": true,
	"int": true, "int64": true, "uint": true, "float64": true, "byte": true, "rune": true}

// closureOverAssembly is rule 5: a function with an *assembly parameter, a named
// non-builtin result, and a function literal that uses that parameter or receiver in any way.
func closureOverAssembly(d *ast.FuncDecl) bool {
	if d.Body == nil || d.Type.Results == nil {
		return false
	}
	var params []string
	if d.Recv != nil {
		for _, p := range d.Recv.List {
			if isAssemblyPtr(p.Type) {
				for _, n := range p.Names {
					params = append(params, n.Name)
				}
			}
		}
	}
	for _, p := range d.Type.Params.List {
		if isAssemblyPtr(p.Type) {
			for _, n := range p.Names {
				params = append(params, n.Name)
			}
		}
	}
	if len(params) == 0 {
		return false
	}
	named := false
	for _, r := range d.Type.Results.List {
		switch x := r.Type.(type) {
		case *ast.SelectorExpr, *ast.FuncType:
			named = true
		case *ast.Ident:
			named = named || !builtinTypes[x.Name]
		}
	}
	if !named {
		return false
	}
	found := false
	ast.Inspect(d.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return !found
		}
		ast.Inspect(lit.Body, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.SelectorExpr:
				// Only the operand can be a capture; x.a with Sel named like the
				// parameter is not.
				ast.Inspect(x.X, func(k ast.Node) bool {
					if id, ok := k.(*ast.Ident); ok && isParam(id.Name, params) {
						found = true
					}
					return !found
				})
				return false
			case *ast.Ident:
				if isParam(x.Name, params) {
					found = true
				}
			}
			return !found
		})
		return false
	})
	return found
}

func isParam(name string, params []string) bool {
	for _, p := range params {
		if p == name {
			return true
		}
	}
	return false
}

var rowName = regexp.MustCompile("^\\s*\\|\\s*`([^`]+)`")

// parseSeamRows reads the "Late-bound seams" section of doc. The heading must exist. The
// body is either "None." on a line of its own (no rows) or a table with at least one
// row; neither, or both, is an error.
func parseSeamRows(doc string) ([]string, error) {
	var rows []string
	found, none := false, false
	in := false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## ") {
			is := strings.TrimSpace(strings.TrimPrefix(line, "## ")) == "Late-bound seams"
			found = found || is
			in = is
			continue
		}
		if !in {
			continue
		}
		if strings.TrimSpace(line) == "None." {
			none = true
		}
		if m := rowName.FindStringSubmatch(line); m != nil {
			rows = append(rows, m[1])
		}
	}
	switch {
	case !found:
		return nil, errors.New(`no "Late-bound seams" section`)
	case none && len(rows) > 0:
		return nil, errors.New(`"Late-bound seams" says None. and also lists rows`)
	case !none && len(rows) == 0:
		return nil, errors.New(`"Late-bound seams" has neither rows nor "None."`)
	}
	return rows, nil
}

// seamRows returns the backticked first column of each row in the "Late-bound seams"
// section of the document at path; nil for "None.". Any other shape is fatal.
func seamRows(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: path is the fixed plan document or a testdata fixture
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	rows, err := parseSeamRows(string(b))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return rows
}

// seamProblems compares candidates with rows and returns the failure messages. declared
// is the set of every type and top-level function name in the package.
func seamProblems(cands map[string]string, rows []string, declared map[string]bool) []string {
	var out []string
	listed := map[string]bool{}
	for _, r := range rows {
		listed[r] = true
	}
	for c, kind := range cands {
		if listed[c] {
			continue
		}
		if kind == kindClosure {
			out = append(out, fmt.Sprintf("late-bound closure %s is not in the seam table: name the type and list it, or remove it", c))
		} else {
			out = append(out, fmt.Sprintf("late-bound adapter %s is not in the seam table", c))
		}
	}
	for _, r := range rows {
		switch _, isCand := cands[r]; {
		case !declared[r]:
			out = append(out, fmt.Sprintf("seam table row %s names no type or function in app", r))
		case !isCand:
			out = append(out, fmt.Sprintf("seam table row %s is no longer late-bound; remove it", r))
		}
	}
	sort.Strings(out)
	return out
}

// declaredNames returns every top-level type and function name in the non-test files.
func declaredNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok {
						out[ts.Name.Name] = true
					}
				}
			case *ast.FuncDecl:
				out[funcKey(d)] = true
			}
		}
	}
	return out
}

// TestSeamTable_MatchesApp: every late-bound candidate in app is in the seam table
// and every row names something that exists and is still late-bound (boundaries.md,
// seam table check, 1-5). An empty table ("None.") means no candidate may exist.
func TestSeamTable_MatchesApp(t *testing.T) {
	rows := seamRows(t, seamDoc)
	cands := seamCandidates(t, ".")
	if len(rows) == 0 && len(cands) > 0 {
		t.Errorf("seam table is None. but %d late-bound candidates exist", len(cands))
	}
	for _, p := range seamProblems(cands, rows, declaredNames(t, ".")) {
		t.Error(p)
	}
}

// wantFixture is every candidate in testdata/seams/unlisted.go.
var wantFixture = []string{"holder", "lazyThing", "makeDoer", "assembly.methodDoer",
	"channelDoer", "guardDoer", "makeFunc"}

// TestSeamTable_FlagsUnlistedType: candidates in the fixture, absent from the table,
// are reported by name, and excluded names are not.
func TestSeamTable_FlagsUnlistedType(t *testing.T) {
	dir := filepath.Join("testdata", "seams")
	cands := seamCandidates(t, dir)
	probs := seamProblems(cands, nil, declaredNames(t, dir))
	got := strings.Join(probs, "\n")
	for _, want := range []string{"holder", "lazyThing"} {
		msg := fmt.Sprintf("late-bound adapter %s is not in the seam table", want)
		if !strings.Contains(got, msg) {
			t.Errorf("missing problem %q in:\n%s", msg, got)
		}
	}
	for _, want := range []string{"makeDoer", "assembly.methodDoer", "channelDoer", "guardDoer", "makeFunc"} {
		msg := fmt.Sprintf("late-bound closure %s is not in the seam table: name the type and list it, or remove it", want)
		if !strings.Contains(got, msg) {
			t.Errorf("missing problem %q in:\n%s", msg, got)
		}
	}
	for _, not := range []string{"assembly", "process", "plain", "doer", "Doer", "closers", "assembly.closers", "noCapture"} {
		if _, ok := cands[not]; ok {
			t.Errorf("%s must not be a candidate", not)
		}
	}
	if len(probs) != len(wantFixture) {
		t.Errorf("want exactly %d problems, got %d:\n%s", len(wantFixture), len(probs), got)
	}
	// Listing them clears the problems.
	if p := seamProblems(cands, wantFixture, declaredNames(t, dir)); len(p) != 0 {
		t.Errorf("listed candidates still flagged: %v", p)
	}
}

// TestSeamTable_FlagsStaleRow: a row naming nothing in app is reported.
func TestSeamTable_FlagsStaleRow(t *testing.T) {
	rows := append(seamRows(t, seamDoc), "lazyGone")
	probs := seamProblems(seamCandidates(t, "."), rows, declaredNames(t, "."))
	want := "seam table row lazyGone names no type or function in app"
	if len(probs) != 1 || probs[0] != want {
		t.Errorf("want only %q, got %v", want, probs)
	}
}

// TestSeamTable_FlagsRowNoLongerLateBound: a row naming a declared type that is not a
// candidate is reported.
func TestSeamTable_FlagsRowNoLongerLateBound(t *testing.T) {
	dir := filepath.Join("testdata", "seams")
	rows := append(append([]string{}, wantFixture...), "plain", "assembly.closers")
	probs := seamProblems(seamCandidates(t, dir), rows, declaredNames(t, dir))
	want := []string{
		"seam table row assembly.closers is no longer late-bound; remove it",
		"seam table row plain is no longer late-bound; remove it",
	}
	if strings.Join(probs, "|") != strings.Join(want, "|") {
		t.Errorf("want %v, got %v", want, probs)
	}
}

// TestSeamRows_MissingSection: no "Late-bound seams" heading is an error.
func TestSeamRows_MissingSection(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "seams", "nosection.md"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSeamRows(string(b)); err == nil {
		t.Error("want an error for a document without the section")
	}
}

// TestSeamRows_None: "None." means zero rows and no error; the check then flags any
// candidate.
func TestSeamRows_None(t *testing.T) {
	rows := seamRows(t, filepath.Join("testdata", "seams", "none.md"))
	if len(rows) != 0 {
		t.Fatalf("want no rows, got %v", rows)
	}
	dir := filepath.Join("testdata", "seams")
	if p := seamProblems(seamCandidates(t, dir), rows, declaredNames(t, dir)); len(p) != len(wantFixture) {
		t.Errorf("want %d problems with an empty table, got %v", len(wantFixture), p)
	}
	if p := seamProblems(map[string]string{}, rows, map[string]bool{}); len(p) != 0 {
		t.Errorf("empty table and no candidates must pass, got %v", p)
	}
}

// TestSeamRows_Malformed: a section with neither rows nor "None.", or with both, is an
// error, and a row-bearing table parses.
func TestSeamRows_Malformed(t *testing.T) {
	cases := []struct {
		name, doc string
		wantErr   bool
		wantRows  int
	}{
		{"neither", "## Late-bound seams\n\nprose only\n\n## Next\n", true, 0},
		{"both", "## Late-bound seams\n\nNone.\n\n| `x` | a | b |\n", true, 0},
		{"rows", "## Late-bound seams\n\n| Type | W | R |\n|---|---|---|\n| `x` | a | b |\n", false, 1},
		{"none outside section", "## Other\n\nNone.\n\n## Late-bound seams\n\nprose\n", true, 0},
		{"none inline in prose", "## Late-bound seams\n\n\"None.\" is empty.\n", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := parseSeamRows(tc.doc)
			if (err != nil) != tc.wantErr || len(rows) != tc.wantRows {
				t.Errorf("err=%v rows=%v", err, rows)
			}
		})
	}
}
