// Package intake accepts error reports from the web shell (D20: POST /api/client-errors),
// limits them, validates and scrubs them, and logs each at warn with source=client. It is
// a service: api owns the route, the body cap and the status codes. It imports telemetry,
// model and lib.
package intake

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Limits and caps. Constants, not settings: a regional has no reason to tune them, and a
// looser value only makes a misbehaving page louder.
const (
	MaxReports          = 20                         // reports per batch
	MaxMessageBytes     = 512                        // after Scrub; longer is truncated
	MaxStackBytes       = telemetry.MaxStackLogBytes // after Scrub; longer keeps the top frames
	MaxUserAgentBytes   = 200
	MaxCount            = 1000 // a client's dedup counter
	PerAddressPerMinute = 30   // reports (not batches) per client address
	GlobalPerMinute     = 600  // reports per process
	MaxTrackedAddresses = 10_000
	RetryAfter          = time.Minute
	OccurredWithin      = 24 * time.Hour // |now - occurred_at| must be less
)

// rateLimitLogEvery is how often the "rate-limited" line may be written, process-wide.
const rateLimitLogEvery = time.Hour

// ErrRateLimited is wrapped (errs.Unavailable) by every rejection for rate; api maps it to
// 429 with Retry-After: 60. It is httpx.ErrRateLimited, the sentinel identity's limiters
// share, so errors.Is holds both ways. Check it with errors.Is before the kind, as with
// db.ErrReadOnly.
var ErrRateLimited = httpx.ErrRateLimited

// Options is what New needs.
type Options struct {
	Logger *slog.Logger    // records are written through it at warn with source=client
	Meter  telemetry.Meter // bms.client_errors.* counters
	Clock  clock.Clock     // the token buckets and OccurredWithin
}

// Submission is what api hands Accept: the batch plus what only api knows.
type Submission struct {
	From      netip.Addr     // the client address api derived; used for the limit, never logged
	Person    model.PersonID // zero when the request has no session
	UserAgent string         // the User-Agent header; truncated at MaxUserAgentBytes
	Batch     Batch
}

// bucket is a token bucket; tokens refill continuously at capacity per minute.
type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) refill(now time.Time, capacity int) {
	if now.After(b.last) {
		b.tokens += now.Sub(b.last).Minutes() * float64(capacity)
		if b.tokens > float64(capacity) {
			b.tokens = float64(capacity)
		}
		b.last = now
	}
}

type addrEntry struct {
	key netip.Addr
	bkt bucket
}

// Intake is the service. Safe for concurrent use.
type Intake struct {
	log      *slog.Logger
	clk      clock.Clock
	accepted telemetry.Counter
	rejected telemetry.Counter

	maxAddrs int // MaxTrackedAddresses; an internal test lowers it to reach the eviction path

	mu          sync.Mutex
	global      bucket
	addrs       map[netip.Addr]*list.Element // values are *addrEntry; front is the longest idle
	order       *list.List
	limitedSeen int       // reports refused since the last rate-limit line
	limitedLog  time.Time // when the last line was written; zero: never
}

// New fails with errs.Invalid if any option is nil (F35).
func New(opts Options) (*Intake, error) {
	const op = "intake.New"
	if opts.Logger == nil || opts.Meter == nil || opts.Clock == nil {
		return nil, errs.E(op, errs.Invalid, errors.New("nil dependency"))
	}
	accepted, err := opts.Meter.Counter("bms.client_errors.accepted", telemetry.WithUnit("{report}"))
	if err != nil {
		return nil, fmt.Errorf("%s: accepted counter: %w", op, err)
	}
	rejected, err := opts.Meter.Counter("bms.client_errors.rejected", telemetry.WithUnit("{report}"))
	if err != nil {
		return nil, fmt.Errorf("%s: rejected counter: %w", op, err)
	}
	now := opts.Clock.Now()
	return &Intake{
		log: opts.Logger, clk: opts.Clock, accepted: accepted, rejected: rejected,
		maxAddrs: MaxTrackedAddresses,
		global:   bucket{tokens: GlobalPerMinute, last: now},
		addrs:    map[netip.Addr]*list.Element{},
		order:    list.New(),
	}, nil
}

