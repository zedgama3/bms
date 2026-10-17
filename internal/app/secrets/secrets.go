// Package secrets writes the generated-secrets file that config reads (config.md
// decision 7). It imports the standard library, lib/errs and config (for the field and
// file-name constants) only. Consumer: app's `bms migrate`.
package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/zedgama3/bms/internal/config"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// FieldBytes is how much crypto/rand material one generated field holds before base64.
const FieldBytes = 64

// Modes of what Ensure creates.
const (
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// randReader is where the key material comes from. An internal test replaces it to prove
// that a failing source is reported (F26).
var randReader = io.Reader(rand.Reader)

// Ensure makes sure the JSON object at path holds a non-empty string under every name in
// fields, generating FieldBytes random bytes (standard base64) for each missing or empty
// one, and returns the names it generated. When the file does not exist it creates the
// parent directory (0700, every missing level) and the file, mode 0600, written to a
// temporary file and linked into place so that it appears complete and only if absent
// (the O_EXCL guarantee). When the file exists it reads it (at most config.MaxSecretFileBytes), adds
// the missing fields, preserves every other field, and replaces the file atomically (a
// temporary file in the same directory, 0600, fsync, rename, directory fsync). It never
// overwrites a non-empty value. Nothing created returns (nil, nil) and leaves the file
// untouched, byte for byte. ctx bounds the work (checked before each file operation).
//
// It fails with errs.Invalid when path is empty, fields is empty or holds a repeated or
// empty name, the directory or file cannot be created, opened, read or replaced, the
// existing file is not a regular file, or its content is not a JSON object whose known
// fields are strings (never echoing the content); with errs.Internal when crypto/rand
// fails; with errs.Canceled or errs.Timeout when ctx ends (F22, F23, F26).
func Ensure(ctx context.Context, path string, fields []string) (created []string, err error) {
	const op = "secrets.Ensure"
	if err := checkArgs(op, path, fields); err != nil {
		return nil, err
	}
	if err := ctxErr(ctx, op); err != nil {
		return nil, err
	}

	data, exists, err := readExisting(ctx, op, path)
	if err != nil {
		return nil, err
	}
	doc := map[string]json.RawMessage{}
	if exists {
		if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
			// The decoder's message can quote the content; say only that it is malformed.
			return nil, invalid(op, "%s is not a JSON object", path)
		}
	}
	for _, f := range fields {
		raw, ok := doc[f]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, invalid(op, "%s: field %q is not a string", path, f)
		}
		if s != "" {
			continue
		}
		delete(doc, f) // an empty value is generated again
	}

	for _, f := range fields {
		if _, ok := doc[f]; ok {
			continue
		}
		value, err := generate(op)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, errs.E(op, errs.Internal, fmt.Errorf("encode a generated field: %w", err))
		}
		doc[f] = raw
		created = append(created, f)
	}
	if len(created) == 0 {
		return nil, nil
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("encode %s: %w", filepath.Base(path), err))
	}
	out = append(out, '\n')

	if !exists {
		again, err := createNew(ctx, op, path, out)
		switch {
		case err != nil:
			return nil, err
		case !again:
			return created, nil
		}
		// Another process created the file between our read and our create: start over
		// from what it wrote, never overwriting it.
		return Ensure(ctx, path, fields)
	}
	if err := replace(ctx, op, path, out); err != nil {
		return nil, err
	}
	return created, nil
}

func checkArgs(op, path string, fields []string) error {
	if path == "" {
		return invalid(op, "the secrets file path is empty")
	}
	if len(fields) == 0 {
		return invalid(op, "no field names")
	}
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f == "" {
			return invalid(op, "a field name is empty")
		}
		if seen[f] {
			return invalid(op, "field %q is listed twice", f)
		}
		seen[f] = true
	}
	return nil
}

func invalid(op, format string, args ...any) error {
	return errs.E(op, errs.Invalid, fmt.Errorf(format, args...))
}

// ctxErr returns nil while ctx is live and otherwise Canceled or Timeout.
func ctxErr(ctx context.Context, op string) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	kind := errs.Canceled
	if errors.Is(err, context.DeadlineExceeded) {
		kind = errs.Timeout
	}
	return errs.E(op, kind, err)
}

