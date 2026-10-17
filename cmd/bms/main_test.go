package main_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildBinary compiles cmd/bms into a temporary directory. It skips when go is not on PATH.
func buildBinary(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "bms")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-o", bin, ".") //nolint:gosec // runs the binary this test just built
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	t.Fatalf("the binary did not run: %v", err)
	return -1
}

// The binary is a one-liner over app.Main: it turns Main's status into the process status.
func TestBinary_ExitStatusIsMainsStatus(t *testing.T) {
	bin := buildBinary(t)
	for _, tc := range []struct {
		name string
		args []string
		want int
		out  string
	}{
		{"version", []string{"version"}, 0, "bms "},
		{"help", []string{"help"}, 0, "usage: bms <command>"},
		{"no arguments", nil, 0, "usage: bms <command>"},
		{"unknown command", []string{"frobnicate"}, 2, ""},
		{"config invalid", []string{"check"}, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, tc.args...) //nolint:gosec // runs the binary this test just built
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}  // no BMS_* at all
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if got := exitCode(t, err); got != tc.want {
				t.Fatalf("exit status = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, tc.want, &stdout, &stderr)
			}
			if !strings.Contains(stdout.String(), tc.out) {
				t.Errorf("stdout = %q, want it to contain %q", &stdout, tc.out)
			}
		})
	}
}
