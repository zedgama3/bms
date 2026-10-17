package api

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"time"
)

// Constants of decision 9: one log line per peer prefix per hour, at most maxPrefixes
// tracked, and one aggregate line per minute for the rest.
const (
	prefixLogEvery    = time.Hour
	aggregateLogEvery = time.Minute
	maxPrefixes       = 1000
	maxUntrackedSet   = 10_000 // bounds the set of untracked prefixes counted in the aggregate
)

// misdirectLog rate-limits the warning for a request refused with 403 misdirected, so a
// scan can neither grow memory nor flood the log. It logs a /24 or /64, never a device.
type misdirectLog struct {
	log *slog.Logger

	mu            sync.Mutex
	logged        map[netip.Prefix]time.Time // prefix -> when its line was written
	untracked     map[netip.Prefix]struct{}  // prefixes refused since the last aggregate line
	untrackedReqs int                        // requests refused since the last aggregate line
	lastAggregate time.Time
	lastEvict     time.Time // when the table was last scanned for expired entries
}

func newMisdirectLog(log *slog.Logger) *misdirectLog {
	return &misdirectLog{
		log:       log,
		logged:    map[netip.Prefix]time.Time{},
		untracked: map[netip.Prefix]struct{}{},
	}
}

// peerPrefix is the /24 (IPv4) or /64 (IPv6) the peer sits in.
func peerPrefix(a netip.Addr) netip.Prefix {
	a = a.Unmap()
	bits := 64
	if a.Is4() {
		bits = 24
	}
	p, err := a.Prefix(bits)
	if err != nil { // unreachable: bits fits the address family
		return netip.PrefixFrom(a, a.BitLen())
	}
	return p
}

// note records a refused request from peer at now and writes the log line decision 9
// asks for, if one is due. The line is written after the lock is released.
func (m *misdirectLog) note(ctx context.Context, peer netip.Addr, now time.Time) {
	if emit := m.decide(peer, now); emit != nil {
		emit(ctx)
	}
}

// decide updates the state under the lock and returns the log call that is due, if any.
func (m *misdirectLog) decide(peer netip.Addr, now time.Time) func(context.Context) {
	prefix := peerPrefix(peer)

	m.mu.Lock()
	defer m.mu.Unlock()

	if last, ok := m.logged[prefix]; ok {
		if now.Sub(last) < prefixLogEvery {
			return nil
		}
		m.logged[prefix] = now
		return m.prefixLine(prefix)
	}
	if len(m.logged) >= maxPrefixes && (m.lastEvict.IsZero() || now.Sub(m.lastEvict) >= aggregateLogEvery) {
		m.lastEvict = now // a scan of the table at most once a minute, not once per refused request
		for p, at := range m.logged {
			if now.Sub(at) >= prefixLogEvery {
				delete(m.logged, p)
			}
		}
	}
	if len(m.logged) < maxPrefixes {
		m.logged[prefix] = now
		return m.prefixLine(prefix)
	}
	m.untrackedReqs++
	if len(m.untracked) < maxUntrackedSet {
		m.untracked[prefix] = struct{}{}
	}
	if !m.lastAggregate.IsZero() && now.Sub(m.lastAggregate) < aggregateLogEvery {
		return nil
	}
	requests, prefixes := m.untrackedReqs, len(m.untracked)
	m.lastAggregate = now
	m.untrackedReqs = 0
	clear(m.untracked)
	return func(ctx context.Context) {
		m.log.LogAttrs(ctx, slog.LevelWarn, "requests from untrusted peers were refused; not all are listed",
			slog.Int("requests", requests),
			slog.Int("prefixes", prefixes),
		)
	}
}

func (m *misdirectLog) prefixLine(prefix netip.Prefix) func(context.Context) {
	return func(ctx context.Context) {
		m.log.LogAttrs(ctx, slog.LevelWarn,
			"request refused: it reached this server without passing a trusted proxy; check http.trusted_proxies",
			slog.String("peer_prefix", prefix.String()),
		)
	}
}
