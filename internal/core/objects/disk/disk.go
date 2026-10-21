// Package disk is the local-disk objects.Store, the default. Plain files, so rsync, restic,
// tar and ls all work on them: <root>/<bucket>/<aa>/<bb>/<key> holds the bytes and
// <key>.meta a small JSON document {size, sha256, content_type, modified}, where aa and bb
// are the first two bytes of SHA-256(key) in hex, so any key shape fans out.
//
// Put writes <key>.part, fsyncs, writes <key>.meta.part, fsyncs, renames the meta, then
// renames the data last and fsyncs the directory: a data file's existence means the object
// is complete. A key therefore may not end in ".meta" or ".part" (Invalid key), and a key
// cannot be both an object and the directory prefix of another (Conflict).
//
//nolint:wrapcheck // every error returned here is built by objects/internal/shared with its kind, or is the context's or the caller's own
package disk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/internal/shared"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

const (
	dirMode  = 0o700
	fileMode = 0o600

	suffixMeta     = ".meta"
	suffixPart     = ".part"
	suffixMetaPart = ".meta.part"
)

// Store keeps objects as files under one root. It holds no state and no open handle
// between calls; it is safe for concurrent use.
type Store struct {
	root string
}

var _ objects.Store = (*Store)(nil)

// New answers a store rooted at dir (<files.dir>/store). Fails errs.Invalid (field
// "files.dir") when dir is empty or relative. It does no I/O: the root is created by bms
// migrate and checked by Ping.
func New(dir string) (*Store, error) {
	const op = "objects.disk.New"
	v := validate.Errors{Op: op}
	switch {
	case strings.TrimSpace(dir) == "":
		v.Addf("files.dir", validate.CodeRequired, "must be set")
	case strings.ContainsRune(dir, 0):
		v.Addf("files.dir", validate.CodeMalformed, "must not contain a NUL byte")
	case !filepath.IsAbs(dir):
		v.Addf("files.dir", validate.CodeMalformed, "must be an absolute path")
	}
	if err := v.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return &Store{root: filepath.Clean(dir)}, nil
}

// Kind answers objects.Disk.
func (*Store) Kind() objects.Kind { return objects.Disk }

// meta is the .meta document.
type meta struct {
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	ContentType string    `json:"content_type,omitempty"`
	Modified    time.Time `json:"modified"`
}

