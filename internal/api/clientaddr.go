package api

import (
	"net/netip"
	"strings"
)

// trustedSet answers "is this address one of the front end's".
type trustedSet []netip.Prefix

func (t trustedSet) contains(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range t {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// deriveClient implements "Client address and scheme, step by step" (api.md). peer is the
// unmapped connection address; xff holds every X-Forwarded-For header line and xfp every
// X-Forwarded-Proto line. Nothing in it is logged.
func deriveClient(peer netip.Addr, trusted trustedSet, xff, xfp []string) (client netip.Addr, scheme string) {
	peer = peer.Unmap()
	if !trusted.contains(peer) {
		return peer, "http"
	}
	return walkForwarded(peer, trusted, xff), forwardedScheme(xfp)
}

// walkForwarded applies the rightmost-untrusted rule to the X-Forwarded-For lines.
func walkForwarded(peer netip.Addr, trusted trustedSet, xff []string) netip.Addr {
	var entries []string
	if len(xff) > 0 {
		for e := range strings.SplitSeq(strings.Join(xff, ","), ",") {
			entries = append(entries, strings.Trim(e, " "))
		}
	}
	right := peer // the hop to the right of the entry being looked at
	for i := len(entries) - 1; i >= 0; i-- {
		a, ok := parseForwarded(entries[i])
		if !ok {
			return right // malformed: use the hop to its right
		}
		if !trusted.contains(a) {
			return a
		}
		right = a
	}
	return peer // no header, or every entry trusted
}

func parseForwarded(s string) (netip.Addr, bool) {
	if s == "" {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		ap, perr := netip.ParseAddrPort(s)
		if perr != nil {
			return netip.Addr{}, false
		}
		a = ap.Addr()
	}
	return a.Unmap().WithZone(""), true
}

// forwardedScheme is "https" only when the last entry of the last X-Forwarded-Proto line
// is exactly https (any case), else "http".
func forwardedScheme(xfp []string) string {
	if len(xfp) == 0 {
		return "http"
	}
	last := xfp[len(xfp)-1]
	if i := strings.LastIndexByte(last, ','); i >= 0 {
		last = last[i+1:]
	}
	if strings.EqualFold(strings.Trim(last, " "), "https") {
		return "https"
	}
	return "http"
}
