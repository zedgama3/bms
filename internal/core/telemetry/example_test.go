package telemetry_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func newExample() (*telemetry.Telemetry, *bytes.Buffer, error) {
	var buf bytes.Buffer
	tel, err := telemetry.New(context.Background(), telemetry.Options{
		Level:    slog.LevelInfo,
		Format:   telemetry.FormatJSON,
		Instance: "north-camp",
		Version:  "dev",
		Stdout:   &buf,
		Clock:    clock.NewFake(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)),
		Environ:  func(string) (string, bool) { return "", false },
	})
	if err != nil {
		return nil, nil, fmt.Errorf("example telemetry: %w", err)
	}
	return tel, &buf, nil
}

// New builds the logger, tracer and meter. With no OTEL_* variables nothing is exported,
// yet every log line written inside a span carries its trace ID.
func ExampleNew() {
	tel, buf, err := newExample()
	if err != nil {
		fmt.Println("new:", err)
		return
	}
	defer func() {
		if err := tel.Close(context.Background()); err != nil {
			fmt.Println("close:", err)
		}
	}()

	tracer, err := tel.Tracer("api")
	if err != nil {
		fmt.Println("tracer:", err)
		return
	}
	ctx, span := tracer.Start(context.Background(), "GET /api/people/{id}")
	defer span.End()
	tel.Logger().InfoContext(ctx, "loaded")

	fmt.Println("exporting:", tel.Exporting())
	fmt.Println("trace id on the line:", strings.Contains(buf.String(), `"trace_id"`))
	// Output:
	// exporting: false
	// trace id on the line: true
}

// A span's trace ID is the request ID; the reference a person sees is its first four bytes.
func ExampleRefFrom() {
	tel, _, err := newExample()
	if err != nil {
		fmt.Println("new:", err)
		return
	}
	defer func() {
		if err := tel.Close(context.Background()); err != nil {
			fmt.Println("close:", err)
		}
	}()
	tracer, err := tel.Tracer("api")
	if err != nil {
		fmt.Println("tracer:", err)
		return
	}
	h := http.Header{}
	h.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	ctx, span := tracer.Start(context.Background(), "GET /api/people", telemetry.WithParentFrom(h))
	defer span.End()

	ref, ok := telemetry.RefFrom(ctx)
	fmt.Println(ref, ok, ref.Validate() == nil)
	_, ok = telemetry.RefFrom(context.Background())
	fmt.Println(ok)
	// Output:
	// 0AF7-6519 true true
	// false
}

// A failed span records the kind and operation of an errs error, never its details beyond
// the scrubbed message.
func ExampleSpan_RecordError() {
	tel, _, err := newExample()
	if err != nil {
		fmt.Println("new:", err)
		return
	}
	defer func() {
		if err := tel.Close(context.Background()); err != nil {
			fmt.Println("close:", err)
		}
	}()
	tracer, err := tel.Tracer("module.hello")
	if err != nil {
		fmt.Println("tracer:", err)
		return
	}
	_, span := tracer.Start(context.Background(), "hello.Greet", telemetry.WithKind(telemetry.SpanInternal))
	span.SetAttrs(telemetry.Token("module", "hello"), telemetry.Int("greetings", 3))
	span.RecordError(errs.E("hello.Greet", errs.NotFound, fmt.Errorf("no such greeting")))
	span.End()
	fmt.Println("recorded")
	// Output:
	// recorded
}

func ExampleScrub() {
	fmt.Println(telemetry.Scrub("sent to pat@example.org from /people?name=Pat"))
	fmt.Println(telemetry.Scrub("GET https://host.example/path?token=abc failed"))
	// Output:
	// sent to [email] from /people?[query]
	// GET https://host.example/path?[query] failed
}

func ExampleText() {
	a := telemetry.Text("reason", "could not mail pat@example.org")
	fmt.Println(a.Key())
	// Output:
	// reason
}

func ExampleRef_Validate() {
	fmt.Println(telemetry.Ref("1A2B-3C4D").Validate() == nil)
	fmt.Println(errs.Is(telemetry.Ref("1a2b3c4d").Validate(), errs.Invalid))
	// Output:
	// true
	// true
}

func ExampleIsToken() {
	for _, s := range []string{"/people/{id}", "_OTHER", "pat@example.org", "a b", "/x?y=1"} {
		fmt.Println(s, telemetry.IsToken(s))
	}
	// Output:
	// /people/{id} true
	// _OTHER true
	// pat@example.org false
	// a b false
	// /x?y=1 false
}
