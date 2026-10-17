// Command bms is the Burn Management System: one binary, several commands (serve,
// migrate, check, openapi, version, help). It is a one-liner over app.Main.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/zedgama3/bms/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// A second signal terminates the process with the operating system's default
	// disposition: the operator's escape hatch when a closer hangs (app.md decision 1).
	go func() {
		<-ctx.Done()
		stop()
	}()
	os.Exit(app.Main(ctx, os.Args[1:], os.Environ(), os.Stdout, os.Stderr))
}
