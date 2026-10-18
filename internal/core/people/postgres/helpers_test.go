//go:build integration

package postgres_test

import (
	"net/netip"
	"strconv"
	"strings"
)

func repeatByte(b byte, n int) string { return strings.Repeat(string(rune(b)), n) }

func repeatStr(s string, n int) string { return strings.Repeat(s, n) }

// mustAddr returns a documentation-range client address derived from seed.
func mustAddr(seed string) netip.Addr {
	n := 0
	for _, c := range seed {
		n += int(c)
	}
	return netip.MustParseAddr("10.9." + strconv.Itoa(n%250) + "." + strconv.Itoa(len(seed)))
}
