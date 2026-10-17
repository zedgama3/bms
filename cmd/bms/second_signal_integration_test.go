//go:build integration

package main_test

import (
	"bytes"
	"context"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/clock"
)

// hangOut is a buffer one goroutine writes (exec's copier) and the test reads.
type hangOut struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (h *hangOut) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.b.Write(p) //nolint:wrapcheck // a bytes.Buffer never fails
}

func (h *hangOut) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.b.String()
}

// F27: a second SIGTERM after the first terminates a process stuck in shutdown, at once,
// with the operating system's default disposition (so a non-zero status). The binary is
// built with -tags e2ehang, which compiles in a module whose Stop blocks forever.
func TestBinary_SecondSignal_Terminates(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	bin := t.TempDir() + "/bms-hang"
	build := exec.CommandContext(ctx, goBin, "build", "-tags", "e2ehang", "-o", bin, ".") //nolint:gosec // builds this repository's own command
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build -tags e2ehang failed: %v\n%s", err, out)
	}

	env := append(binaryEnv(t), "BMS_HTTP_SHUTDOWN_TIMEOUT=60s")
	mig := exec.CommandContext(ctx, bin, "migrate") //nolint:gosec // runs the binary this test just built
	mig.Env = env
	if out, err := mig.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, bytes.ReplaceAll(out, []byte(marker), []byte("[marker]")))
	}

	var stdout, stderr hangOut
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
	for ready := false; !ready; {
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
		select {
		case err := <-exited:
			t.Fatalf("serve exited before it was ready: %v\n%s\n%s", err, stdout.String(), stderr.String())
		default:
		}
		if !ready {
			if err := clock.Wait(ctx, clock.Real(), 50*time.Millisecond); err != nil {
				t.Fatalf("serve never became ready: %v", err)
			}
		}
	}

	if err := srv.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("first signal: %v", err)
	}
	// The first signal starts an orderly shutdown that cannot finish: the module's Stop
	// never returns. Wait until the closers in front of the modules have reported, so the
	// process is known to be stuck in Stop, and show that it is still alive.
	for !strings.Contains(stdout.String(), `"name":"signals"`) {
		select {
		case err := <-exited:
			t.Fatalf("the process ended after the first signal although Stop blocks: %v\n%s", err, stdout.String())
		default:
		}
		if err := clock.Wait(ctx, clock.Real(), 20*time.Millisecond); err != nil {
			t.Fatalf("shutdown never reached the modules: %v\n%s", err, stdout.String())
		}
	}
	select {
	case err := <-exited:
		t.Fatalf("the process ended although Stop blocks: %v", err)
	default:
	}
	if err := srv.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("second signal: %v", err)
	}
	select {
	case err := <-exited:
		if got := exitCode(t, err); got == 0 {
			t.Errorf("exit status = 0 after a forced stop, want non-zero\n%s", stdout.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second signal did not terminate the process")
	}
}
