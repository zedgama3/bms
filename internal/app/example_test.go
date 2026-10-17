package app_test

import (
	"context"
	"fmt"
	"os"

	"github.com/zedgama3/bms/internal/app"
)

// Main runs one command in process and returns its exit status; the caller decides what to
// do with it (cmd/bms passes it to os.Exit).
func ExampleMain_help() {
	code := app.Main(context.Background(), []string{"help"}, nil, os.Stdout, os.Stderr)
	fmt.Println("exit status:", code)
	// Output:
	// usage: bms <command>
	//   serve    run the server until SIGINT or SIGTERM
	//   migrate  create the secrets file on a new installation and apply migrations
	//   check    report configuration, modules, database and schema; exit as serve would
	//   openapi  print the OpenAPI document (needs no configuration)
	//   version  print the build version
	//   help     print this usage
	// Configuration comes from `BMS_*` environment variables; run `bms check` to see it.
	// exit status: 0
}

// An unknown command is the operator's mistake: usage on stderr and status 2.
func ExampleMain_unknownCommand() {
	code := app.Main(context.Background(), []string{"frobnicate"}, nil, os.Stdout, discard{})
	fmt.Println("exit status:", code)
	// Output:
	// exit status: 2
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
