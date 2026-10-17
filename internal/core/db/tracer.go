package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/zedgama3/bms/internal/core/telemetry"
)

// queryTracer is the pgx.QueryTracer that gives each statement a client span. It records
// the operation (the statement's first SQL keyword) and the SQLSTATE of a failure; never
// the statement text, its arguments, a row value or the connection URL (decision 4).
type queryTracer struct{ tracer telemetry.Tracer }

func newQueryTracer(t telemetry.Tracer) pgx.QueryTracer { return queryTracer{tracer: t} }

type spanKey struct{}

// TraceQueryStart opens a db.<OPERATION> span and stores it in the returned context.
func (q queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op := operationOf(data.SQL)
	ctx, span := q.tracer.Start(ctx, "db."+op,
		telemetry.WithKind(telemetry.SpanClient),
		telemetry.WithAttrs(
			telemetry.Token("db.operation", op),
			telemetry.Token("db.system", "postgresql"),
		))
	return context.WithValue(ctx, spanKey{}, span)
}

// TraceQueryEnd marks a failed statement and ends the span.
func (queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(spanKey{}).(telemetry.Span)
	if !ok {
		return
	}
	defer span.End()
	if data.Err == nil {
		return
	}
	span.RecordError(Wrap("db.statement", data.Err)) // scrubbed: names, never values
	var pg *pgconn.PgError
	if errors.As(data.Err, &pg) && pg.Code != "" {
		span.SetAttrs(telemetry.Token("db.response.status_code", pg.Code))
	}
}

// operationOf returns the statement's first SQL keyword in upper case, skipping leading
// white space and comments; OTHER when the text does not start with a letter.
func operationOf(sql string) string {
	i := 0
	for i < len(sql) {
		switch {
		case sql[i] == ' ' || sql[i] == '\t' || sql[i] == '\n' || sql[i] == '\r':
			i++
		case i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*':
			end := indexFrom(sql, "*/", i+2)
			if end < 0 {
				return "OTHER"
			}
			i = end + 2
		default:
			return keywordAt(sql, i)
		}
	}
	return "OTHER"
}

func indexFrom(s, sub string, from int) int {
	for i := from; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// keywordAt reads the run of letters at i, upper-cased.
func keywordAt(sql string, i int) string {
	const maxKeyword = 32
	j := i
	for j < len(sql) && (sql[j] >= 'a' && sql[j] <= 'z' || sql[j] >= 'A' && sql[j] <= 'Z') {
		j++
	}
	if j == i || j-i > maxKeyword {
		return "OTHER"
	}
	out := make([]byte, 0, j-i)
	for _, c := range []byte(sql[i:j]) {
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
