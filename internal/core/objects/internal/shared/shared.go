// Package shared holds what the objects implementations (disk, s3, objectstest) have in
// common: context errors, the "no such object" error, and the reader that enforces a Put's
// size and checksum. It is internal to internal/core/objects.
//
//nolint:wrapcheck // every error returned here is built by objects/internal/shared with its kind, or is the context's or the caller's own
package shared

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// CtxErr returns nil while ctx is live, else errs.Timeout (deadline) or errs.Canceled.
func CtxErr(ctx context.Context, op string) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	return errs.E(op, errs.KindOf(err), err)
}

// Missing is the error of a Get or Head of an absent object: errs.Unavailable wrapping
// objects.ErrMissing. It names no bucket or key.
func Missing(op string) error {
	return errs.E(op, errs.Unavailable, objects.ErrMissing)
}

// Unavailable wraps a store or I/O failure. When the cause is the context ending, the kind
// is Timeout or Canceled instead. The text of cause is kept (it may name a path or an
// endpoint, never a secret or a file's content).
func Unavailable(op string, cause error) error {
	if cause == nil {
		return nil
	}
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return errs.E(op, errs.KindOf(cause), cause)
	}
	return errs.E(op, errs.Unavailable, cause)
}

// SizeError is Put's Invalid "size": fewer or more bytes than declared.
func SizeError(op string) error {
	return fieldErr(op, "size", "mismatch", "the body is not exactly the declared size")
}

// ChecksumError is Put's Invalid "checksum".
func ChecksumError(op string) error {
	return fieldErr(op, "checksum", "mismatch", "the body does not match the declared SHA-256")
}

func fieldErr(op, field, code, reason string) error {
	v := validate.Errors{Op: op}
	v.Addf(field, code, "%s", reason)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Verifier reads at most the declared size from a Put's body, hashing as it goes. Read it to
// the end (an io.Copy, or the SDK's upload), then call Finish for the verdict.
type Verifier struct {
	ctx  context.Context //nolint:containedctx // one Put's reader, never kept beyond it
	src  io.Reader
	want int64
	sum  [32]byte
	h    hash.Hash
	n    int64
	err  error // the first non-EOF read error of src, or the ctx's
	eof  bool  // src ended before want bytes
}

// NewVerifier returns a Verifier over in.Body that expects in.Size bytes with in.SHA256.
func NewVerifier(ctx context.Context, in objects.PutInput) *Verifier {
	return &Verifier{ctx: ctx, src: in.Body, want: in.Size, sum: in.SHA256, h: sha256.New()}
}

// Read implements io.Reader. It never returns more than the declared size in total.
func (v *Verifier) Read(p []byte) (int, error) {
	if v.err != nil {
		return 0, v.err
	}
	if err := v.ctx.Err(); err != nil {
		v.err = err
		return 0, err
	}
	remaining := v.want - v.n
	if remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := v.src.Read(p)
	if n > 0 {
		v.h.Write(p[:n]) //nolint:errcheck,gosec // hash.Hash.Write never returns an error
		v.n += int64(n)
	}
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, io.EOF):
		if v.n < v.want {
			v.eof = true
			return n, io.ErrUnexpectedEOF
		}
		return n, io.EOF
	default:
		v.err = err
		return n, err
	}
}

// Short reports that the body ended before the declared size.
func (v *Verifier) Short() bool { return v.eof }

// Complete reports that the declared number of bytes was read.
func (v *Verifier) Complete() bool { return v.n == v.want }

// Failure returns the non-EOF error that stopped the reading (the body's, or the ctx's), or nil.
func (v *Verifier) Failure() error { return v.err }

// Finish is the verdict, to be called after the declared size was read: errs.Invalid "size"
// when the body was short or has more bytes, errs.Invalid "checksum" when the SHA-256
// differs, and the body's or the context's failure otherwise. nil means the bytes were
// exactly right.
func (v *Verifier) Finish(op string) error {
	switch {
	case v.err != nil:
		return Unavailable(op, fmt.Errorf("read the body: %w", v.err))
	case v.n < v.want:
		return SizeError(op)
	}
	var one [1]byte
	n, err := v.src.Read(one[:])
	switch {
	case n > 0:
		return SizeError(op)
	case err != nil && !errors.Is(err, io.EOF):
		return Unavailable(op, fmt.Errorf("read the body: %w", err))
	}
	var got [32]byte
	copy(got[:], v.h.Sum(nil))
	if got != v.sum {
		return ChecksumError(op)
	}
	return nil
}
