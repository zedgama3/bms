package telemetry_test

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/model"
)

func jsonLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line is not JSON: %q: %v", sc.Text(), err)
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return out
}

func TestHandler_JSONShape(t *testing.T) {
	h := newHarness(t, nil)
	ctx, sp := h.tracer(t).Start(context.Background(), "shape")
	h.tel.Logger().InfoContext(ctx, "hello", slog.String("module", "hello"), slog.Int("n", 7))
	h.tel.Logger().WarnContext(ctx, "second")
	sp.End()
	id, _ := telemetry.TraceIDFrom(ctx)

	lines := jsonLines(t, h.buf.String())
	if len(lines) != 2 {
		t.Fatalf("want one line per record, got %d: %q", len(lines), h.buf.String())
	}
	l := lines[0]
	for _, k := range []string{"time", "level", "msg", "instance", "trace_id", "span_id", "module", "n"} {
		if _, ok := l[k]; !ok {
			t.Errorf("missing key %q in %v", k, l)
		}
	}
	if l["msg"] != "hello" || l["instance"] != "test-copy" || l["trace_id"] != id || l["level"] != "INFO" {
		t.Errorf("line = %v", l)
	}
	if s, ok := l["span_id"].(string); !ok || len(s) != 16 {
		t.Errorf("span_id = %v", l["span_id"])
	}
}

