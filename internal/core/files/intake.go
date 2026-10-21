package files

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files/internal/imgmeta"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// ctxReader fails a Read once its context has ended, so a stalled-but-trickling upload
// stops at its deadline.
type ctxReader struct {
	ctx context.Context //nolint:containedctx // one Stage call's reader, never kept beyond it
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err //nolint:wrapcheck // classified by classifyRead
	}
	return c.r.Read(p) //nolint:wrapcheck // classified by classifyRead
}

// writeError marks an error that came from writing the temp file, not from reading the
// body, so a full disk is Unavailable and a dropped client is not.
type writeError struct{ err error }

func (w writeError) Error() string { return w.err.Error() }
func (w writeError) Unwrap() error { return w.err }

// failWriter wraps the temp file so a write error is recognisable.
type failWriter struct{ w io.Writer }

func (f failWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err != nil {
		return n, writeError{err}
	}
	return n, nil
}

// classifyRead turns an error from reading the body or writing the spool into an errs kind.
func classifyRead(op string, err error) error {
	var wf writeError
	var tooBig *http.MaxBytesError
	var ee *errs.Error
	switch {
	case errors.As(err, &wf):
		return errs.E(op, errs.Unavailable, fmt.Errorf("write the temp file: %w", wf.err))
	case errors.As(err, &tooBig):
		return errs.E(op, errs.Invalid, fmt.Errorf("the request body is too large: %w", err))
	case errors.As(err, &ee):
		return err // an Invalid from ReadUpload (file order) or a ctx error already classified
	}
	if k, ok := ctxKind(err); ok {
		return errs.E(op, k, err)
	}
	return fieldErr(op, errs.Invalid, "file", codeMalformed, nil, "the upload could not be read to its end")
}

// Stage spools the upload to a temp file under the size limit, sniffs its type against
// Accept, strips image metadata, hashes it and puts it in the owner's bucket. It fails
// errs.Internal inside a transaction; errs.Invalid (fields name, file, accept; codes
// too_long, control_chars, required, too_large, type, malformed); errs.Conflict (quota)
// when the site is already at its quota; errs.Unavailable when the temp directory or the
// store fails; errs.Timeout and errs.Canceled. On any failure nothing is kept.
func (sc *scoped) Stage(ctx context.Context, up Upload) (st Staged, err error) {
	const op = "files.Store.Stage"
	s := sc.s
	ctx, span := s.tracer.Start(ctx, "files.stage", telemetry.WithAttrs(telemetry.Token("owner", sc.owner)))
	defer span.End()
	defer func() {
		add(s.metrics.uploads, ctx, 1, telemetry.Token("outcome", outcomeOf(err)))
		if err != nil {
			span.RecordError(err)
		}
	}()

	if _, inTx := db.TxMode(ctx); inTx {
		return Staged{}, errs.E(op, errs.Internal, errors.New("Stage called inside a transaction: a handler must not hold a connection through an upload"))
	}
	if err := up.Validate(); err != nil {
		return Staged{}, errs.E(op, errs.Invalid, err)
	}
	clean, err := cleanName(up.Name)
	if err != nil {
		return Staged{}, errs.E(op, errs.Invalid, err)
	}
	if up.Body == nil {
		return Staged{}, fieldErr(op, errs.Invalid, "file", codeRequired, nil, "must be set")
	}
	if err := ctx.Err(); err != nil {
		return Staged{}, errs.E(op, errs.KindOf(err), err)
	}
	pol, err := s.readPolicy(ctx, op)
	if err != nil {
		return Staged{}, err
	}
	if pol.QuotaBytes > 0 {
		_, used, uerr := s.store.Usage(ctx)
		if uerr != nil {
			return Staged{}, fmt.Errorf("%s: read the usage: %w", op, uerr)
		}
		if used >= pol.QuotaBytes {
			return Staged{}, fieldErr(op, errs.Conflict, "file", codeQuota, maxParam(pol.QuotaBytes), "the storage quota is used up")
		}
	}

	id := model.NewID[FileID]()
	spool := filepath.Join(s.tempDir, id.String())
	var final string
	defer func() {
		for _, p := range []string{spool, spool + ".strip"} {
			if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				s.logger.WarnContext(ctx, "a temp file could not be removed; the sweep will take it", slog.String("error.kind", errs.KindOf(rerr).String()))
			}
		}
	}()

	kind, err := sc.spool(ctx, op, spool, up, pol)
	if err != nil {
		return Staged{}, err
	}
	final = spool
	stripped := false
	if kind != PDF && !up.KeepMetadata {
		final = spool + ".strip"
		if err := stripFile(op, kind, spool, final); err != nil {
			return Staged{}, err
		}
		stripped = true
	}

	f, err := os.Open(final) //nolint:gosec // G304: a file this call created under the temp directory
	if err != nil {
		return Staged{}, errs.E(op, errs.Unavailable, fmt.Errorf("open the spooled file: %w", err))
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	h := sha256.New()
	size, err := io.Copy(h, ctxReader{ctx, f})
	if err != nil {
		return Staged{}, classifyFile(op, err)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Staged{}, errs.E(op, errs.Unavailable, fmt.Errorf("rewind the spooled file: %w", err))
	}
	if _, err := s.objects.Put(ctx, objects.PutInput{
		Bucket: sc.bucket, Key: objects.Key(id.String()), Size: size, SHA256: sum,
		ContentType: kind.ContentType(), Body: f,
	}); err != nil {
		if k, ok := ctxKind(err); ok {
			return Staged{}, errs.E(op, k, err)
		}
		if k := errs.KindOf(err); k == errs.Timeout || k == errs.Canceled {
			return Staged{}, errs.E(op, k, err)
		}
		return Staged{}, errs.E(op, errs.Unavailable, fmt.Errorf("put the object: %w", err))
	}
	return Staged{
		Bucket: sc.bucket, Key: objects.Key(id.String()), Name: storedName(clean, kind),
		ContentType: kind.ContentType(), Size: size, SHA256: sum, Stripped: stripped, StagedAt: s.now(),
	}, nil
}

