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

// seamCandidates is seamScan without the stored-literal failures.
func seamCandidates(t *testing.T, dir string) map[string]string {
	t.Helper()
	c, _ := seamScan(t, dir)
	return c
}

// storedLiterals returns the failures for func literals that capture the assembly and
// are stored in a project package's struct literal (rule 5b). They fail whether or not
// the table lists anything, because a literal has no declaration a row could name.
func storedLiterals(t *testing.T, dir string) []string {
	t.Helper()
	_, l := seamScan(t, dir)
	return l
}

// seamScan parses the non-test .go files in dir and returns the late-bound candidates:
// a struct with a *assembly field, any type named lazy*, a top-level function taking or
// receiving *assembly, returning a named (non-builtin) type or a func type, that holds a
// capture (5a), and a method value a.m stored in a project package's struct literal
// (5b, keyed assembly.m). It also returns the stored-literal failures. assembly and
// process are excluded by name.
func seamScan(t *testing.T, dir string) (map[string]string, []string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]string{}
	var lits []string
	fset := token.NewFileSet()
	var files []*ast.File
	methods := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		files = append(files, f)
		// First pass: the methods declared on assembly.
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil && strings.HasPrefix(funcKey(fd), "assembly.") {
				methods[fd.Name.Name] = true
			}
		}
	}
	for _, f := range files {
		imports := fileImports(f)
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
				held, stored := captures(d, methods, imports)
				if held && returnsSeamShape(d) {
					out[funcKey(d)] = kindClosure
				}
				for _, c := range stored {
					if c.lit {
						lits = append(lits, fmt.Sprintf("func literal in %s captures the assembly and is stored in %s: extract it to a named function or type and list it, or remove it", funcKey(d), c.typ))
					} else {
						out["assembly."+c.method] = kindClosure
					}
				}
			}
		}
	}
	sort.Strings(lits)
	return out, lits
}

// fileImports maps each import name in f to its path.
func fileImports(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, im := range f.Imports {
		path := strings.Trim(im.Path.Value, "\"`")
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		out[name] = path
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

// assemblyParams returns the names of d's receiver and parameters of type *assembly.
func assemblyParams(d *ast.FuncDecl) []string {
	var params []string
	var lists []*ast.FieldList
	if d.Recv != nil {
		lists = append(lists, d.Recv)
	}
	if d.Type.Params != nil {
		lists = append(lists, d.Type.Params)
	}
	for _, l := range lists {
		for _, p := range l.List {
			if isAssemblyPtr(p.Type) {
				for _, n := range p.Names {
					params = append(params, n.Name)
				}
			}
		}
	}
	return params
}

// returnsSeamShape reports whether d has a result list containing a named non-builtin
// type or a func type (rule 5a).
func returnsSeamShape(d *ast.FuncDecl) bool {
	if d.Type.Results == nil {
		return false
	}
	for _, r := range d.Type.Results.List {
		switch x := r.Type.(type) {
		case *ast.SelectorExpr, *ast.FuncType:
			return true
		case *ast.Ident:
			if !builtinTypes[x.Name] {
				return true
			}
		}
	}
	return false
}

// usesParam reports whether n uses one of params as an identifier. A selector's Sel and
// a composite literal's key are names, not uses.
func usesParam(n ast.Node, params []string) bool {
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		switch x := m.(type) {
		case *ast.SelectorExpr:
			if usesParam(x.X, params) {
				found = true
			}
			return false
		case *ast.KeyValueExpr:
			if usesParam(x.Value, params) {
				found = true
			}
			return false
		case *ast.Ident:
			if isParam(x.Name, params) {
				found = true
			}
		}
		return !found
	})
	return found
}

// storedCapture is a capture that is the value of a field of a project package's struct
// literal (rule 5b).
type storedCapture struct {
	lit    bool
	method string // the method name, for a method value
	typ    string // pkg.T, for a literal
}