// check validates a bucket and key and refuses the two suffixes the layout reserves.
func check(b objects.Bucket, k objects.Key) error {
	v := validate.Errors{Op: "objects.disk.check"}
	for _, err := range []error{b.Validate(), k.Validate()} {
		for _, fe := range validate.Fields(err) {
			v.Add(errs.E("", errs.Invalid, fe))
		}
	}
	if k.Validate() == nil && (strings.HasSuffix(string(k), suffixMeta) || strings.HasSuffix(string(k), suffixPart)) {
		v.Addf("key", validate.CodeMalformed, "must not end in %s or %s on a disk store", suffixMeta, suffixPart)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// dataPath is where the bytes of (b, k) live.
func (s *Store) dataPath(b objects.Bucket, k objects.Key) string {
	sum := sha256.Sum256([]byte(k))
	return filepath.Join(s.root, string(b), hex.EncodeToString(sum[:1]), hex.EncodeToString(sum[1:2]), filepath.FromSlash(string(k)))
}

// Put stores the object; see objects.Store.Put. It fails errs.Conflict when the object
// (or a part of a concurrent Put of the same key) exists, errs.Invalid for a bad bucket,
// key, size or checksum (nothing is kept), and errs.Unavailable for a disk failure.
func (s *Store) Put(ctx context.Context, in objects.PutInput) (info objects.Info, err error) {
	const op = "objects.disk.Put"
	if err := in.Validate(); err != nil {
		return objects.Info{}, errs.E(op, errs.Invalid, err)
	}
	if err := check(in.Bucket, in.Key); err != nil {
		return objects.Info{}, errs.E(op, errs.Invalid, err)
	}
	if in.Body == nil {
		return objects.Info{}, shared.SizeError(op)
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Info{}, err
	}
	data := s.dataPath(in.Bucket, in.Key)
	if err := os.MkdirAll(filepath.Dir(data), dirMode); err != nil {
		return objects.Info{}, mapPathErr(op, err)
	}
	part := data + suffixPart
	// The .part file is the lock: O_EXCL lets one writer of a key through.
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode) //nolint:gosec // G304: built from a validated bucket and key under the root
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return objects.Info{}, errs.E(op, errs.Conflict, errors.New("the object is being written"))
		}
		return objects.Info{}, mapPathErr(op, err)
	}
	closed, done := false, false
	defer func() {
		if done {
			return
		}
		// A failed Put keeps nothing: close the file and remove what it wrote.
		if !closed {
			err = errors.Join(err, f.Close())
		}
		for _, p := range []string{part, data + suffixMetaPart} {
			if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				err = errors.Join(err, rerr)
			}
		}
	}()
	if _, serr := os.Stat(data); serr == nil {
		return objects.Info{}, errs.E(op, errs.Conflict, errors.New("the object exists"))
	} else if !errors.Is(serr, fs.ErrNotExist) {
		return objects.Info{}, mapPathErr(op, serr)
	}
	vr := shared.NewVerifier(ctx, in)
	if _, cerr := io.Copy(f, vr); cerr != nil {
		switch {
		case vr.Short():
			return objects.Info{}, shared.SizeError(op)
		case vr.Failure() != nil:
			return objects.Info{}, shared.Unavailable(op, fmt.Errorf("read the body: %w", vr.Failure()))
		}
		return objects.Info{}, mapPathErr(op, cerr)
	}
	if verr := vr.Finish(op); verr != nil {
		return objects.Info{}, verr
	}
	if serr := f.Sync(); serr != nil {
		return objects.Info{}, mapPathErr(op, serr)
	}
	closed = true
	if cerr := f.Close(); cerr != nil {
		return objects.Info{}, mapPathErr(op, cerr)
	}
	now := time.Now().UTC()
	m := meta{Size: in.Size, SHA256: hex.EncodeToString(in.SHA256[:]), ContentType: in.ContentType, Modified: now}
	if werr := writeMeta(data+suffixMetaPart, m); werr != nil {
		return objects.Info{}, mapPathErr(op, werr)
	}
	if rerr := os.Rename(data+suffixMetaPart, data+suffixMeta); rerr != nil {
		return objects.Info{}, mapPathErr(op, rerr)
	}
	if rerr := os.Rename(part, data); rerr != nil {
		return objects.Info{}, mapPathErr(op, rerr)
	}
	done = true
	if serr := syncDir(filepath.Dir(data)); serr != nil {
		return objects.Info{}, mapPathErr(op, serr)
	}
	return objects.Info{
		Bucket: in.Bucket, Key: in.Key, Size: in.Size, SHA256: in.SHA256,
		ContentType: in.ContentType, Modified: now,
	}, nil
}

func writeMeta(path string, m meta) (err error) {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode) //nolint:gosec // G304: built from a validated bucket and key under the root
	if err != nil {
		return err //nolint:wrapcheck // mapPathErr classifies it
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := f.Write(b); err != nil {
		return err //nolint:wrapcheck // mapPathErr classifies it
	}
	return f.Sync() //nolint:wrapcheck // mapPathErr classifies it
}

// syncDir fsyncs a directory so a rename in it survives a crash.
func syncDir(dir string) (err error) {
	d, err := os.Open(dir) //nolint:gosec // G304: a directory this store created
	if err != nil {
		return err //nolint:wrapcheck // mapPathErr classifies it
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	if err := d.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err //nolint:wrapcheck // mapPathErr classifies it
	}
	return nil
}

// mapPathErr classifies a file-system error: a key that collides with a directory is
// Conflict, everything else Unavailable. The OS text is kept: it names a path under the
// store root, never a file's content.
func mapPathErr(op string, err error) error {
	if errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.EISDIR) {
		return errs.E(op, errs.Conflict, errors.New("the key collides with another object's path"))
	}
	return shared.Unavailable(op, err)
}

// readMeta reads the .meta of a data file; ok is false when it is absent or damaged.
func readMeta(dataPath string) (m meta, ok bool) {
	b, err := os.ReadFile(dataPath + suffixMeta) //nolint:gosec // G304: built from a validated bucket and key under the root
	if err != nil {
		return meta{}, false
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return meta{}, false
	}
	return m, true
}

// info builds the Info of an opened data file: the size and time from the file itself, the
// checksum and type from .meta when it is whole (zero otherwise: Verify then cannot compare).
func (s *Store) info(b objects.Bucket, k objects.Key, st fs.FileInfo) objects.Info {
	in := objects.Info{Bucket: b, Key: k, Size: st.Size(), Modified: st.ModTime().UTC()}
	if m, ok := readMeta(s.dataPath(b, k)); ok {
		in.ContentType = m.ContentType
		if raw, err := hex.DecodeString(m.SHA256); err == nil && len(raw) == len(in.SHA256) {
			copy(in.SHA256[:], raw)
		}
	}
	return in
}