func TestHandler_TextFormat(t *testing.T) {
	h := newHarness(t, func(o *telemetry.Options) { o.Format = telemetry.FormatText })
	ctx, sp := h.tracer(t).Start(context.Background(), "text")
	h.tel.Logger().InfoContext(ctx, "hello", slog.String("module", "hello"))
	sp.End()
	id, _ := telemetry.TraceIDFrom(ctx)
	out := h.buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("text format wrote JSON: %q", out)
	}
	for _, want := range []string{"level=INFO", "msg=hello", "instance=test-copy", "trace_id=" + id, "module=hello"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestHandler_LevelFilter(t *testing.T) {
	h := newHarness(t, func(o *telemetry.Options) { o.Level = slog.LevelWarn })
	ctx := context.Background()
	h.tel.Logger().DebugContext(ctx, "dbg")
	h.tel.Logger().InfoContext(ctx, "inf")
	h.tel.Logger().WarnContext(ctx, "wrn")
	h.tel.Logger().ErrorContext(ctx, "err")
	out := h.buf.String()
	if strings.Contains(out, "dbg") || strings.Contains(out, "inf") || !strings.Contains(out, "wrn") || !strings.Contains(out, "err") {
		t.Errorf("out = %q", out)
	}
	if h.tel.Logger().Enabled(ctx, slog.LevelInfo) {
		t.Error("Enabled(info) at warn level")
	}
}

func TestHandler_AddSourceAtDebugOnly(t *testing.T) {
	for lvl, want := range map[slog.Level]bool{slog.LevelDebug: true, slog.LevelInfo: false} {
		h := newHarness(t, func(o *telemetry.Options) { o.Level = lvl })
		h.tel.Logger().InfoContext(context.Background(), "src")
		lines := jsonLines(t, h.buf.String())
		if len(lines) != 1 {
			t.Fatalf("level %v: %d lines", lvl, len(lines))
		}
		if _, got := lines[0]["source"]; got != want {
			t.Errorf("level %v: source present = %v, want %v", lvl, got, want)
		}
	}
}

func TestHandler_SecretAndEmailLogValueRedacted(t *testing.T) {
	h := newHarness(t, nil)
	email, err := model.ParseEmail(markerEmail)
	if err != nil {
		t.Fatalf("ParseEmail: %v", err)
	}
	h.tel.Logger().InfoContext(context.Background(), "values",
		slog.Any("credential", secret.New(marker)), slog.Any("contact", email))
	if strings.Contains(h.buf.String(), marker) || strings.Contains(h.buf.String(), "zq9@") {
		t.Errorf("stdout leaked: %q", h.buf.String())
	}
	for _, r := range h.obs.records() {
		for k, v := range r.Attrs {
			if strings.Contains(v, marker) || strings.Contains(v, "zq9@") {
				t.Errorf("record attr %s leaked: %q", k, v)
			}
		}
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("violations = %+v", h.obs.violations())
	}
}

func TestHandler_RecordWithoutContext_HasNoTraceID(t *testing.T) {
	h := newHarness(t, nil)
	h.tel.Logger().Info("no context") //nolint:sloglint // the test is about a call without a ctx
	lines := jsonLines(t, h.buf.String())
	if len(lines) != 1 {
		t.Fatalf("lines = %d", len(lines))
	}
	if _, ok := lines[0]["trace_id"]; ok {
		t.Errorf("trace_id present without a span: %v", lines[0])
	}
	if recs := h.obs.records(); len(recs) != 1 || recs[0].TraceID != "" {
		t.Errorf("records = %+v", recs)
	}
}

func TestHandler_LongValue_Truncated(t *testing.T) {
	h := newHarness(t, nil)
	long := repeat('v', 5000)
	h.tel.Logger().InfoContext(context.Background(), "long", slog.String("stack", long))
	recs := h.obs.records()
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	v := recs[0].Attrs["stack"]
	if len(v) > telemetry.MaxLogValueBytes || !strings.HasSuffix(v, "…") {
		t.Errorf("len=%d", len(v))
	}
	if !strings.HasPrefix(v, "vvvv") {
		t.Error("the top of the value must be kept")
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("truncation is not a violation: %+v", h.obs.violations())
	}
}

func TestHandler_ScrubsMessageAndValues(t *testing.T) {
	h := newHarness(t, nil)
	h.tel.Logger().InfoContext(context.Background(), "failed for "+markerEmail,
		slog.String("detail", "GET /p?x="+marker+" by "+markerEmail),
		slog.Group("grp", slog.String("inner", markerEmail)))
	out := h.buf.String()
	if strings.Contains(out, "zq9@") || strings.Contains(out, marker) {
		t.Errorf("stdout leaked: %q", out)
	}
	recs := h.obs.records()
	if len(recs) != 1 || !strings.Contains(recs[0].Message, "[email]") {
		t.Fatalf("records = %+v", recs)
	}
	if !strings.Contains(recs[0].Attrs["detail"], "?[query]") || !strings.Contains(recs[0].Attrs["grp.inner"], "[email]") {
		t.Errorf("attrs = %v", recs[0].Attrs)
	}
}

func TestHandler_DeniedKey_RedactsInGroupsAndWith(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	lg := h.tel.Logger()
	lg.InfoContext(ctx, "plain", slog.String("email", marker))
	lg.InfoContext(ctx, "grouped", slog.Group("user", slog.String("password", marker)))
	lg.With(slog.String("token", marker)).InfoContext(ctx, "with")
	lg.WithGroup("g").With(slog.String("phone", marker)).InfoContext(ctx, "withgroup")

	if strings.Contains(h.buf.String(), marker) {
		t.Errorf("stdout leaked: %q", h.buf.String())
	}
	recs := h.obs.records()
	byMsg := map[string]telemetry.LogRecord{}
	for _, r := range recs {
		byMsg[r.Message] = r
	}
	for _, m := range []string{"plain", "grouped", "with", "withgroup"} {
		if _, ok := byMsg[m]; !ok {
			t.Fatalf("record %q was dropped (the record must still go out)", m)
		}
	}
	if byMsg["plain"].Attrs["email"] != "[denied]" || byMsg["grouped"].Attrs["user.password"] != "[denied]" ||
		byMsg["with"].Attrs["token"] != "[denied]" || byMsg["withgroup"].Attrs["g.phone"] != "[denied]" {
		t.Errorf("attrs: %v", recs)
	}
	n := 0
	for _, v := range h.obs.violations() {
		if v.Signal == "log" && v.Rule == "denied_key" {
			n++
		}
	}
	if n < 4 {
		t.Errorf("want a log denied_key violation per leak, got %d: %+v", n, h.obs.violations())
	}
}

func TestHandler_StdoutWriteFails_CountsNoPanic(t *testing.T) {
	h := newHarness(t, func(o *telemetry.Options) { o.Stdout = failWriter{} })
	ctx := context.Background()
	h.tel.Logger().InfoContext(ctx, "into the void")

	if err := h.tel.Logger().Handler().Handle(ctx, slog.NewRecord(h.clk.Now(), slog.LevelInfo, "direct", 0)); err == nil {
		t.Error("Handle must return the stdout handler's error")
	}
	found := false
	for _, m := range h.obs.measurements() {
		if m.Instrument == "bms.telemetry.log_write_failures" && m.Value > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("log_write_failures not counted: %+v", h.obs.measurements())
	}
}

func TestHandler_Logger_CarriesInstance(t *testing.T) {
	h := newHarness(t, nil)
	h.tel.Logger().With(slog.String("module", "hello")).InfoContext(context.Background(), "scoped")
	recs := h.obs.records()
	if len(recs) != 1 || recs[0].Attrs["instance"] != "test-copy" || recs[0].Attrs["module"] != "hello" {
		t.Errorf("records = %+v", recs)
	}
}

func TestHandler_ClientStack_AllowedToIntakeCap(t *testing.T) {
	if telemetry.MaxStackLogBytes != 4096 {
		t.Fatalf("MaxStackLogBytes = %d", telemetry.MaxStackLogBytes)
	}
	h := newHarness(t, nil)
	exact := repeat('s', telemetry.MaxStackLogBytes)
	over := repeat('s', 5000)
	h.tel.Logger().InfoContext(context.Background(), "exact", slog.String("client.stack", exact))
	h.tel.Logger().InfoContext(context.Background(), "over", slog.String("client.stack", over))
	h.tel.Logger().InfoContext(context.Background(), "other", slog.String("client.message", exact))
	recs := h.obs.records()
	if len(recs) != 3 {
		t.Fatalf("records = %d", len(recs))
	}
	if got := recs[0].Attrs["client.stack"]; got != exact {
		t.Errorf("a 4096-byte client.stack was cut: len=%d", len(got))
	}
	v := recs[1].Attrs["client.stack"]
	if len(v) > telemetry.MaxStackLogBytes || len(v) < 4000 || !strings.HasSuffix(v, "…") {
		t.Errorf("over-cap client.stack: len=%d, want <= 4096 ending in an ellipsis", len(v))
	}
	o := recs[2].Attrs["client.message"]
	if len(o) > telemetry.MaxLogValueBytes || !strings.HasSuffix(o, "…") {
		t.Errorf("other strings keep MaxLogValueBytes: len=%d", len(o))
	}
	if len(h.obs.violations()) != 0 {
		t.Errorf("truncation is not a violation: %+v", h.obs.violations())
	}
}