// readExisting reads path when it exists. A missing file is (nil, false, nil).
func readExisting(ctx context.Context, op, path string) (data []byte, exists bool, err error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, false, nil
	case err != nil:
		return nil, false, invalid(op, "cannot inspect %s: %w", path, err)
	case !info.Mode().IsRegular():
		return nil, false, invalid(op, "%s is not a regular file", path)
	}
	if err := ctxErr(ctx, op); err != nil {
		return nil, false, err
	}
	f, err := os.Open(path) //nolint:gosec // the operator's configured path
	if err != nil {
		return nil, false, invalid(op, "cannot open %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, closeErr(op, path, f)) }()
	data, err = io.ReadAll(io.LimitReader(f, config.MaxSecretFileBytes+1))
	if err != nil {
		return nil, false, invalid(op, "cannot read %s: %w", path, err)
	}
	if len(data) > config.MaxSecretFileBytes {
		return nil, false, invalid(op, "%s is larger than %d bytes", path, config.MaxSecretFileBytes)
	}
	return data, true, nil
}

func closeErr(op, path string, f *os.File) error {
	if err := f.Close(); err != nil {
		return invalid(op, "cannot close %s: %w", path, err)
	}
	return nil
}

// generate returns FieldBytes of crypto/rand material, standard base64.
func generate(op string) (string, error) {
	buf := make([]byte, FieldBytes)
	if _, err := io.ReadFull(randReader, buf); err != nil {
		return "", errs.E(op, errs.Internal, fmt.Errorf("read random bytes: %w", err))
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// createNew creates the directory and the file, complete or not at all: the content is
// written to a temporary file in the same directory (0600, fsync) and linked into place
// with link(2), which fails if path exists (the O_EXCL guarantee) and never exposes a
// half-written file to a reader. exists is true when the file appeared in the meantime and
// nothing was written.
func createNew(ctx context.Context, op, path string, content []byte) (exists bool, err error) {
	dir := filepath.Dir(path)
	if err := ctxErr(ctx, op); err != nil {
		return false, err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return false, invalid(op, "cannot create the directory of %s: %w", path, err)
	}
	if err := ctxErr(ctx, op); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, ".secrets-*.tmp") // mode 0600
	if err != nil {
		return false, invalid(op, "cannot create a temporary file beside %s: %w", path, err)
	}
	name := tmp.Name()
	defer removeQuietly(name) // the link, when made, keeps the content
	if err := tmp.Chmod(fileMode); err != nil {
		return false, invalid(op, "cannot restrict the temporary file beside %s: %w", path, closeWith(err, tmp))
	}
	if err := writeSync(tmp, content); err != nil {
		return false, invalid(op, "cannot write the temporary file beside %s: %w", path, err)
	}
	if err := ctxErr(ctx, op); err != nil {
		return false, err
	}
	switch err := os.Link(name, path); {
	case errors.Is(err, os.ErrExist):
		return true, nil
	case err != nil:
		return false, invalid(op, "cannot create %s: %w", path, err)
	}
	if err := syncDir(dir); err != nil {
		return false, invalid(op, "cannot sync the directory of %s: %w", path, err)
	}
	return false, nil
}

// replace swaps path for content atomically: temporary file, fsync, rename, directory fsync.
func replace(ctx context.Context, op, path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := ctxErr(ctx, op); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".secrets-*.tmp") // mode 0600
	if err != nil {
		return invalid(op, "cannot create a temporary file beside %s: %w", path, err)
	}
	name := tmp.Name()
	if err := tmp.Chmod(fileMode); err != nil {
		removeQuietly(name)
		return invalid(op, "cannot restrict the temporary file beside %s: %w", path, closeWith(err, tmp))
	}
	if err := writeSync(tmp, content); err != nil {
		removeQuietly(name)
		return invalid(op, "cannot write the temporary file beside %s: %w", path, err)
	}
	if err := ctxErr(ctx, op); err != nil {
		removeQuietly(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		removeQuietly(name)
		return invalid(op, "cannot replace %s: %w", path, err)
	}
	if err := syncDir(dir); err != nil {
		return invalid(op, "cannot sync the directory of %s: %w", path, err)
	}
	return nil
}

// writeSync writes content, syncs and closes f, returning every failure.
func writeSync(f *os.File, content []byte) error {
	_, err := f.Write(content)
	if err == nil {
		err = f.Sync()
	}
	return closeWith(err, f)
}

// closeWith closes f and joins its failure to err.
func closeWith(err error, f *os.File) error {
	if cerr := f.Close(); cerr != nil && !errors.Is(cerr, os.ErrClosed) {
		return errors.Join(err, cerr)
	}
	return err
}

// removeQuietly deletes a file this package created and could not finish; the caller is
// already returning the failure that matters.
func removeQuietly(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return // nothing more can be done; the original failure is the one reported
	}
}

// syncDir fsyncs a directory so a rename or create survives a crash. Platforms that cannot
// open or sync a directory (Windows) are tolerated.
func syncDir(dir string) (err error) {
	d, err := os.Open(dir) //nolint:gosec // the directory of the operator's configured path
	if err != nil {
		return nil //nolint:nilerr // best effort where directories cannot be opened
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	if serr := d.Sync(); serr != nil && !errors.Is(serr, os.ErrInvalid) {
		return fmt.Errorf("sync directory: %w", serr)
	}
	return nil
}