// Accept takes one batch: checks ctx (F47); charges the batch's report count against the
// global bucket then the address's bucket, rejecting the whole batch with errs.Unavailable
// wrapping ErrRateLimited when either is short (F45, F46); validates the batch (F36–F44);
// then, for each report, scrubs and truncates the text fields, replaces UUID-shaped
// segments in Screen and API.Route with "{id}", and writes one log record at warn. Returns
// nil once every record is written. It does no I/O other than logging.
func (i *Intake) Accept(ctx context.Context, s Submission) error {
	const op = "intake.Accept"
	if err := ctx.Err(); err != nil {
		return errs.E(op, errs.KindOf(err), err)
	}
	if !s.From.IsValid() {
		return errs.E(op, errs.Invalid, errors.New("client address is not valid"))
	}
	n := len(s.Batch.Reports)
	if err := validate.Range("reports", n, 1, MaxReports); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if err := i.charge(ctx, s.From, n); err != nil {
		return err
	}
	now := i.clk.Now()
	acc := validate.Errors{Op: op}
	acc.Add(s.Batch.Validate())
	for idx, r := range s.Batch.Reports {
		if d := now.Sub(r.OccurredAt); !r.OccurredAt.IsZero() && (d >= OccurredWithin || -d >= OccurredWithin) {
			acc.Add(fieldErrf("reports[%d].occurred_at", idx, "range",
				"must be within %s of now", OccurredWithin))
		}
	}
	if err := acc.Err(); err != nil {
		i.rejected.Add(ctx, int64(n), telemetry.Token("reason", "invalid"))
		return err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}

	agent := cut(telemetry.Scrub(s.UserAgent), MaxUserAgentBytes)
	for _, r := range s.Batch.Reports {
		count := r.Count
		if count == 0 {
			count = 1
		}
		attrs := []slog.Attr{
			slog.String("origin", "client"),
			slog.String("client.kind", string(r.Kind)),
			slog.String("client.message", cut(telemetry.Scrub(r.Message), MaxMessageBytes)),
		}
		if r.Stack != "" {
			attrs = append(attrs, slog.String("client.stack", cut(telemetry.Scrub(r.Stack), MaxStackBytes)))
		}
		attrs = append(attrs,
			slog.String("client.screen", replaceIDSegments(r.Screen)),
			slog.String("client.occurred_at", r.OccurredAt.UTC().Format(time.RFC3339)),
			slog.Int("client.count", count),
			slog.String("client.app_version", r.AppVersion),
			slog.String("client.error_ref", r.ErrorRef),
			slog.String("client.trace_id", r.TraceID),
		)
		if r.API != nil {
			attrs = append(attrs,
				slog.String("client.api.method", r.API.Method),
				slog.String("client.api.route", replaceIDSegments(r.API.Route)),
				slog.Int("client.api.status", r.API.Status),
				slog.String("client.api.code", r.API.Code),
			)
		}
		attrs = append(attrs, slog.String("user_agent", agent))
		if !s.Person.IsZero() {
			attrs = append(attrs, slog.String("person_id", s.Person.String()))
		}
		i.log.LogAttrs(ctx, slog.LevelWarn, "client error", attrs...)
		i.accepted.Add(ctx, int64(count), telemetry.Token("kind", string(r.Kind)))
	}
	return nil
}

func fieldErrf(fieldFormat string, idx int, code, reasonFormat string, args ...any) error {
	return errs.E("", errs.Invalid, &validate.FieldError{
		Field: fmt.Sprintf(fieldFormat, idx), Code: code, Reason: fmt.Sprintf(reasonFormat, args...),
	})
}

