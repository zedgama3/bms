package identity

import (
	"net/netip"
	"sync"
	"time"
)

// Bucket names, as the bms.identity.rate_limited metric spells them.
const (
	bucketAddress = "address"
	bucketEmail   = "email"
)

// limiter slows down guessing: failures counted per client address and per submitted
// (canonical) address, each in a bounded map. In memory only, per process; the keys are
// personal data in flight and are never logged or persisted.
type limiter struct {
	mu      sync.Mutex
	maxKeys int
	addr    *failBucket
	email   *failBucket
}

func newLimiter() *limiter {
	return &limiter{maxKeys: MaxTrackedKeys, addr: newFailBucket(), email: newFailBucket()}
}

// failBucket maps a key to the times of its recent failures. The entries form a list from
// the key whose last failure is oldest (head) to the newest (tail), so a sweep stops at the
// first key that is still inside the window.
type failBucket struct {
	keys       map[string]*failEntry
	head, tail *failEntry
}

type failEntry struct {
	key        string
	at         []time.Time // ascending
	prev, next *failEntry
}

func newFailBucket() *failBucket { return &failBucket{keys: map[string]*failEntry{}} }

func (b *failBucket) unlink(e *failEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		b.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		b.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

func (b *failBucket) pushBack(e *failEntry) {
	e.prev, e.next = b.tail, nil
	if b.tail != nil {
		b.tail.next = e
	} else {
		b.head = e
	}
	b.tail = e
}

// sweep drops the keys whose newest failure has left the window.
func (b *failBucket) sweep(now time.Time, window time.Duration) {
	cutoff := now.Add(-window)
	for e := b.head; e != nil && !e.at[len(e.at)-1].After(cutoff); e = b.head {
		b.unlink(e)
		delete(b.keys, e.key)
	}
}

// count returns the failures of key inside the window.
func (b *failBucket) count(key string, now time.Time, window time.Duration) int {
	e, ok := b.keys[key]
	if !ok {
		return 0
	}
	cutoff := now.Add(-window)
	n := 0
	for _, t := range e.at {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

// add records a failure of key, keeping at most keep times, and evicts the oldest key
// beyond maxKeys.
func (b *failBucket) add(key string, now time.Time, window time.Duration, keep, maxKeys int) {
	cutoff := now.Add(-window)
	if e, ok := b.keys[key]; ok {
		kept := e.at[:0]
		for _, t := range e.at {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		kept = append(kept, now)
		if len(kept) > keep {
			kept = kept[len(kept)-keep:]
		}
		e.at = kept
		b.unlink(e)
		b.pushBack(e)
		return
	}
	e := &failEntry{key: key, at: []time.Time{now}}
	b.keys[key] = e
	b.pushBack(e)
	for len(b.keys) > maxKeys {
		oldest := b.head
		b.unlink(oldest)
		delete(b.keys, oldest.key)
	}
}

// remove deletes one failure of key made at the given time (the newest such), and the key
// when it was the last.
func (b *failBucket) remove(key string, at time.Time) {
	e, ok := b.keys[key]
	if !ok {
		return
	}
	for i := len(e.at) - 1; i >= 0; i-- {
		if e.at[i].Equal(at) {
			e.at = append(e.at[:i], e.at[i+1:]...)
			break
		}
	}
	if len(e.at) == 0 {
		b.unlink(e)
		delete(b.keys, key)
		return
	}
	b.reposition(e)
}

// newest is the time of the entry's last failure, the key of the list's order.
func (e *failEntry) newest() time.Time { return e.at[len(e.at)-1] }

// reposition moves e toward the head until the list is ordered by newest time again, after
// a removal made e's newest time earlier. Sweep and eviction rely on that order.
func (b *failBucket) reposition(e *failEntry) {
	p := e.prev
	for p != nil && p.newest().After(e.newest()) {
		p = p.prev
	}
	if p == e.prev {
		return
	}
	b.unlink(e)
	if p == nil {
		e.next = b.head
		if b.head != nil {
			b.head.prev = e
		} else {
			b.tail = e
		}
		b.head = e
		return
	}
	e.prev, e.next = p, p.next
	if p.next != nil {
		p.next.prev = e
	} else {
		b.tail = e
	}
	p.next = e
}

// addrKey is the bucket key of a client address; the zero Addr has none. An IPv6 client is
// keyed by its /64, since one subscriber holds the whole prefix.
func addrKey(a netip.Addr) (string, bool) {
	if !a.IsValid() {
		return "", false
	}
	a = a.Unmap()
	if a.Is6() {
		return netip.PrefixFrom(a, 64).Masked().String(), true
	}
	return a.String(), true
}

// reserve counts one attempt in both buckets, atomically with the check, so concurrent
// requests cannot all pass a check made before any of them is counted. It reports which
// bucket, if any, already holds limit attempts inside the window; then nothing is counted.
// The time is read from clock inside the lock and returned as at, so concurrent reservations
// append their times in order; a caller that learns the attempt was not a failure calls
// forgive with the same at. It sweeps first, so a fake clock drives the expiry.
func (l *limiter) reserve(a netip.Addr, email string, clock func() time.Time, limit int, window time.Duration) (at time.Time, bucket string, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := clock()
	l.addr.sweep(now, window)
	l.email.sweep(now, window)
	ak, haveAddr := addrKey(a)
	if haveAddr && l.addr.count(ak, now, window) >= limit {
		return now, bucketAddress, false
	}
	if email != "" && l.email.count(email, now, window) >= limit {
		return now, bucketEmail, false
	}
	if haveAddr {
		l.addr.add(ak, now, window, limit, l.maxKeys)
	}
	if email != "" {
		l.email.add(email, now, window, limit, l.maxKeys)
	}
	return now, "", true
}

// forgive takes back the attempt reserve counted at now.
func (l *limiter) forgive(a netip.Addr, email string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ak, ok := addrKey(a); ok {
		l.addr.remove(ak, now)
	}
	if email != "" {
		l.email.remove(email, now)
	}
}
