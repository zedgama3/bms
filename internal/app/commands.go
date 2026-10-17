package app

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"runtime"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// commandNameRE is the grammar of a command name.
var commandNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// builtins are the six commands every build has, in `bms help` order.
func builtins() []command {
	return []command{
		{name: "serve", summary: "run the server until SIGINT or SIGTERM", run: runServe},
		{name: "migrate", summary: "create the secrets file on a new installation and apply migrations", run: runMigrate},
		{name: "check", summary: "report configuration, modules, database and schema; exit as serve would", run: runCheck},
		{name: "openapi", summary: "print the OpenAPI document (needs no configuration)", run: runOpenAPI},
		{name: "version", summary: "print the build version", run: runVersion},
		{name: "help", summary: "print this usage", run: runHelp},
	}
}

// commands returns the command table: the built-ins, then each part's, in part order. It
// fails with errs.Internal when two commands or two parts share a name or a name breaks
// its grammar (F28: a programming error, never the operator's).
func (p *process) commands() ([]command, error) {
	const op = "app.commands"
	a := &assembly{}
	seenPart := map[string]bool{}
	for _, pt := range p.parts {
		if pt.name == "" || seenPart[pt.name] {
			return nil, errs.E(op, errs.Internal, fmt.Errorf("core part name %q is empty or used twice", pt.name))
		}
		if !module.IsCoreArea(pt.name) {
			return nil, errs.E(op, errs.Internal, fmt.Errorf("core part name %q is not a reserved core area name", pt.name))
		}
		seenPart[pt.name] = true
		if pt.declare == nil {
			continue
		}
		if err := pt.declare(a); err != nil {
			return nil, fmt.Errorf("%s: part %s: declare: %w", op, pt.name, err)
		}
	}
	table := append(builtins(), a.commands...)
	seen := map[string]bool{}
	for _, c := range table {
		if !commandNameRE.MatchString(c.name) || seen[c.name] {
			return nil, errs.E(op, errs.Internal, fmt.Errorf("command name %q is malformed or used twice", c.name))
		}
		if c.run == nil {
			return nil, errs.E(op, errs.Internal, fmt.Errorf("command %q has no run function", c.name))
		}
		seen[c.name] = true
	}
	return table, nil
}

func find(table []command, name string) (command, bool) {
	for _, c := range table {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// width is the width of the name column of the usage text.
func width(table []command) int {
	w := 0
	for _, c := range table {
		w = max(w, len(c.name))
	}
	return w
}

// helpLine is one command's line of the usage text.
func helpLine(c command, w int) string {
	line := fmt.Sprintf("  %-*s  %s", w, c.name, c.summary)
	if c.usage != "" {
		line += "  " + c.usage
	}
	return line
}

// usageTo writes the usage text to w.
func (p *process) usageTo(w io.Writer, table []command) {
	width := width(table)
	sayln(w, "usage: bms <command>")
	for _, c := range table {
		sayln(w, helpLine(c, width))
	}
	sayln(w, "Configuration comes from `BMS_*` environment variables; run `bms check` to see it.")
}

// runHelp prints the usage to stdout.
func runHelp(_ context.Context, p *process) int {
	table, err := p.commands()
	if err != nil {
		p.problem(err)
		return ExitFailure
	}
	p.usageTo(p.stdout, table)
	return ExitOK
}

// runVersion prints `bms <version> (<go version> <os>/<arch>)`.
func runVersion(_ context.Context, p *process) int {
	say(p.stdout, "bms %s (%s %s/%s)\n", p.version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return ExitOK
}
