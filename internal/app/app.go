// Package app is the composition root (plan/07-abstractions.md, layer rule 5): the only
// package, with cmd/bms, that imports config, the modules, the core implementations and
// the web embed. It builds the process from its configuration, runs one command, and
// stops cleanly. It may import anything in the repository.
package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Exit statuses. They are the operator's API (systemd, compose, scripts) and are pinned
// by a test (decision 3).
const (
	ExitOK      = 0 // the command did its work; `serve` stopped cleanly after a signal
	ExitFailure = 1 // a runtime failure: database unreachable, start failed, a closer timed out, interrupted during start-up
	ExitUsage   = 2 // the operator's input: unknown command, invalid configuration or OTEL_* variable, missing session key, unknown module in BMS_MODULES_DISABLED, a registry or routing problem the build has
	ExitSchema  = 3 // the database schema is behind or ahead of the binary, or migrate found a changed file or another run: run `bms migrate` or upgrade the binary
)

// Main runs the command named by args[0] (the program name already stripped) with the
// environment environ (os.Environ() form) and returns the exit status. stdout receives the
// command's output; stderr receives problems and, for every command but serve, the log
// lines (decision 4). ctx is the process's lifetime: cmd/bms derives it from SIGINT and
// SIGTERM; a test cancels it. When ctx ends, serve shuts down in order and Main returns
// ExitOK (or ExitFailure if a closer failed); every other command stops where it is and
// returns ExitFailure. It never panics for an operator's input and never calls os.Exit.
// Consumers: cmd/bms (once); app's own tests (every command, in process).
func Main(ctx context.Context, args, environ []string, stdout, stderr io.Writer) int {
	p := &process{
		args:     args,
		environ:  environ,
		lookup:   lookupIn(environ),
		stdout:   stdout,
		stderr:   stderr,
		clock:    clock.Real(),
		registry: module.Default(),
		parts:    coreParts,
	}
	return p.run(ctx)
}

// command is one entry of the command table. Core parts add theirs in declare (identity:
// create-superuser, rotate-secret; people: export, delete). Names are "[a-z][a-z0-9-]*".
type command struct {
	name    string
	summary string // one English line for `bms help` (operator text, like log lines)
	// usage is the argument synopsis, "" when the command takes none: "<person-id>".
	usage string
	// run does the work. p carries the writers, the environment, the registry, the parts
	// and the clock; it loads configuration itself when it needs it (decision 5). It
	// returns the exit status and never calls os.Exit.
	run func(ctx context.Context, p *process) int
}

// process is one invocation of Main: what every command starts from.
type process struct {
	args     []string
	environ  []string
	lookup   func(key string) (string, bool) // over environ; telemetry.Options.Environ
	stdout   io.Writer
	stderr   io.Writer
	clock    clock.Clock      // clock.Real() in Main; a fake in tests
	registry *module.Registry // module.Default() in Main; a fresh one in tests
	parts    []part           // coreParts in Main; a test's own in tests
	version  string           // Version()
}

// say writes formatted operator text to w. A failed write to the operator's terminal or
// pipe has nowhere left to be reported, so it is dropped here, once.
func say(w io.Writer, format string, args ...any) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return
	}
}

// sayln writes args and a newline to w, as say does.
func sayln(w io.Writer, args ...any) {
	if _, err := fmt.Fprintln(w, args...); err != nil {
		return
	}
}

// lookupIn returns a lookup over entries of the form NAME=value; later entries win.
func lookupIn(environ []string) func(string) (string, bool) {
	env := make(map[string]string, len(environ))
	for _, e := range environ {
		if name, value, ok := strings.Cut(e, "="); ok {
			env[name] = value
		}
	}
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

// run resolves the command and runs it (F1, F2, F28).
func (p *process) run(ctx context.Context) int {
	table, err := p.commands()
	if err != nil {
		p.problem(err)
		return ExitFailure
	}
	name := "help"
	rest := []string(nil)
	if len(p.args) > 0 {
		name, rest = p.args[0], p.args[1:]
	}
	if name == "-h" || name == "--help" {
		name = "help"
	}
	cmd, ok := find(table, name)
	if !ok {
		say(p.stderr, "unknown command %q\n", name)
		p.usageTo(p.stderr, table)
		return ExitUsage
	}
	if len(rest) == 1 && (rest[0] == "-h" || rest[0] == "--help") {
		sayln(p.stdout, helpLine(cmd, width(table)))
		return ExitOK
	}
	if want := len(strings.Fields(cmd.usage)); len(rest) != want {
		say(p.stderr, "bms %s: takes %s\n", cmd.name, argsText(cmd, want))
		sayln(p.stderr, helpLine(cmd, width(table)))
		return ExitUsage
	}
	if cmd.name != "help" {
		v, err := Version()
		if err != nil {
			p.problem(err)
			return ExitUsage
		}
		p.version = v
	}
	return cmd.run(ctx, p)
}

func argsText(cmd command, want int) string {
	if want == 0 {
		return "no arguments"
	}
	return "the arguments " + cmd.usage
}

// problem writes err to stderr: one "<field>: <reason>" line per validate.FieldError, else
// the message. It never prints more than the error carries, and every error that reaches
// it was built not to carry a secret (config reasons, db's scrubbed errors).
func (p *process) problem(err error) {
	writeProblem(p.stderr, err)
}

func writeProblem(w io.Writer, err error) {
	if fields := validate.Fields(err); len(fields) > 0 {
		for _, fe := range fields {
			sayln(w, fe.Error())
		}
		return
	}
	sayln(w, err.Error())
}

// loadConfig is config.Load under a deadline (checkTimeout): reading a few small files
// must not hang a command.
func (p *process) loadConfig(ctx context.Context) (config.Config, error) {
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	cfg, err := config.Load(cctx, p.environ)
	if err != nil {
		return config.Config{}, err //nolint:wrapcheck // config.Load names itself
	}
	return cfg, nil
}

// earlyFail reports a failure that happened before telemetry existed and returns the
// exit status: ExitFailure with "interrupted" when ctx ended (F14), else the problem
// lines and statusOf(err, conflictStatus).
func (p *process) earlyFail(ctx context.Context, err error, conflictStatus int) int {
	if errs.Is(err, errs.Canceled) && ctx.Err() != nil {
		sayln(p.stderr, interruptedText)
		return ExitFailure
	}
	p.problem(err)
	return statusOf(err, conflictStatus)
}

// interruptedText is what the operator reads when a signal ended the command (F14).
const interruptedText = "interrupted: rerun the command"

// statusOf maps err to an exit status: Invalid is the operator's input (ExitUsage), Conflict
// is conflictStatus (ExitSchema where the schema is the question, ExitUsage where the build
// is), everything else a runtime failure.
func statusOf(err error, conflictStatus int) int {
	switch {
	case errs.Is(err, errs.Invalid):
		return ExitUsage
	case errs.Is(err, errs.Conflict):
		return conflictStatus
	default:
		return ExitFailure
	}
}
