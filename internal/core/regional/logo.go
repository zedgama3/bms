package regional

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// MaxLogoBytes caps the logo's bytes: 2 MiB, a bound, not a setting (a header image is far
// below it; a photo above it is the wrong file). It is enforced before files' site-wide
// files.max_bytes, so the limit a settings admin sees is this one.
const MaxLogoBytes int64 = 2 << 20

// LogoRef is the one file record the logo is attached to: kind files.LogoRecordKind
// ("settings.logo", whose area is "settings", so files' ownership check passes for the Store
// scoped "settings": the owner of deployed logos does not change) and a fixed ID, the first 16
// bytes of sha256("settings.logo") (the fixed-ID pattern of departments-events decision 25).
// Callers must not modify it.
var LogoRef = audit.Ref{Kind: files.LogoRecordKind, ID: logoRefID()}

// logoRefID is the fixed ID of LogoRef.
func logoRefID() [16]byte {
	sum := sha256.Sum256([]byte(files.LogoRecordKind))
	return [16]byte(sum[:16])
}

// The audit actions regional records, under its own area. Entries recorded before the logo
// moved here keep the names settings.logo.changed and settings.logo.removed (the verb is the same).
const (
	actionLogoChanged audit.Action = "regional.logo.changed"
	actionLogoRemoved audit.Action = "regional.logo.removed"
)

// LogoResult is what PUT /logo and DELETE /logo answer.
type LogoResult struct {
	Revision settings.Revision `json:"revision"` // the settings revision after the call (a removal with no logo: the current one)
	Logo     string            `json:"logo"`     // the file ID after the call; "" after a removal
}

const (
	fieldFile   = "file"
	codeTooBig  = "too_large"
	logoField   = "logo"
	logoName    = "logo"           // the stored name of the logo, whatever the upload was called
	discardWait = 10 * time.Second // a Discard after the request's own context ended
)

// capReader fails with errs.Invalid (field "file", code "too_large", param max) on the
// first byte past max, and never asks the wrapped reader for more than one byte beyond max.
// files.Stage returns a classified error from the body unchanged (files.md, "Stage").
type capReader struct {
	op   string
	r    io.Reader
	max  int64
	read int64
	err  error
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	if room := c.max + 1 - c.read; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := c.r.Read(p)
	c.read += int64(n)
	if c.read > c.max {
		c.err = errs.E(c.op, errs.Invalid, &validate.FieldError{
			Field: fieldFile, Code: codeTooBig, Params: map[string]string{"max": strconv.FormatInt(c.max, 10)},
			Reason: "the file is larger than the limit",
		})
		return 0, c.err
	}
	return n, err //nolint:wrapcheck // the body's own error, classified by Stage
}

// logoFileID reads the id of the logo's file: the newest of LogoRef's files, "" when there is
// none. It may run inside a Read transaction (files: ByRecord does no audit and no store I/O).
func (s *Service) logoFileID(ctx context.Context) (string, error) {
	list, err := s.files.ByRecord(ctx, LogoRef)
	if err != nil {
		return "", err //nolint:wrapcheck // callers add their op
	}
	if len(list) == 0 {
		return "", nil
	}
	return list[len(list)-1].ID.String(), nil
}

// refuseReadOnly fails errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Service) refuseReadOnly(op string) error {
	if s.db.ReadOnly() {
		return errs.E(op, errs.Unavailable, db.ErrReadOnly)
	}
	return nil
}

// preflightSetLogo runs the checks that come before a byte of the upload is read, in order:
// settings.manage, a writable copy. The PUT handler runs it before it parses the multipart
// body, so a person without the permission is refused at once; SetLogo runs it again, because
// it is also called without the handler.
func (s *Service) preflightSetLogo(ctx context.Context, op string) error {
	if err := s.require(ctx, op, settings.PermissionManage); err != nil {
		return err
	}
	return s.refuseReadOnly(op)
}

// SetLogo replaces the regional's logo with body. Needs settings.manage. The file is stored
// under the name "logo" whatever name the upload carried (the argument is kept for the
// route's signature and ignored, so a person's file name is never stored). It fails
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy before a byte of body is read.
// Then, outside any transaction, it stages body (at most MaxLogoBytes; a files.Brand kind by
// its bytes); then, inside settings.Revise (one Write transaction under the revision lock, so
// two uploads on two instances do not interleave), it attaches the object to LogoRef replacing
// any earlier logo, records regional.logo.changed and bumps the revision, which the result
// carries. A staged object that was not attached is discarded. It also fails errs.Unauthenticated, errs.Permission, errs.Invalid (field "file":
// too_large {max}, type, malformed, required), errs.Conflict (quota), errs.Unavailable,
// errs.Timeout, errs.Canceled, Revise's kinds and the recorder's kind.
func (s *Service) SetLogo(ctx context.Context, _ string, body io.Reader) (_ LogoResult, err error) {
	const op = "regional.Service.SetLogo"
	ctx, span := s.start(ctx, "logo.set")
	defer endSpan(span, &err)
	if err := s.preflightSetLogo(ctx, op); err != nil {
		return LogoResult{}, err
	}
	if body == nil {
		return LogoResult{}, errs.E(op, errs.Invalid, &validate.FieldError{
			Field: fieldFile, Code: validate.CodeRequired, Reason: "choose a file",
		})
	}
	staged, err := s.files.Stage(ctx, files.Upload{
		Name: logoName, Accept: files.Brand, Body: &capReader{op: op, r: body, max: MaxLogoBytes},
	})
	if err != nil {
		return LogoResult{}, wrap(op, err)
	}
	defer s.discard(ctx, staged)

	var logo string
	rev, err := s.revision.Revise(ctx, func(ctx context.Context) (bool, error) {
		f, err := s.files.Attach(ctx, staged, files.Attachment{
			Record: LogoRef, Level: model.SensitivityPublic, Replace: true,
		})
		if err != nil {
			return false, err //nolint:wrapcheck // SetLogo adds its op once, outside Revise
		}
		span.SetAttrs(telemetry.ID("regional.file_id", f.ID))
		if err := s.audit.Record(ctx, logoEntry(actionLogoChanged, f.ID)); err != nil {
			return false, err //nolint:wrapcheck // SetLogo adds its op once, outside Revise
		}
		logo = f.ID.String()
		return true, nil
	})
	if err != nil {
		return LogoResult{}, wrap(op, err)
	}
	return LogoResult{Revision: rev, Logo: logo}, nil
}

