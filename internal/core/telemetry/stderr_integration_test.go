//go:build integration && linux

package telemetry_test

import (
	"io"
	"os"
	"syscall"
	"testing"
)

// captureStderr redirects file descriptor 2 (where the SDK's global logger writes, fixed
// at its init) into a pipe until the returned stop function is called; stop returns what
// was written.
func captureStderr(t *testing.T) (stop func() string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved, err := syscall.Dup(2)
	if err != nil {
		t.Fatalf("dup: %v", err)
	}
	if err := syscall.Dup3(int(w.Fd()), 2, 0); err != nil {
		t.Fatalf("dup3: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		b, rerr := io.ReadAll(r)
		if rerr != nil {
			b = append(b, []byte("read error")...)
		}
		done <- string(b)
	}()
	stopped := false
	stop = func() string {
		if stopped {
			return ""
		}
		stopped = true
		if err := syscall.Dup3(saved, 2, 0); err != nil {
			t.Errorf("restore stderr: %v", err)
		}
		if err := syscall.Close(saved); err != nil {
			t.Errorf("close saved: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Errorf("close pipe: %v", err)
		}
		return <-done
	}
	t.Cleanup(func() { stop() })
	return stop
}
