// Package s3 is the S3-compatible objects.Store: MinIO, Garage, SeaweedFS, Ceph RGW or AWS,
// through github.com/minio/minio-go/v7 (Apache-2.0). It is the only package that imports the
// SDK. One real bucket holds every logical objects.Bucket as a key prefix:
// <Config.Prefix><bucket>/<key>, so an operator creates one bucket, never one per module.
//
// The store keeps no state beyond the SDK clients. It retries nothing itself (the SDK's
// retries are off: files.md "Store unreachable"), and bounds each call by Config.Timeout:
// the connection, the TLS handshake and the wait for response headers on every call, and the
// whole call for Head, Delete, Ping and the listing's pages. A Put or Get stream is bounded
// by the caller's context.
//
//nolint:wrapcheck // every error returned here is built by objects/internal/shared with its kind, or is the context's or the caller's own
package s3

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/internal/shared"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Defaults and bounds of Config.
const (
	// DefaultRegion is what MinIO and Garage accept.
	DefaultRegion = "us-east-1"
	// DefaultTimeout is Config.Timeout when it is zero.
	DefaultTimeout = 30 * time.Second
	// MaxTimeout is the largest Config.Timeout.
	MaxTimeout = 10 * time.Minute

	// metaSHA256 is the user-metadata key under which Put records the SHA-256 (hex), so Head can
	// answer it from every S3-compatible store.
	metaSHA256 = "sha256"
)

// Config is how to reach the real bucket.
type Config struct {
	// Endpoint is host[:port]. The connection is https, except for a loopback host
	// (localhost, 127.0.0.0/8, ::1), which is http: a development store. An explicit
	// "http://" or "https://" prefix overrides that, for a plain-HTTP store on a trusted
	// private network.
	Endpoint string
	// Bucket is the one real bucket; the logical buckets are prefixes inside it.
	Bucket string
	// Region defaults to "us-east-1" (what MinIO and Garage accept).
	Region string
	// Prefix is "" or "a/b/": everything this store writes lives under it.
	Prefix string
	// PathStyle selects path-style requests (host/bucket/key); every self-hosted store uses
	// it. False asks the SDK to detect (virtual-host style for AWS).
	PathStyle bool
	// AccessKey and SecretKey are the credentials; never logged.
	AccessKey secret.Secret
	SecretKey secret.Secret
	// Timeout bounds each call (see the package comment); defaults to 30 s.
	Timeout time.Duration
}

var (
	bucketRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	prefixRE = regexp.MustCompile(`^([A-Za-z0-9._-]+/)*$`)
	hostRE   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)
)