// charge takes n tokens from the global bucket and the address's bucket, or none (F45, F46).
func (i *Intake) charge(ctx context.Context, from netip.Addr, n int) error {
	const op = "intake.Accept"
	now := i.clk.Now()
	key := bucketKey(from)

	i.mu.Lock()
	i.sweep(now)
	i.global.refill(now, GlobalPerMinute)

	entry := i.entryFor(key, now)

	if i.global.tokens < float64(n) || entry.bkt.tokens < float64(n) {
		i.limitedSeen += n
		logNow := i.limitedLog.IsZero() || now.Sub(i.limitedLog) >= rateLimitLogEvery
		seen := i.limitedSeen
		if logNow {
			i.limitedLog, i.limitedSeen = now, 0
		}
		i.mu.Unlock()
		i.rejected.Add(ctx, int64(n), telemetry.Token("reason", "rate_limited"))
		if logNow {
			i.log.WarnContext(ctx, "client error reports rate-limited", slog.Int("reports", seen))
		}
		return errs.E(op, errs.Unavailable, ErrRateLimited)
	}
	i.global.tokens -= float64(n)
	entry.bkt.tokens -= float64(n)
	i.mu.Unlock()
	return nil
}

// entryFor returns the address's bucket, refilled, creating it (and evicting the bucket
// idle longest when the table is full) if needed. The caller holds i.mu.
func (i *Intake) entryFor(key netip.Addr, now time.Time) *addrEntry {
	if el, ok := i.addrs[key]; ok {
		if e, ok := el.Value.(*addrEntry); ok {
			i.order.MoveToBack(el)
			e.bkt.refill(now, PerAddressPerMinute)
			return e
		}
		i.order.Remove(el) // cannot happen: the list only holds *addrEntry
		delete(i.addrs, key)
	}
	if i.order.Len() >= i.maxAddrs {
		i.evictOldest()
	}
	e := &addrEntry{key: key, bkt: bucket{tokens: PerAddressPerMinute, last: now}}
	i.addrs[key] = i.order.PushBack(e)
	return e
}

// evictOldest removes the bucket idle longest. The caller holds i.mu.
func (i *Intake) evictOldest() {
	el := i.order.Front()
	if el == nil {
		return
	}
	if e, ok := el.Value.(*addrEntry); ok {
		delete(i.addrs, e.key)
	}
	i.order.Remove(el)
}

// sweep drops buckets idle for a minute: by then they are full again, so nothing is lost.
// The list is ordered by last use, so this stops at the first bucket still in use.
func (i *Intake) sweep(now time.Time) {
	for el := i.order.Front(); el != nil; el = i.order.Front() {
		if e, ok := el.Value.(*addrEntry); ok && now.Sub(e.bkt.last) < time.Minute {
			return
		}
		i.evictOldest()
	}
}

// bucketKey maps an address to its bucket: an IPv4-mapped address as IPv4, IPv6 by /64.
func bucketKey(a netip.Addr) netip.Addr {
	a = a.Unmap().WithZone("")
	if a.Is6() {
		return netip.PrefixFrom(a, 64).Masked().Addr()
	}
	return a
}

// cut returns s unchanged when it fits in limit bytes, otherwise a valid-UTF-8 prefix
// followed by "…" whose total length is at most limit. A stack keeps its top frames.
func cut(s string, limit int) string {
	const ellipsis = "…"
	if len(s) <= limit {
		return s
	}
	n := limit - len(ellipsis)
	if n < 0 {
		n = 0
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + ellipsis
}

// replaceIDSegments replaces every "/"-separated segment shaped like a UUID or 32 hex
// digits with "{id}": a concrete record ID must not become part of a route.
func replaceIDSegments(s string) string {
	if s == "" {
		return s
	}
	out := make([]byte, 0, len(s))
	start := 0
	for j := 0; j <= len(s); j++ {
		if j < len(s) && s[j] != '/' {
			continue
		}
		seg := s[start:j]
		if idShaped(seg) {
			out = append(out, "{id}"...)
		} else {
			out = append(out, seg...)
		}
		if j < len(s) {
			out = append(out, '/')
		}
		start = j + 1
	}
	return string(out)
}

func idShaped(seg string) bool {
	switch len(seg) {
	case 32:
		return allHex(seg)
	case 36:
		return seg[8] == '-' && seg[13] == '-' && seg[18] == '-' && seg[23] == '-' &&
			allHex(seg[:8]) && allHex(seg[9:13]) && allHex(seg[14:18]) && allHex(seg[19:23]) && allHex(seg[24:])
	default:
		return false
	}
}

func allHex(s string) bool {
	for j := 0; j < len(s); j++ {
		c := s[j]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
