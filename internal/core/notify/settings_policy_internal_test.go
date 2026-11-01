package notify

import (
	"context"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/settings/settingstest"
)

// Moved from app: TestRetentionSource_ReadsTheDuration. The 240h value reaches the cutoff
// computation as read; Purge only floors it at MinRetention.
func TestRetentionSource_ReadsTheDuration(t *testing.T) {
	r := settingstest.NewFake("notify", Settings)
	r.Set("retention", "240h")
	got, err := retentionFrom(context.Background(), r)
	if err != nil || got != 240*time.Hour {
		t.Errorf("retention = %v, %v, want 240h", got, err)
	}
	if max(got, MinRetention) != 240*time.Hour {
		t.Errorf("240h is below MinRetention %v: the cutoff would not use it", MinRetention)
	}
}
