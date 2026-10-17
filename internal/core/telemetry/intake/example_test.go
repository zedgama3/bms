package intake_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Accept logs one record per report, scrubbed, and never the client address.
func ExampleIntake_Accept() {
	var buf bytes.Buffer
	clk := clock.NewFake(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	tel, err := telemetry.New(context.Background(), telemetry.Options{
		Level: slog.LevelInfo, Format: telemetry.FormatJSON, Instance: "north-camp", Version: "dev",
		Stdout: &buf, Clock: clk, Environ: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		fmt.Println("telemetry:", err)
		return
	}
	defer func() {
		if err := tel.Close(context.Background()); err != nil {
			fmt.Println("close:", err)
		}
	}()
	meter, err := tel.Meter("intake")
	if err != nil {
		fmt.Println("meter:", err)
		return
	}
	in, err := intake.New(intake.Options{Logger: tel.Logger(), Meter: meter, Clock: clk})
	if err != nil {
		fmt.Println("intake:", err)
		return
	}

	err = in.Accept(context.Background(), intake.Submission{
		From:      netip.MustParseAddr("192.0.2.7"),
		UserAgent: "ExampleBrowser/1.0",
		Batch: intake.Batch{Reports: []intake.Report{{
			Kind:       intake.KindCrash,
			Message:    "cannot read property of undefined (sent by pat@example.org)",
			Screen:     "/people/{id}",
			OccurredAt: clk.Now(),
		}}},
	})
	fmt.Println("accept:", err)
	out := buf.String()
	fmt.Println("scrubbed:", !strings.Contains(out, "pat@example.org"))
	fmt.Println("address logged:", strings.Contains(out, "192.0.2.7"))
	// Output:
	// accept: <nil>
	// scrubbed: true
	// address logged: false
}

// A batch that does not fit the rate limit is refused whole; check ErrRateLimited before
// the kind to answer 429.
func ExampleErrRateLimited() {
	var buf bytes.Buffer
	clk := clock.NewFake(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	tel, err := telemetry.New(context.Background(), telemetry.Options{
		Level: slog.LevelInfo, Format: telemetry.FormatJSON, Instance: "north-camp", Version: "dev",
		Stdout: &buf, Clock: clk, Environ: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		fmt.Println("telemetry:", err)
		return
	}
	defer func() {
		if err := tel.Close(context.Background()); err != nil {
			fmt.Println("close:", err)
		}
	}()
	meter, err := tel.Meter("intake")
	if err != nil {
		fmt.Println("meter:", err)
		return
	}
	in, err := intake.New(intake.Options{Logger: tel.Logger(), Meter: meter, Clock: clk})
	if err != nil {
		fmt.Println("intake:", err)
		return
	}
	one := intake.Batch{Reports: []intake.Report{{Kind: intake.KindScreen, Message: "boom", OccurredAt: clk.Now()}}}
	var last error
	for range intake.PerAddressPerMinute + 1 {
		last = in.Accept(context.Background(), intake.Submission{From: netip.MustParseAddr("192.0.2.7"), Batch: one})
	}
	fmt.Println(errors.Is(last, intake.ErrRateLimited), errs.Is(last, errs.Unavailable))
	// Output:
	// true true
}
