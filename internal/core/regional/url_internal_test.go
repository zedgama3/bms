package regional

import (
	"errors"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// regional.public_url canonical form (settings.md, D40). Every host is invented.
func TestCanonicalPublicURL_Table(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty is unset", "", ""},
		{"plain https", "https://burn.example.org", "https://burn.example.org"},
		{"trailing slash", "https://burn.example.org/", "https://burn.example.org"},
		{"several trailing slashes", "https://burn.example.org///", "https://burn.example.org"},
		{"scheme and host lowercased", "HTTPS://Burn.Example.ORG", "https://burn.example.org"},
		{"port kept", "http://Burn.Example.org:8080/", "http://burn.example.org:8080"},
		{"sub-path kept with its case", "https://burn.example.org/Bms/", "https://burn.example.org/Bms"},
		{"sub-path double slash end", "https://burn.example.org/bms//", "https://burn.example.org/bms"},
		{"port 1", "https://burn.example.org:1", "https://burn.example.org:1"},
		{"port 65535", "https://burn.example.org:65535", "https://burn.example.org:65535"},
		{"ipv6 host", "http://[::1]:8080/", "http://[::1]:8080"},
		{"ipv6 zone stays escaped", "http://[fe80::1%25eth0]:8080/", "http://[fe80::1%25eth0]:8080"},
		{"escaped host stays escaped", "https://EX%C3%A9mple.org/", "https://ex%c3%a9mple.org"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalPublicURL(tc.in)
			if err != nil {
				t.Fatalf("canonicalPublicURL(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("canonicalPublicURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if again, err := canonicalPublicURL(got); err != nil || again != got {
				t.Errorf("not idempotent: %q -> %q (%v)", got, again, err)
			}
		})
	}
}

func TestCheckPublicURL_Refusals_CarryReason(t *testing.T) {
	tests := []struct {
		name, in, code, reason string
	}{
		{"no scheme", "burn.example.org", "url", "scheme"},
		{"ftp", "ftp://burn.example.org", "url", "scheme"},
		{"relative", "/burn", "url", "scheme"},
		{"no host", "https://", "url", "host"},
		{"port zero", "https://burn.example.org:0", "url", "host"},
		{"port too big", "https://burn.example.org:65536", "url", "host"},
		{"userinfo", "https://wren:pw@burn.example.org", "url", "userinfo"},
		{"user only", "https://wren@burn.example.org", "url", "userinfo"},
		{"query", "https://burn.example.org/?a=1", "url", "query"},
		{"empty query", "https://burn.example.org?", "url", "query"},
		{"fragment", "https://burn.example.org/#top", "url", "fragment"},
		{"space", "https://burn .example.org", "url", "chars"},
		{"control", "https://burn.example.org/\x01", "url", "chars"},
		{"zero-width space", "https://burn.example.org/\u200b", "url", "chars"},
		{"too long", "https://burn.example.org/" + strings.Repeat("a", settings.MaxURLBytes), "too_long", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkPublicURL(tc.in)
			if err == nil {
				t.Fatalf("checkPublicURL(%q) accepted", tc.in)
			}
			var fe *validate.FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("not a FieldError: %v", err)
			}
			if fe.Code != tc.code {
				t.Errorf("code = %q, want %q", fe.Code, tc.code)
			}
			if tc.reason != "" && fe.Params["reason"] != tc.reason {
				t.Errorf("reason param = %q, want %q", fe.Params["reason"], tc.reason)
			}
		})
	}
}
