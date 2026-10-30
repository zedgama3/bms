package intake

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
)

func TestReplaceIDSegments_Table(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"/people/{id}", "/people/{id}"},
		{"/people/123e4567-e89b-12d3-a456-426614174000", "/people/{id}"},
		{"/a/123E4567E89B12D3A456426614174000/b/123e4567-e89b-12d3-a456-426614174000", "/a/{id}/b/{id}"},
		{"/people/123e4567-e89b-12d3-a456-42661417400", "/people/123e4567-e89b-12d3-a456-42661417400"},
		{"/people/123e4567-e89b-12d3-a456-42661417400z", "/people/123e4567-e89b-12d3-a456-42661417400z"},
		{"/trailing/", "/trailing/"},
	}
	for _, tc := range cases {
		if got := replaceIDSegments(tc.in); got != tc.want {
			t.Errorf("replaceIDSegments(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCut_FitsLimitAndKeepsValidUTF8(t *testing.T) {
	for _, s := range []string{strings.Repeat("a", 700), strings.Repeat("é", 400), strings.Repeat("€", 300)} {
		got := cut(s, MaxMessageBytes)
		if len(got) > MaxMessageBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") {
			t.Errorf("cut = %d bytes, valid=%v", len(got), utf8.ValidString(got))
		}
	}
	if got := cut("short", MaxMessageBytes); got != "short" {
		t.Errorf("short string changed: %q", got)
	}
}

func TestBucketKey_MappedAndZonedAddressesShareABucket(t *testing.T) {
	a := bucketKey(netip.MustParseAddr("::ffff:192.0.2.9"))
	b := bucketKey(netip.MustParseAddr("192.0.2.9"))
	if a != b {
		t.Errorf("mapped %v != plain %v", a, b)
	}
	c := bucketKey(netip.MustParseAddr("fe80::1%eth0"))
	d := bucketKey(netip.MustParseAddr("fe80::2"))
	if c != d {
		t.Errorf("same /64 differs: %v %v", c, d)
	}
}

func TestAccept_AddressTableFull_EvictsIdle(t *testing.T) {
	c := telemetrytest.New(t)
	clk := clock.NewFake(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	in, err := New(Options{Logger: c.Logger(), Meter: c.Meter("intake"), Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	in.maxAddrs = 3
	one := Batch{Reports: []Report{{Kind: KindScreen, Message: "boom", OccurredAt: clk.Now()}}}
	accept := func(addr string) {
		t.Helper()
		if err := in.Accept(context.Background(), Submission{From: netip.MustParseAddr(addr), Batch: one}); err != nil {
			t.Fatalf("Accept(%s): %v", addr, err)
		}
		if err := clk.Advance(time.Second); err != nil { // far less than the one-minute sweep
			t.Fatal(err)
		}
	}
	accept("192.0.2.1") // idle longest
	accept("192.0.2.2")
	accept("192.0.2.3")
	accept("192.0.2.2") // touched again: 192.0.2.1 stays the longest idle

	if got := len(in.addrs); got != 3 {
		t.Fatalf("table holds %d buckets, want 3", got)
	}
	accept("192.0.2.4") // table full: the longest-idle bucket makes room

	if got := len(in.addrs); got != 3 || in.order.Len() != 3 {
		t.Fatalf("table holds %d buckets (list %d), want 3", got, in.order.Len())
	}
	if _, ok := in.addrs[netip.MustParseAddr("192.0.2.1")]; ok {
		t.Error("the longest-idle address was not evicted")
	}
	for _, kept := range []string{"192.0.2.2", "192.0.2.3", "192.0.2.4"} {
		if _, ok := in.addrs[netip.MustParseAddr(kept)]; !ok {
			t.Errorf("%s should still be tracked", kept)
		}
	}
}

func TestUsageModuleOf_Table(t *testing.T) {
	c := telemetrytest.New(t)
	u, err := NewUsage(UsageOptions{
		Logger: c.Logger(), Meter: c.Meter("usage"), Clock: clock.NewFake(time.Now()), Enabled: true,
		Modules: map[string]string{"/login": "shell", "/volunteers/shifts": "volunteers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ screen, want string }{
		{"/login", "shell"}, // the manifest's set wins
		{"/volunteers/shifts", "volunteers"},
		{"/people/{id}", "people"}, // not in the map: the first segment
		{"/", "shell"},
		{"/{id}", "shell"}, // a first segment that is not a name
		{OtherToken, OtherToken},
	} {
		if got := u.moduleOf(tc.screen); got != tc.want {
			t.Errorf("moduleOf(%q) = %q, want %q", tc.screen, got, tc.want)
		}
	}
}

func TestFolder_AllowlistAndCap(t *testing.T) {
	var listed, capped folder
	if err := listed.setup("screens", []string{"/a", "/b"}, 1, EntryToken); err != nil {
		t.Fatal(err)
	}
	if err := capped.setup("screens", nil, 2); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		f          *folder
		in, want   string
		wantFolded bool
	}{
		{&listed, "/a", "/a", false},
		{&listed, EntryToken, EntryToken, false},
		{&listed, "/c", OtherToken, true},
		{&capped, "/x", "/x", false},
		{&capped, "/y", "/y", false},
		{&capped, "/x", "/x", false}, // admitted values keep passing
		{&capped, "/z", OtherToken, true},
	} {
		if got, folded := tc.f.fold(tc.in); got != tc.want || folded != tc.wantFolded {
			t.Errorf("fold(%q) = %q, %v; want %q, %v", tc.in, got, folded, tc.want, tc.wantFolded)
		}
	}
	if err := (&folder{}).setup("screens", []string{"has space"}, 1); err == nil {
		t.Error("a non-token entry was accepted")
	}
}
