package people

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"slices"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

//go:embed templates/*.tmpl
var embeddedTemplates embed.FS

// Templates holds people's notification templates, deletion-requested (en, es), in the
// layout notify.Register reads: files <ident>.<lang>.tmpl at the root. app registers it
// under the owner "people". The template says only that a request is waiting: it names no
// person and carries no note (people.md decision 27).
var Templates fs.FS

func init() {
	sub, err := fs.Sub(embeddedTemplates, "templates")
	if err != nil {
		panic("people: embedded templates missing: " + err.Error())
	}
	Templates = sub
}

// The deletion-request notice (people.md, "Amendment 2026-10-19 (D39)").
const (
	// TemplateDeletionRequested is the notify template RequestDeletion sends.
	TemplateDeletionRequested notify.Template = "people.deletion-requested"

	// deletionRequestsLink is the page of requests, a path under the site URL.
	deletionRequestsLink = "/people/admin/requests"
)

// Holders is authz's seam for "who is appointed to this job": *authz.Service. It returns
// the persons who hold the permission site-wide now through an explicit grant (never the
// base role), superusers excluded.
type Holders interface {
	HoldersOf(ctx context.Context, p authz.Permission) ([]model.PersonID, error)
}

// notifyDeletionRequested tells the holders of people.data.delete that a request is
// waiting. Call it inside the Write transaction that filed the request, so a failure rolls
// the request back. A nil Notify or Holders, no holders, or e-mail not configured
// (Configured false, or a first Send answering Unsupported) sends nothing and succeeds with
// one info line carrying a count or a reason and never a person. Unsupported after an
// earlier batch was queued fails errs.Unavailable, so the request and the batch roll back.
// Any other Configured, HoldersOf or Send error is returned with its kind.
func (s *Service) notifyDeletionRequested(ctx context.Context, op string, request RequestID) error {
	if s.notify == nil || s.holders == nil {
		s.logger.InfoContext(ctx, "deletion request filed; no e-mail notice is wired", slog.String("reason", "not_wired"))
		return nil
	}
	// Ask once, before any recipient work, whether e-mail is on: a notice is all or nothing.
	configured, err := s.notify.Configured(ctx)
	if err != nil {
		return wrap(op, err)
	}
	if !configured {
		s.logger.InfoContext(ctx, "deletion request filed; e-mail is not configured, no notice sent",
			slog.String("reason", "unsupported"))
		return nil
	}
	holders, err := s.holders.HoldersOf(ctx, PermissionDataDelete)
	if err != nil {
		return wrap(op, err)
	}
	if len(holders) == 0 {
		s.logger.InfoContext(ctx, "deletion request filed; nobody holds the permission to decide it",
			slog.Int("holders", 0))
		return nil
	}
	// One Send takes at most notify.MaxRecipients people; the key is unique per recipient,
	// so a second batch with the same key never repeats one.
	// If e-mail is switched off after the first batch, the earlier batches are rolled back
	// with the request (Unavailable) rather than leaving only some holders told.
	sentAny := false
	for batch := range slices.Chunk(holders, notify.MaxRecipients) {
		err := s.notify.Send(ctx, notify.Message{
			Template: TemplateDeletionRequested,
			To:       slices.Clone(batch),
			Key:      "deletion-requested:" + request.String(),
			Link:     deletionRequestsLink,
		})
		switch {
		case err == nil:
			sentAny = true
		case errs.Is(err, errs.Unsupported) && sentAny:
			return errs.E(op, errs.Unavailable, errors.New("e-mail was switched off while the deletion notice was being queued"))
		case errs.Is(err, errs.Unsupported):
			s.logger.InfoContext(ctx, "deletion request filed; e-mail is not configured, no notice sent",
				slog.String("reason", "unsupported"))
			return nil
		default:
			return wrap(op, err)
		}
	}
	return nil
}