// Get opens the object; a missing one is errs.Unavailable wrapping objects.ErrMissing. The
// Body is the open file: the caller closes it.
func (s *Store) Get(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Object, error) {
	const op = "objects.disk.Get"
	if err := check(b, k); err != nil {
		return objects.Object{}, errs.E(op, errs.Invalid, err)
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Object{}, err
	}
	f, err := os.Open(s.dataPath(b, k))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return objects.Object{}, shared.Missing(op)
		}
		return objects.Object{}, shared.Unavailable(op, err)
	}
	st, err := f.Stat()
	if err != nil {
		return objects.Object{}, errs.E(op, errs.Unavailable, errors.Join(err, f.Close()))
	}
	if st.IsDir() {
		return objects.Object{}, errs.E(op, errs.Unavailable, errors.Join(objects.ErrMissing, f.Close()))
	}
	return objects.Object{Info: s.info(b, k, st), Body: f}, nil
}

// Head answers the object's Info; missing as Get.
func (s *Store) Head(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Info, error) {
	const op = "objects.disk.Head"
	if err := check(b, k); err != nil {
		return objects.Info{}, errs.E(op, errs.Invalid, err)
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Info{}, err
	}
	st, err := os.Stat(s.dataPath(b, k))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return objects.Info{}, shared.Missing(op)
		}
		return objects.Info{}, shared.Unavailable(op, err)
	}
	if st.IsDir() {
		return objects.Info{}, shared.Missing(op)
	}
	return s.info(b, k, st), nil
}

// Delete removes the object (the data first, so a half-deleted object is never readable);
// a missing one is not an error.
func (s *Store) Delete(ctx context.Context, b objects.Bucket, k objects.Key) error {
	const op = "objects.disk.Delete"
	if err := check(b, k); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	data := s.dataPath(b, k)
	for _, p := range []string{data, data + suffixMeta} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return shared.Unavailable(op, err)
		}
	}
	return nil
}

// List walks the bucket's directory and calls fn for every complete object whose key starts
// with prefix. Modified is the data file's modification time. fn's error stops the walk and
// is returned unchanged. A bucket with no directory is empty.
func (s *Store) List(ctx context.Context, b objects.Bucket, prefix string, fn func(objects.Entry) error) error {
	const op = "objects.disk.List"
	if err := b.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if fn == nil {
		return errs.E(op, errs.Invalid, errors.New("nil callback"))
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	base := filepath.Join(s.root, string(b))
	var stop error
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, suffixMeta) || strings.HasSuffix(name, suffixPart) {
			return nil
		}
		rel, rerr := filepath.Rel(base, path)
		if rerr != nil {
			return rerr
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 3 {
			return nil
		}
		key := objects.Key(strings.Join(parts[2:], "/"))
		if !strings.HasPrefix(string(key), prefix) || key.Validate() != nil {
			return nil //nolint:nilerr // not an object of ours (or not under the prefix): skipped, not an error
		}
		st, serr := d.Info()
		if serr != nil {
			if errors.Is(serr, fs.ErrNotExist) {
				return nil //nolint:nilerr // deleted while walking: not an error
			}
			return serr
		}
		if ferr := fn(objects.Entry{Key: key, Size: st.Size(), Modified: st.ModTime().UTC()}); ferr != nil {
			stop = ferr
			return ferr
		}
		return nil
	})
	switch {
	case stop != nil:
		return stop
	case err != nil:
		if cerr := shared.CtxErr(ctx, op); cerr != nil {
			return cerr
		}
		return shared.Unavailable(op, err)
	}
	return nil
}

// Ping creates the root when it is missing, then writes and removes <root>/.bms-probe. It
// fails errs.Unavailable when the root cannot be created or written.
func (s *Store) Ping(ctx context.Context) error {
	const op = "objects.disk.Ping"
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return shared.Unavailable(op, err)
	}
	probe := filepath.Join(s.root, string(objects.ProbeKey))
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode) //nolint:gosec // G304: a fixed name under the root
	if err != nil {
		return shared.Unavailable(op, err)
	}
	_, werr := f.Write([]byte("bms-probe-0123456"))
	cerr := f.Close()
	rerr := os.Remove(probe)
	if err := errors.Join(werr, cerr, rerr); err != nil {
		return shared.Unavailable(op, err)
	}
	return nil
}
