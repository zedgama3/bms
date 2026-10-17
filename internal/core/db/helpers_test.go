package db_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// marker stands in for a password or a row value; no error, log or span may contain it.
const marker = "ZQ9MARKER7"

// validOptions returns Options that pass Validate, with the marker as the password.
func validOptions() db.Options {
	return db.Options{
		URL:            secret.New("postgres://bms:" + marker + "@localhost:5432/bms?sslmode=disable"),
		MaxConns:       10,
		ConnectTimeout: 30 * time.Second,
		QueryTimeout:   10 * time.Second,
	}
}

// closedPortURL returns a postgres URL whose port had a listener that is now closed, so a
// connect is refused at once.
func closedPortURL(t testing.TB) string {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return fmt.Sprintf("postgres://bms:%s@%s/bms?sslmode=disable", marker, addr)
}

// wantKind fails the test unless err carries kind.
func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want kind %v", kind)
	}
	if !errs.Is(err, kind) {
		t.Fatalf("got kind %v, want %v", errs.KindOf(err), kind)
	}
}

// noMarker fails the test if err's text carries the marker.
func noMarker(t testing.TB, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), marker) {
		t.Fatalf("error text leaks the marker")
	}
}
