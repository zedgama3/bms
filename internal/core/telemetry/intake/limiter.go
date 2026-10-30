package intake

import (
	"container/list"
	"net/netip"
	"sync"
	"time"
)

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

// limiter is the token-bucket machinery Intake and Usage share (telemetry T1.3: one
// implementation, two independent instances). It holds one global bucket and one bucket per
// client address (IPv6 by /64), the table bounded and swept by the clock; no goroutine.
type limiter struct {
	perAddr   int // tokens per address per minute, and its burst
	globalCap int // tokens per process per minute, and its burst
	maxAddrs  int // MaxTrackedAddresses; an internal test lowers it to reach the eviction path

	mu          sync.Mutex
	global      bucket
	addrs       map[netip.Addr]*list.Element // values are *addrEntry; front is the longest idle
	order       *list.List
	limitedSeen int       // items refused since the last rate-limit line
	limitedLog  time.Time // when the last line was written; zero: never
}

// setup prepares l in place (it holds a mutex, so it is never copied).
func (l *limiter) setup(perAddr, globalCap int, now time.Time) {
	l.perAddr, l.globalCap, l.maxAddrs = perAddr, globalCap, MaxTrackedAddresses
	l.global = bucket{tokens: float64(globalCap), last: now}
	l.addrs = map[netip.Addr]*list.Element{}
	l.order = list.New()
}

// take charges n items against the global bucket and the address's bucket, or neither. When
// it fails, logNow says whether the caller should write its hourly line and seen is the
// number of items refused since the previous line.
func (l *limiter) take(now time.Time, from netip.Addr, n int) (ok, logNow bool, seen int) {
	key := bucketKey(from)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	l.global.refill(now, l.globalCap)
	entry := l.entryFor(key, now)

	if l.global.tokens < float64(n) || entry.bkt.tokens < float64(n) {
		l.limitedSeen += n
		logNow = l.limitedLog.IsZero() || now.Sub(l.limitedLog) >= rateLimitLogEvery
		seen = l.limitedSeen
		if logNow {
			l.limitedLog, l.limitedSeen = now, 0
		}
		return false, logNow, seen
	}
	l.global.tokens -= float64(n)
	entry.bkt.tokens -= float64(n)
	return true, false, 0
}

// entryFor returns the address's bucket, refilled, creating it (and evicting the bucket
// idle longest when the table is full) if needed. The caller holds l.mu.
func (l *limiter) entryFor(key netip.Addr, now time.Time) *addrEntry {
	if el, ok := l.addrs[key]; ok {
		if e, ok := el.Value.(*addrEntry); ok {
			l.order.MoveToBack(el)
			e.bkt.refill(now, l.perAddr)
			return e
		}
		l.order.Remove(el) // cannot happen: the list only holds *addrEntry
		delete(l.addrs, key)
	}
	if l.order.Len() >= l.maxAddrs {
		l.evictOldest()
	}
	e := &addrEntry{key: key, bkt: bucket{tokens: float64(l.perAddr), last: now}}
	l.addrs[key] = l.order.PushBack(e)
	return e
}

// evictOldest removes the bucket idle longest. The caller holds l.mu.
func (l *limiter) evictOldest() {
	el := l.order.Front()
	if el == nil {
		return
	}
	if e, ok := el.Value.(*addrEntry); ok {
		delete(l.addrs, e.key)
	}
	l.order.Remove(el)
}

// sweep drops buckets idle for a minute: by then they are full again, so nothing is lost.
// The list is ordered by last use, so this stops at the first bucket still in use.
func (l *limiter) sweep(now time.Time) {
	for el := l.order.Front(); el != nil; el = l.order.Front() {
		if e, ok := el.Value.(*addrEntry); ok && now.Sub(e.bkt.last) < time.Minute {
			return
		}
		l.evictOldest()
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
