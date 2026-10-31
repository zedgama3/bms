package app

import (
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
// type, with a function literal that references the assembly. assembly and process are
// excluded by name.
func seamCandidates(t *testing.T, dir string) map[string]bool {
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
					ts, ok := s.(*ast.TypeSpec)
					if !ok || ts.Name.Name == "assembly" || ts.Name.Name == "process" {
						continue
					}
					if strings.HasPrefix(ts.Name.Name, "lazy") || hasAssemblyField(ts.Type) {
						out[ts.Name.Name] = true
					}
				}
			case *ast.FuncDecl:
				if closureOverAssembly(d) {
					out[funcKey(d)] = true
				}
			}
		}
	}
	return out
}

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
// non-builtin result, and a function literal that selects from that parameter.
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
		case *ast.SelectorExpr:
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
			if sel, ok := m.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok {
					for _, p := range params {
						if id.Name == p {
							found = true
						}
					}
				}
			}
			return !found
		})
		return false
	})
	return found
}

var rowName = regexp.MustCompile("^\\s*\\|\\s*`([^`]+)`")

// seamRows returns the backticked first column of each row in the "Late-bound seams"
// section of the document at path.
func seamRows(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: path is the fixed plan document or a testdata fixture
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var rows []string
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == "Late-bound seams"
			continue
		}
		if !in {
			continue
		}
		if m := rowName.FindStringSubmatch(line); m != nil {
			rows = append(rows, m[1])
		}
	}
	return rows
}

// seamProblems compares candidates with rows and returns the failure messages. declared
// is the set of every type and top-level function name in the package.
func seamProblems(cands map[string]bool, rows []string, declared map[string]bool) []string {
	var out []string
	listed := map[string]bool{}
	for _, r := range rows {
		listed[r] = true
	}
	for c := range cands {
		if !listed[c] {
			out = append(out, fmt.Sprintf("late-bound adapter %s is not in the seam table", c))
		}
	}
	for _, r := range rows {
		if !declared[r] {
			out = append(out, fmt.Sprintf("seam table row %s names no type or function in app", r))
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
// and every row names something that exists (boundaries.md, seam table check, 1-5).
func TestSeamTable_MatchesApp(t *testing.T) {
	rows := seamRows(t, seamDoc)
	if len(rows) == 0 {
		t.Fatal("no rows found in the Late-bound seams section")
	}
	for _, p := range seamProblems(seamCandidates(t, "."), rows, declaredNames(t, ".")) {
		t.Error(p)
	}
}

// TestSeamTable_FlagsUnlistedType: candidates in the fixture, absent from the table,
// are reported by name, and excluded names are not.
func TestSeamTable_FlagsUnlistedType(t *testing.T) {
	dir := filepath.Join("testdata", "seams")
	cands := seamCandidates(t, dir)
	probs := seamProblems(cands, nil, declaredNames(t, dir))
	got := strings.Join(probs, "\n")
	for _, want := range []string{"holder", "lazyThing", "makeDoer", "assembly.methodDoer"} {
		msg := fmt.Sprintf("late-bound adapter %s is not in the seam table", want)
		if !strings.Contains(got, msg) {
			t.Errorf("missing problem %q in:\n%s", msg, got)
		}
	}
	for _, not := range []string{"assembly", "process", "plain", "doer", "Doer"} {
		if cands[not] {
			t.Errorf("%s must not be a candidate", not)
		}
	}
	if len(probs) != 4 {
		t.Errorf("want exactly 4 problems, got %d:\n%s", len(probs), got)
	}
	// Listing them clears the problems.
	if p := seamProblems(cands, []string{"holder", "lazyThing", "makeDoer", "assembly.methodDoer"}, declaredNames(t, dir)); len(p) != 0 {
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