// captures walks d's body. held reports whether d holds any capture (a func literal using
// the assembly, or an uncalled method value a.m); stored lists those that are 5b.
func captures(d *ast.FuncDecl, methods map[string]bool, imports map[string]string) (held bool, stored []storedCapture) {
	params := assemblyParams(d)
	if d.Body == nil || len(params) == 0 {
		return false, nil
	}
	called := map[ast.Node]bool{}
	var stack []ast.Node
	// compType returns the type expression of the composite literal at stack[i]. An
	// elided type (a {..} element of a slice, array or map literal) takes its element type
	// from the next enclosing literal, with a pointer stripped.
	var compType func(i int) ast.Expr
	compType = func(i int) ast.Expr {
		comp, ok := stack[i].(*ast.CompositeLit)
		if !ok {
			return nil
		}
		if comp.Type != nil {
			return comp.Type
		}
		j := i - 1
		if j >= 0 {
			if _, ok := stack[j].(*ast.KeyValueExpr); ok {
				j--
			}
		}
		if j < 0 {
			return nil
		}
		if _, ok := stack[j].(*ast.CompositeLit); !ok {
			return nil
		}
		var elem ast.Expr
		switch t := compType(j).(type) {
		case *ast.ArrayType:
			elem = t.Elt
		case *ast.MapType:
			elem = t.Value
		}
		if st, ok := elem.(*ast.StarExpr); ok {
			elem = st.X
		}
		return elem
	}
	storedIn := func() string {
		// stack holds the ancestors of the capture; its top is the parent.
		n := len(stack)
		ci := -1
		if n >= 1 {
			switch stack[n-1].(type) {
			case *ast.CompositeLit:
				ci = n - 1
			case *ast.KeyValueExpr:
				if n >= 2 {
					if _, ok := stack[n-2].(*ast.CompositeLit); ok {
						ci = n - 2
					}
				}
			}
		}
		if ci < 0 {
			return ""
		}
		sel, ok := compType(ci).(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || !strings.HasPrefix(imports[id.Name], "github.com/zedgama3/bms/") {
			return ""
		}
		return id.Name + "." + sel.Sel.Name
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch x := n.(type) {
		case *ast.CallExpr:
			called[x.Fun] = true
		case *ast.FuncLit:
			if usesParam(x.Body, params) {
				held = true
				if typ := storedIn(); typ != "" {
					stored = append(stored, storedCapture{lit: true, typ: typ})
				}
			}
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && isParam(id.Name, params) && methods[x.Sel.Name] && !called[x] {
				held = true
				if typ := storedIn(); typ != "" {
					stored = append(stored, storedCapture{method: x.Sel.Name, typ: typ})
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	return held, stored
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
	cands, lits := seamScan(t, ".")
	if len(rows) == 0 && len(cands) > 0 {
		t.Errorf("seam table is None. but %d late-bound candidates exist", len(cands))
	}
	for _, p := range matchProblems(cands, lits, rows, declaredNames(t, ".")) {
		t.Error(p)
	}
}

// matchProblems is the whole MatchesApp evaluation: the table problems plus every stored
// literal, which no row can clear.
func matchProblems(cands map[string]string, lits, rows []string, declared map[string]bool) []string {
	return append(seamProblems(cands, rows, declared), lits...)
}

// wantFixture is every candidate in testdata/seams/unlisted.go.
var wantFixture = []string{"holder", "lazyThing", "makeDoer", "assembly.methodDoer",
	"channelDoer", "guardDoer", "makeFunc", "assembly.send"}

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
	for _, want := range []string{"makeDoer", "assembly.methodDoer", "channelDoer", "guardDoer", "makeFunc", "assembly.send"} {
		msg := fmt.Sprintf("late-bound closure %s is not in the seam table: name the type and list it, or remove it", want)
		if !strings.Contains(got, msg) {
			t.Errorf("missing problem %q in:\n%s", msg, got)
		}
	}
	for _, not := range []string{"assembly", "process", "plain", "doer", "Doer", "closers", "assembly.closers", "noCapture", "keyOnly", "selOnly", "assembly.run"} {
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

// TestSeamTable_FlagsStoredLiteral: a capturing literal stored in a project package's
// struct literal fails, listed or not (rule 5b, B31).
func TestSeamTable_FlagsStoredLiteral(t *testing.T) {
	dir := filepath.Join("testdata", "seams", "stored")
	want := []string{
		"func literal in assembly.storeElided captures the assembly and is stored in audit.Decl: extract it to a named function or type and list it, or remove it",
		"func literal in assembly.storeLiteral captures the assembly and is stored in audit.Options: extract it to a named function or type and list it, or remove it",
		"func literal in assembly.storeNested captures the assembly and is stored in n.Options: extract it to a named function or type and list it, or remove it",
		"func literal in storePositional captures the assembly and is stored in n.Decl: extract it to a named function or type and list it, or remove it",
	}
	got := storedLiterals(t, dir)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("want %v, got %v", want, got)
	}
	// Listing cannot clear it: a literal is no candidate, so no row can name it.
	cands, lits := seamScan(t, dir)
	for _, bad := range []string{"assembly.storeLiteral", "storePositional", "assembly.storeElided", "assembly.storeNested"} {
		if _, ok := cands[bad]; ok {
			t.Errorf("%s must not be a candidate", bad)
		}
	}
	rows := []string{"assembly.storeLiteral", "storePositional", "assembly.storeElided", "assembly.storeNested",
		"assembly.actor", "assembly.other", "assembly.later"}
	probs := matchProblems(cands, lits, rows, declaredNames(t, dir))
	for _, w := range want {
		found := false
		for _, p := range probs {
			found = found || p == w
		}
		if !found {
			t.Errorf("listed, the literal failure %q must remain; got %v", w, probs)
		}
	}
}

// TestSeamTable_FlagsStoredMethodValue: a method value stored in a project package's
// struct literal is the candidate assembly.<m>; unlisted it is reported, listed it clears
// (rule 5b, B32).
func TestSeamTable_FlagsStoredMethodValue(t *testing.T) {
	dir := filepath.Join("testdata", "seams", "stored")
	cands := seamCandidates(t, dir)
	names := []string{"assembly.actor", "assembly.later", "assembly.other"}
	if len(cands) != len(names) {
		t.Fatalf("want exactly %v as candidates, got %v", names, cands)
	}
	var want []string
	for _, n := range names {
		if cands[n] != kindClosure {
			t.Fatalf("want %s as a closure candidate, got %v", n, cands)
		}
		want = append(want, "late-bound closure "+n+" is not in the seam table: name the type and list it, or remove it")
	}
	if p := seamProblems(cands, nil, declaredNames(t, dir)); strings.Join(p, "|") != strings.Join(want, "|") {
		t.Errorf("want %v, got %v", want, p)
	}
	if p := seamProblems(cands, names, declaredNames(t, dir)); len(p) != 0 {
		t.Errorf("listed method value still flagged: %v", p)
	}
}

// TestSeamTable_IgnoresWiring: captures in an app struct, a call argument, a stdlib
// composite literal, a local or a goroutine are neither candidates nor failures (rule 6).
func TestSeamTable_IgnoresWiring(t *testing.T) {
	dir := filepath.Join("testdata", "seams", "wiring")
	if c := seamCandidates(t, dir); len(c) != 0 {
		t.Errorf("wiring must not be a candidate, got %v", c)
	}
	if l := storedLiterals(t, dir); len(l) != 0 {
		t.Errorf("wiring must not fail, got %v", l)
	}
}
