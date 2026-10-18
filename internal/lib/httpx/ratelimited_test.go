package httpx_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"
)

func TestErrRateLimited_IsSharedSentinel(t *testing.T) {
	if httpx.ErrRateLimited == nil {
		t.Fatal("ErrRateLimited is nil")
	}
	if errors.Is(httpx.ErrRateLimited, httpx.ErrUnsupportedMediaType) || errors.Is(httpx.ErrUnsupportedMediaType, httpx.ErrRateLimited) {
		t.Error("the sentinels must be distinct")
	}
	if httpx.CodeRateLimited != "rate_limited" {
		t.Errorf("CodeRateLimited = %q", httpx.CodeRateLimited)
	}
}

func TestErrRateLimited_FoundThroughWrappingAndKeepsUnavailable(t *testing.T) {
	wrapped := fmt.Errorf("handler: %w", errs.E("identity.Service.SignIn", errs.Unavailable, httpx.ErrRateLimited))
	if !errors.Is(wrapped, httpx.ErrRateLimited) {
		t.Error("errors.Is does not find the sentinel through wrapping")
	}
	if !errs.Is(wrapped, errs.Unavailable) {
		t.Errorf("kind = %v, want Unavailable", errs.KindOf(wrapped))
	}
	if errors.Is(errs.E("x", errs.Unavailable, errors.New("down")), httpx.ErrRateLimited) {
		t.Error("an ordinary Unavailable matches the sentinel")
	}
}