// discard removes a staged object that was not attached (a no-op once attached). It runs
// on a context that outlives the request's, so a canceled upload still cleans up; a failure
// is logged once with the file's ID and left to files' sweep.
func (s *Service) discard(ctx context.Context, st files.Staged) {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), discardWait)
	defer cancel()
	if err := s.files.Discard(dctx, st); err != nil {
		s.logger.WarnContext(ctx, "a staged logo could not be discarded; the sweep will take it",
			slog.String("file_id", string(st.Key)), slog.String("error.kind", errs.KindOf(err).String()))
	}
}

// logoEntry is the audit entry of a logo change or removal; the reason names the file.
func logoEntry(action audit.Action, id files.FileID) audit.Entry {
	return audit.Entry{
		Action: action, Outcome: audit.Succeeded, Record: LogoRef,
		Fields: []string{logoField}, Level: model.SensitivityInternal, Reason: "file:" + id.String(),
	}
}

// RemoveLogo removes the regional's logo. Needs settings.manage. Inside settings.Revise it
// deletes LogoRef's files, records regional.logo.removed and bumps the revision; with no logo
// it changes nothing (no entry, no bump) and answers the current revision and an empty logo.
// It fails errs.Unauthenticated, errs.Permission, errs.Unavailable wrapping db.ErrReadOnly
// (checked first), Revise's kinds and the store's, files' and the recorder's kinds.
func (s *Service) RemoveLogo(ctx context.Context) (_ LogoResult, err error) {
	const op = "regional.Service.RemoveLogo"
	ctx, span := s.start(ctx, "logo.remove")
	defer endSpan(span, &err)
	if err := s.require(ctx, op, settings.PermissionManage); err != nil {
		return LogoResult{}, err
	}
	if err := s.refuseReadOnly(op); err != nil {
		return LogoResult{}, err
	}
	rev, err := s.revision.Revise(ctx, func(ctx context.Context) (bool, error) {
		old, err := s.logoFileID(ctx)
		if err != nil {
			return false, err //nolint:wrapcheck // RemoveLogo adds its op once, outside Revise
		}
		if old == "" { // nothing to remove: idempotent
			return false, nil
		}
		id, perr := model.ParseID[files.FileID](old)
		if perr != nil {
			return false, errs.E(op, errs.Internal, perr)
		}
		span.SetAttrs(telemetry.ID("regional.file_id", id))
		n, err := s.files.DeleteByRecord(ctx, LogoRef)
		if err != nil {
			return false, err //nolint:wrapcheck // RemoveLogo adds its op once, outside Revise
		}
		if n == 0 {
			return false, nil
		}
		if err := s.audit.Record(ctx, logoEntry(actionLogoRemoved, id)); err != nil {
			return false, err //nolint:wrapcheck // RemoveLogo adds its op once, outside Revise
		}
		return true, nil
	})
	if err != nil {
		return LogoResult{}, wrap(op, err)
	}
	return LogoResult{Revision: rev}, nil
}

// OpenLogo opens the logo for serving. It needs no permission: the logo is public. It lists
// LogoRef's files (the newest when more than one) and opens it; the caller serves it with
// files.Serve and closes the Content. It reads the files store, so a replica never serves a
// deleted file. It fails errs.NotFound (no logo), errs.Unavailable wrapping objects.ErrMissing
// (a copy whose bytes were not synced) and the store's kinds.
func (s *Service) OpenLogo(ctx context.Context) (_ files.File, _ files.Content, err error) {
	const op = "regional.Service.OpenLogo"
	ctx, span := s.start(ctx, "logo.open")
	defer endSpan(span, &err)
	list, err := s.files.ByRecord(ctx, LogoRef)
	if err != nil {
		return files.File{}, nil, wrap(op, err)
	}
	if len(list) == 0 {
		return files.File{}, nil, errs.E(op, errs.NotFound, errors.New("no logo is set"))
	}
	id := list[len(list)-1].ID
	span.SetAttrs(telemetry.ID("regional.file_id", id))
	f, c, err := s.files.Open(ctx, id)
	if err != nil {
		return files.File{}, nil, wrap(op, err)
	}
	return f, c, nil
}
