package demo

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/files"
)

var _ files.PolicySource = (*Service)(nil)

// Policy answers the inner policy lowered to the demo's caps: MaxBytes at most MaxUploadBytes,
// QuotaBytes at most MaxQuotaBytes (0, unlimited, becomes MaxQuotaBytes) and MaxPerRecord at
// most MaxPerRecord. It never raises a field. The caps are constants, not settings, so a
// demo settings-admin cannot lift them. Fails with the inner source's kind.
func (s *Service) Policy(ctx context.Context) (files.Policy, error) {
	p, err := s.o.Files.Policy(ctx)
	if err != nil {
		return files.Policy{}, fmt.Errorf("demo.Service.Policy: %w", err)
	}
	p.MaxBytes = min(p.MaxBytes, MaxUploadBytes)
	if p.QuotaBytes <= 0 {
		p.QuotaBytes = MaxQuotaBytes
	}
	p.QuotaBytes = min(p.QuotaBytes, MaxQuotaBytes)
	p.MaxPerRecord = min(p.MaxPerRecord, MaxPerRecord)
	return p, nil
}
