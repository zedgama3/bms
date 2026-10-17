//go:build integration

package main_test

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/clock"
)

const marker = "ZQ9MARKER7"

func binaryEnv(t *testing.T) []string {
	t.Helper()
	pool := dbtest.TempDB(t)
	var name string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	u, err := url.Parse(os.Getenv("BMS_TEST_DB_DSN"))
	if err != nil {
		t.Fatal("BMS_TEST_DB_DSN is not a URL")
	}
	u.Path = "/" + name
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"BMS_DB_URL=" + u.String(),
		"BMS_SESSION_KEY=" + marker + strings.Repeat("k", 38),
		"BMS_DATA_DIR=" + filepath.Join(t.TempDir(), "data"),
		"BMS_HTTP_ADDR=" + addr,
		"BMS_LOG_FORMAT=json",
	}
}

func TestBinary_SIGTERM_StopsCleanlyExit0(t *testing.T) {
	bin := buildBinary(t)
	env := binaryEnv(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	mig := exec.CommandContext(ctx, bin, "migrate") //nolint:gosec // runs the binary this test just built
	mig.Env = env
	if out, err := mig.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, bytes.ReplaceAll(out, []byte(marker), []byte("[marker]")))
	}

	var stdout, stderr bytes.Buffer
	srv := exec.CommandContext(ctx, bin, "serve") //nolint:gosec // runs the binary this test just built
	srv.Env = env
	srv.Stdout, srv.Stderr = &stdout, &stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- srv.Wait() }()

	addr := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "BMS_HTTP_ADDR="); ok {
			addr = v
		}
	}
	ready := false
	for !ready {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/readyz", nil)
		if err != nil {
			t.Fatal(err)
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			ready = resp.StatusCode == http.StatusOK
			if cerr := resp.Body.Close(); cerr != nil {
				t.Fatal(cerr)
			}
		}
		if ready {
			break
		}
		select {
		case err := <-exited:
			t.Fatalf("serve exited before it was ready: %v\n%s\n%s", err, &stdout, &stderr)
		default:
		}
		if err := clock.Wait(ctx, clock.Real(), 50*time.Millisecond); err != nil {
			t.Fatalf("serve never became ready: %v", err)
		}
	}

	if err := srv.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case err := <-exited:
		if got := exitCode(t, err); got != 0 {
			t.Fatalf("exit status = %d, want 0\nstdout:\n%s\nstderr:\n%s", got, &stdout, &stderr)
		}
	case <-ctx.Done():
		t.Fatal("the server did not stop after SIGTERM")
	}
	if !strings.Contains(stdout.String(), `"stopped"`) {
		t.Errorf("stdout lacks the stopped line:\n%s", &stdout)
	}
	if strings.Contains(stdout.String()+stderr.String(), marker) {
		t.Error("output carries the secret marker")
	}
}