// Validate fails errs.Invalid joining one FieldError per problem, on the fields
// "files.s3_endpoint", "files.s3_bucket", "files.s3_prefix", "files.s3_access_key",
// "files.s3_secret_key", "files.s3_region" and "files.s3_timeout". Messages never echo a
// value.
func (c Config) Validate() error {
	v := validate.Errors{Op: "objects.s3.Config.Validate"}
	if _, _, err := splitEndpoint(c.Endpoint); err != nil {
		v.Addf("files.s3_endpoint", errCode(c.Endpoint), "must be host or host:port (optionally with http:// or https://) and nothing else")
	}
	if c.Bucket == "" {
		v.Addf("files.s3_bucket", validate.CodeRequired, "must be set")
	} else if !bucketRE.MatchString(c.Bucket) {
		v.Addf("files.s3_bucket", validate.CodeMalformed, "must be a valid S3 bucket name")
	}
	if !prefixRE.MatchString(c.Prefix) || len(c.Prefix) > 200 {
		v.Addf("files.s3_prefix", validate.CodeMalformed, "must be empty or segments of letters, digits, dot, underscore and hyphen, each followed by a slash")
	}
	if c.Region != "" && (len(c.Region) > 64 || strings.ContainsFunc(c.Region, func(r rune) bool { return r <= ' ' || r > '~' })) {
		v.Addf("files.s3_region", validate.CodeMalformed, "must be a region name")
	}
	if !c.AccessKey.IsSet() {
		v.Addf("files.s3_access_key", validate.CodeRequired, "must be set")
	}
	if !c.SecretKey.IsSet() {
		v.Addf("files.s3_secret_key", validate.CodeRequired, "must be set")
	}
	if c.Timeout < 0 || c.Timeout > MaxTimeout {
		v.Addf("files.s3_timeout", validate.CodeRange, "must be between 0 (the default) and %s", MaxTimeout)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func errCode(endpoint string) string {
	if strings.TrimSpace(endpoint) == "" {
		return validate.CodeRequired
	}
	return validate.CodeMalformed
}

// splitEndpoint returns the host[:port] of an endpoint and whether the connection is secure.
func splitEndpoint(endpoint string) (hostport string, secure bool, err error) {
	e := strings.TrimSpace(endpoint)
	scheme := ""
	switch {
	case strings.HasPrefix(e, "https://"):
		scheme, e = "https", strings.TrimPrefix(e, "https://")
	case strings.HasPrefix(e, "http://"):
		scheme, e = "http", strings.TrimPrefix(e, "http://")
	}
	if e == "" || strings.ContainsAny(e, "/?#@ \t\r\n") {
		return "", false, errors.New("malformed endpoint")
	}
	host := e
	if h, port, serr := net.SplitHostPort(e); serr == nil {
		if port == "" {
			return "", false, errors.New("malformed endpoint")
		}
		host = h
	} else if strings.Contains(e, ":") && !strings.HasPrefix(e, "[") {
		return "", false, errors.New("malformed endpoint")
	}
	host = strings.Trim(host, "[]")
	if !hostRE.MatchString(host) && net.ParseIP(host) == nil {
		return "", false, errors.New("malformed endpoint")
	}
	switch scheme {
	case "https":
		secure = true
	case "http":
		secure = false
	default:
		secure = !isLoopback(host)
	}
	return e, secure, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Store is the S3 objects.Store. Safe for concurrent use.
type Store struct {
	cfg    Config
	client *minio.Client
	anon   *minio.Client // no credentials: the probe's anonymous read
}

var _ objects.Store = (*Store)(nil)

// New builds the SDK clients; it does no network I/O. Fails errs.Invalid from
// Config.Validate.
func New(c Config) (*Store, error) {
	const op = "objects.s3.New"
	if err := c.Validate(); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	if c.Region == "" {
		c.Region = DefaultRegion
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	host, secure, err := splitEndpoint(c.Endpoint)
	if err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	lookup := minio.BucketLookupAuto
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	newClient := func(creds *credentials.Credentials) (*minio.Client, error) {
		cl, err := minio.New(host, &minio.Options{
			Creds:        creds,
			Secure:       secure,
			Region:       c.Region,
			BucketLookup: lookup,
			Transport:    transport(c.Timeout),
			MaxRetries:   1, // files retries nothing in-line
		})
		if err != nil {
			return nil, errs.E(op, errs.Invalid, fmt.Errorf("build the S3 client: %w", err))
		}
		return cl, nil
	}
	client, err := newClient(credentials.NewStaticV4(c.AccessKey.Value(), c.SecretKey.Value(), ""))
	if err != nil {
		return nil, err
	}
	anon, err := newClient(credentials.NewStaticV4("", "", ""))
	if err != nil {
		return nil, err
	}
	return &Store{cfg: c, client: client, anon: anon}, nil
}

// transport bounds connecting and the wait for response headers; the body of a long
// upload or download is bounded by the caller's context.
func transport(timeout time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
	}
}

// Kind answers objects.S3.
func (*Store) Kind() objects.Kind { return objects.S3 }

// name is the real object name of (b, k).
func (s *Store) name(b objects.Bucket, k objects.Key) string {
	return s.cfg.Prefix + string(b) + "/" + string(k)
}

func (s *Store) probeName() string { return s.cfg.Prefix + string(objects.ProbeKey) }

func (s *Store) call(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.cfg.Timeout)
}

// mapErr classifies an SDK error: the context ending, a missing key, a precondition
// failure (the key exists), and everything else as Unavailable. The SDK's text is kept: it
// names the endpoint and the server's reason, never a credential or a file's content.
// ctx is the caller's context, never the per-call one.
func mapErr(op string, ctx context.Context, err error) error { //nolint:revive // ctx second keeps op first, as errs.E does
	if err == nil {
		return nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return errs.E(op, errs.KindOf(cerr), cerr)
	}
	// A deadline the store's own per-call Timeout set, while the caller's context is alive, is
	// an unreachable store (Unavailable), not the caller's Timeout.
	resp := minio.ToErrorResponse(err)
	switch {
	case resp.Code == minio.NoSuchKey || (resp.StatusCode == http.StatusNotFound && resp.Code != minio.NoSuchBucket):
		return shared.Missing(op)
	case resp.StatusCode == http.StatusPreconditionFailed:
		return errs.E(op, errs.Conflict, errors.New("the object exists"))
	}
	return errs.E(op, errs.Unavailable, err)
}

func check(op string, b objects.Bucket, k objects.Key) error {
	if err := b.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if err := k.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	return nil
}

// Put uploads the object in one request, hashing the body as the SDK reads it. The SDK sends
// a whole-object checksum header only to servers that accept trailing headers, which not every
// S3-compatible store does, so the check is the client's: when the upload is done and the
// SHA-256 or the size is wrong the object is deleted and Put fails Invalid. The SHA-256 is
// recorded as user metadata, for Head. A key that exists is Conflict (If-None-Match: * where
// the store honours it, and a HEAD first where it does not).
func (s *Store) Put(ctx context.Context, in objects.PutInput) (objects.Info, error) {
	const op = "objects.s3.Put"
	if err := in.Validate(); err != nil {
		return objects.Info{}, errs.E(op, errs.Invalid, err)
	}
	if in.Body == nil {
		return objects.Info{}, shared.SizeError(op)
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Info{}, err
	}
	name := s.name(in.Bucket, in.Key)
	hctx, cancel := s.call(ctx)
	_, herr := s.client.StatObject(hctx, s.cfg.Bucket, name, minio.StatObjectOptions{})
	cancel()
	switch {
	case herr == nil:
		return objects.Info{}, errs.E(op, errs.Conflict, errors.New("the object exists"))
	case !errors.Is(mapErr(op, ctx, herr), objects.ErrMissing):
		return objects.Info{}, mapErr(op, ctx, herr)
	}

	vr := shared.NewVerifier(ctx, in)
	opts := minio.PutObjectOptions{
		ContentType:      in.ContentType,
		DisableMultipart: true, // one atomic request up to 5 GiB
		UserMetadata:     map[string]string{metaSHA256: hex.EncodeToString(in.SHA256[:])},
	}
	opts.SetMatchETagExcept("*")
	_, err := s.client.PutObject(ctx, s.cfg.Bucket, name, vr, in.Size, opts)
	if err != nil {
		// The body decides first: a short or wrong body is Invalid whatever the SDK said.
		switch {
		case vr.Short():
			return objects.Info{}, shared.SizeError(op)
		case vr.Failure() != nil:
			return objects.Info{}, shared.Unavailable(op, fmt.Errorf("read the body: %w", vr.Failure()))
		}
		if vr.Complete() {
			if verr := vr.Finish(op); verr != nil && errs.Is(verr, errs.Invalid) {
				return objects.Info{}, verr
			}
		}
		if resp := minio.ToErrorResponse(err); resp.Code == "BadDigest" || resp.Code == "XAmzContentSHA256Mismatch" || resp.Code == "InvalidDigest" {
			return objects.Info{}, shared.ChecksumError(op)
		}
		return objects.Info{}, mapErr(op, ctx, err)
	}
	if verr := vr.Finish(op); verr != nil {
		// A store that ignored the checksum header kept bytes that are wrong: remove them.
		dctx, dcancel := s.call(context.WithoutCancel(ctx))
		defer dcancel()
		if derr := s.client.RemoveObject(dctx, s.cfg.Bucket, name, minio.RemoveObjectOptions{}); derr != nil {
			return objects.Info{}, errors.Join(verr, mapErr(op, ctx, derr))
		}
		return objects.Info{}, verr
	}
	return objects.Info{
		Bucket: in.Bucket, Key: in.Key, Size: in.Size, SHA256: in.SHA256,
		ContentType: in.ContentType, Modified: time.Now().UTC(),
	}, nil
}

// Get opens the object. The body issues a ranged GET per seek and is bound to ctx.
func (s *Store) Get(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Object, error) {
	const op = "objects.s3.Get"
	if err := check(op, b, k); err != nil {
		return objects.Object{}, err
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Object{}, err
	}
	obj, err := s.client.GetObject(ctx, s.cfg.Bucket, s.name(b, k), minio.GetObjectOptions{})
	if err != nil {
		return objects.Object{}, mapErr(op, ctx, err)
	}
	st, err := obj.Stat()
	if err != nil {
		return objects.Object{}, errors.Join(mapErr(op, ctx, err), obj.Close())
	}
	return objects.Object{Info: infoOf(b, k, st), Body: obj}, nil
}

// Head reads the object's metadata and the SHA-256 Put recorded.
func (s *Store) Head(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Info, error) {
	const op = "objects.s3.Head"
	if err := check(op, b, k); err != nil {
		return objects.Info{}, err
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Info{}, err
	}
	cctx, cancel := s.call(ctx)
	defer cancel()
	st, err := s.client.StatObject(cctx, s.cfg.Bucket, s.name(b, k), minio.StatObjectOptions{})
	if err != nil {
		return objects.Info{}, mapErr(op, ctx, err)
	}
	return infoOf(b, k, st), nil
}

func infoOf(b objects.Bucket, k objects.Key, st minio.ObjectInfo) objects.Info {
	in := objects.Info{Bucket: b, Key: k, Size: st.Size, ContentType: st.ContentType, Modified: st.LastModified.UTC()}
	for k, v := range st.UserMetadata { // keys come back canonicalised ("Sha256")
		if strings.EqualFold(k, metaSHA256) {
			if raw, err := hex.DecodeString(v); err == nil && len(raw) == len(in.SHA256) {
				copy(in.SHA256[:], raw)
			}
		}
	}
	if in.SHA256 == ([32]byte{}) { // a store that records a checksum of its own
		if raw, err := base64.StdEncoding.DecodeString(st.ChecksumSHA256); err == nil && len(raw) == len(in.SHA256) {
			copy(in.SHA256[:], raw)
		}
	}
	return in
}

// Delete removes the object; the store's answer for a missing key is not an error.
func (s *Store) Delete(ctx context.Context, b objects.Bucket, k objects.Key) error {
	const op = "objects.s3.Delete"
	if err := check(op, b, k); err != nil {
		return err
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	cctx, cancel := s.call(ctx)
	defer cancel()
	if err := s.client.RemoveObject(cctx, s.cfg.Bucket, s.name(b, k), minio.RemoveObjectOptions{}); err != nil {
		if errors.Is(mapErr(op, ctx, err), objects.ErrMissing) {
			return nil
		}
		return mapErr(op, ctx, err)
	}
	return nil
}

// List walks every object of the logical bucket under prefix. Each page is bounded by the
// transport's header timeout; the walk by ctx.
func (s *Store) List(ctx context.Context, b objects.Bucket, prefix string, fn func(objects.Entry) error) error {
	const op = "objects.s3.List"
	if err := b.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if fn == nil {
		return errs.E(op, errs.Invalid, errors.New("nil callback"))
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	base := s.cfg.Prefix + string(b) + "/"
	for obj := range s.client.ListObjectsIter(lctx, s.cfg.Bucket, minio.ListObjectsOptions{Prefix: base + prefix, Recursive: true}) {
		if obj.Err != nil {
			return mapErr(op, ctx, obj.Err)
		}
		key := objects.Key(strings.TrimPrefix(obj.Key, base))
		if key.Validate() != nil {
			continue // not one of ours: a folder placeholder, or a key outside the grammar
		}
		if err := fn(objects.Entry{Key: key, Size: obj.Size, Modified: obj.LastModified.UTC()}); err != nil {
			return err
		}
	}
	return shared.CtxErr(ctx, op)
}

// Ping puts ProbeKey (16 random bytes) in the configured prefix, reads it back with a
// client that has no credentials, and deletes it. A 200 to the anonymous read is Invalid
// wrapping objects.ErrPublic; a 403 or 404 is the healthy answer; a failure of the put or
// of the anonymous request itself is Unavailable.
func (s *Store) Ping(ctx context.Context) (err error) {
	const op = "objects.s3.Ping"
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	cctx, cancel := s.call(ctx)
	defer cancel()
	var body [16]byte
	if _, rerr := rand.Read(body[:]); rerr != nil {
		return errs.E(op, errs.Internal, fmt.Errorf("random probe body: %w", rerr))
	}
	name := s.probeName()
	if _, perr := s.client.PutObject(cctx, s.cfg.Bucket, name, strings.NewReader(string(body[:])), int64(len(body)),
		minio.PutObjectOptions{ContentType: "application/octet-stream", DisableMultipart: true}); perr != nil {
		return mapPing(op, ctx, perr)
	}
	defer func() {
		// The probe is always removed, whatever the verdict.
		dctx, dcancel := s.call(context.WithoutCancel(ctx))
		defer dcancel()
		if derr := s.client.RemoveObject(dctx, s.cfg.Bucket, name, minio.RemoveObjectOptions{}); derr != nil {
			err = errors.Join(err, mapPing(op, ctx, derr))
		}
	}()
	return s.anonymousProbe(cctx, ctx, op, name)
}

// anonymousProbe reads name with no credentials.
func (s *Store) anonymousProbe(cctx, ctx context.Context, op, name string) error {
	obj, gerr := s.anon.GetObject(cctx, s.cfg.Bucket, name, minio.GetObjectOptions{})
	if gerr != nil {
		return mapPing(op, ctx, gerr)
	}
	_, serr := obj.Stat()
	cerr := obj.Close()
	switch {
	case serr == nil:
		// The read was served: every object in this bucket is exposed.
		return errs.E(op, errs.Invalid, errors.Join(objects.ErrPublic, cerr))
	case isDenied(serr):
		return cerr // 403 or 404: not readable anonymously, which is right
	default:
		return errors.Join(mapPing(op, ctx, serr), cerr)
	}
}

// isDenied reports the answers that mean "not readable anonymously".
func isDenied(err error) bool {
	resp := minio.ToErrorResponse(err)
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusNotFound, http.StatusUnauthorized:
		return true
	}
	return false
}

// mapPing maps an SDK failure during Ping: never Missing (a probe is never "missing" in the
// contract's sense), so a 404 on the bucket itself reads as Unavailable.
func mapPing(op string, ctx context.Context, err error) error { //nolint:revive // ctx second keeps op first
	mapped := mapErr(op, ctx, err)
	if errors.Is(mapped, objects.ErrMissing) {
		return errs.E(op, errs.Unavailable, fmt.Errorf("the bucket or probe is not reachable: %w", err))
	}
	return mapped
}

var _ io.ReadSeekCloser = (*minio.Object)(nil)