// classifyFile classifies an error reading back the spooled file.
func classifyFile(op string, err error) error {
	if k, ok := ctxKind(err); ok {
		return errs.E(op, k, err)
	}
	return errs.E(op, errs.Unavailable, fmt.Errorf("read the spooled file: %w", err))
}

// spool streams the body to path, sniffing the first SniffBytes, and answers the kind.
func (sc *scoped) spool(ctx context.Context, op, path string, up Upload, pol Policy) (kind Kind, err error) {
	body := ctxReader{ctx, up.Body}
	head := make([]byte, SniffBytes)
	n, err := io.ReadFull(body, head)
	switch {
	case err == nil, errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
	default:
		return 0, classifyRead(op, err)
	}
	if n == 0 {
		return 0, fieldErr(op, errs.Invalid, "file", codeRequired, nil, "choose a file")
	}
	kind, ok := Detect(head[:n])
	if !ok || !up.Accept.Has(kind) {
		return 0, fieldErr(op, errs.Invalid, "file", codeType, nil, "this kind of file is not accepted")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a fresh name under the temp directory
	if err != nil {
		return 0, errs.E(op, errs.Unavailable, fmt.Errorf("create the temp file: %w", err))
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	w := failWriter{f}
	if _, err := w.Write(head[:n]); err != nil {
		return 0, classifyRead(op, err)
	}
	total := int64(n)
	if total > pol.MaxBytes {
		return 0, tooLarge(op, pol)
	}
	if n == SniffBytes {
		rest, err := io.Copy(w, io.LimitReader(body, pol.MaxBytes-total+1))
		total += rest
		if err != nil {
			return 0, classifyRead(op, err)
		}
		if total > pol.MaxBytes {
			return 0, tooLarge(op, pol)
		}
	}
	if err := f.Sync(); err != nil {
		return 0, errs.E(op, errs.Unavailable, fmt.Errorf("flush the temp file: %w", err))
	}
	return kind, nil
}

func tooLarge(op string, pol Policy) error {
	return fieldErr(op, errs.Invalid, "file", codeTooLarge, maxParam(pol.MaxBytes), "the file is larger than the limit")
}

// stripFile rewrites the spooled image without its metadata into dst.
func stripFile(op string, kind Kind, src, dst string) (err error) {
	in, err := os.Open(src) //nolint:gosec // G304: a file this call created under the temp directory
	if err != nil {
		return errs.E(op, errs.Unavailable, fmt.Errorf("open the spooled file: %w", err))
	}
	defer func() { err = errors.Join(err, in.Close()) }()
	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a fresh name under the temp directory
	if err != nil {
		return errs.E(op, errs.Unavailable, fmt.Errorf("create the temp file: %w", err))
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	var format imgmeta.Format
	switch kind {
	case PNG:
		format = imgmeta.PNG
	case JPEG:
		format = imgmeta.JPEG
	case GIF:
		format = imgmeta.GIF
	case WebP:
		format = imgmeta.WebP
	default:
		return errs.E(op, errs.Internal, fmt.Errorf("no stripper for kind %s", kind))
	}
	if serr := imgmeta.Strip(format, out, in); serr != nil {
		if errors.Is(serr, imgmeta.ErrMalformed) {
			return fieldErr(op, errs.Invalid, "file", codeMalformed, nil, "the file is damaged")
		}
		return errs.E(op, errs.Unavailable, fmt.Errorf("strip the image: %w", serr))
	}
	if serr := out.Sync(); serr != nil {
		return errs.E(op, errs.Unavailable, fmt.Errorf("flush the temp file: %w", serr))
	}
	return nil
}
