# Files

- **Status:** Draft (2026-10-20)
- **Package:** internal/core/files (service, catalogue, intake, serving), internal/core/files/postgres (rows, chunk store, migrations), internal/core/files/disk, internal/core/files/s3 (blob stores), internal/core/files/filesettings (the setting declarations), internal/core/files/filestest, internal/core/files/blobtest   **UI:** none of its own (the shell pieces are proposed to `web-shell`)
- **Depends on specs:** `model`, `lib-errs`, `lib-core`, `lib-secret`, `config` (through `app`), `db`, `audit` (the `Recorder`, `Ref`), `authz` (`ActorFunc` only), `settings` (`Decl`, a scoped `Reader` through `app`), `telemetry`, `api` (`httpx`, the per-route body cap and timeout proposed below), `module-contract` (`Deps.Files`), `people` (a `Source` for export and deletion), `app` (part 10), `jobs` (Draft, in parallel: the orphan sweep)   **Used by:** `settings` (the regional logo), `volunteers` (certificate copies for qualifications, 09 §5), later `incidents` (photos and documents on an incident), `afterreports` (photos), any module that attaches a file to a record

## Summary

**Purpose.** Keep the bytes a module attaches to one of its records, with the record's permissions deciding who may read them. Package `internal/core/files`; imports `model`, `lib`, `core/db`, `core/audit`, `core/authz` (`ActorFunc`), `core/telemetry`. Never `core/identity`, `core/settings`, `core/module`, `api`, `app`, `config` or a module. Files owns no routes, screens or permissions: the owning module (or core part) serves a file from its own route after its own permission check; files stores, audits, strips, hashes and writes the safe response.

**Exported (`files`).**
- `FileID` (= `model.ID[...]`); `Kind` (`PNG`, `JPEG`, `GIF`, `WebP`, `PDF`; `String`, `ContentType`, `Extension`, `Inline`); `Accept []Kind` with `Images`, `Documents` (PDF), `Brand` (PNG, JPEG, WebP); `StoreKind` (`StorePostgres`, `StoreDisk`, `StoreS3`; `ParseStoreKind`).
- `File{ID, Record audit.Ref, Subject, UploadedBy model.PersonID, Level model.Sensitivity, Name, ContentType string, Size int64, SHA256 [32]byte, Stripped bool, CreatedAt}` (+`Validate`, `LogValue` = ID only). `Upload{Record audit.Ref, Subject model.PersonID, Level model.Sensitivity, Name string, Accept Accept, KeepMetadata, Replace bool, Body io.Reader}` (+`Validate`). `Content` (= `io.ReadSeekCloser`, bound to `Open`'s ctx). `Multipart{Fields map[string]string, Name, DeclaredType string, Body io.Reader}`. `Policy{MaxBytes, QuotaBytes int64, MaxPerRecord int}` (+`Validate`), `DefaultPolicy()`; `PolicySource{Policy(ctx) (Policy, error)}`. `Deletion{Deleted, Anonymized int}`; `Swept{Blobs, TempFiles int}`.
- `Store` (= `module.Deps.Files`, scoped to one owner by `(*Service).Scoped(owner)`): `Create(ctx, Upload) (File, error)`, `Get(ctx, FileID) (File, error)`, `ByRecord(ctx, audit.Ref) ([]File, error)`, `Open(ctx, FileID) (File, Content, error)`, `Delete(ctx, FileID) error`, `DeleteByRecord(ctx, audit.Ref) (int, error)`.
- `Blob` (the storage seam): `Put(ctx, key string, size int64, r io.Reader) error`, `Open(ctx, key) (io.ReadSeekCloser, error)`, `Delete(ctx, key) error` (idempotent), `List(ctx, func(key string, modified time.Time) error) error`, `Ping(ctx) error`, `Kind() StoreKind`, `Transactional() bool`. Implementations `postgres.NewBlob(db.DB)`, `disk.New(dir string)`, `s3.New(s3.Config)`. `ErrBlobMissing` (a row without bytes; `Unavailable`).
- Package functions: `ReadUpload(r *http.Request, field string) (*Multipart, error)`; `Serve(w http.ResponseWriter, r *http.Request, f File, c Content)` (never fails; headers under Behavior); `Detect(head []byte) (Kind, bool)`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "files"), Actor authz.ActorFunc, Blob Blob, TempDir string, Policy PolicySource (nil = DefaultPolicy)}` (+`Validate`); `New(Options) (*Service, error)` (no I/O; works against `db.None()`). `*Service` (safe for concurrent use; nothing to close): `Scoped(owner) (Store, error)`, `Ping(ctx) error`, `SweepOrphans(ctx) (Swept, error)` (the job), `ExportPerson(ctx, model.PersonID) (json.RawMessage, error)`, `DeletePerson(ctx, model.PersonID) (Deletion, error)`.
- Constants: `MaxNameBytes = 255`, `MaxFields = 32`, `MaxFieldBytes = 4096`, `SniffBytes = 512`, `ChunkBytes = 1 << 20`, `UploadCeiling = 1 << 30` (the route body cap), `UploadTimeout = 10 * time.Minute`, `ServeTimeout = 10 * time.Minute`, `OrphanAge = 24 * time.Hour`, `MaxFilesPerRecordCeiling = 200`, `JobSweepOrphans = "files.sweep-orphans"`, `LogoRecordKind = "settings.logo"`.
- Settings (core area `files`, `filesettings.Settings []settings.Decl`): `files.max_bytes` (Int, default 26214400, 1048576..1073741824), `files.quota_bytes` (Int, default 0 = unlimited, 0..), `files.max_per_record` (Int, default 20, 1..200). Config (Proposed change 3): `files.store` (`postgres|disk|s3`), `files.disk_dir`, `files.s3_*`. Audit actions `files.file.created|deleted|read`, `files.sweep.ran`. `postgres.Migrations fs.FS`; `Set = db.Set{Name: "files", Schema: "core"}`. No permissions, routes, signals or screens.
- `filestest.NewFake(owner) *Fake` (an in-memory `Store` validating like the real one; `Files()`, `Fail(err)`, `Blob()`), `filestest.NewBlob() *MemBlob`; `blobtest.Conformance(t, func(t) Blob)`.

**Key decisions.** 1 one `core.file` row per attachment, immutable (replace = delete + create); the owning record is an `audit.Ref` whose area must equal the scoped owner, so a module can create, list, open and delete only its own files (another owner's file is `NotFound`). 2 no two-step upload: `Create` runs inside the caller's `Write` transaction with the record it belongs to, so a file never exists without its record and no "pending upload" state exists. 3 the bytes live behind `Blob`; the recommended default is the `postgres` chunk store (`core.file_chunk`, 1 MiB chunks), because one `pg_dump` then holds everything, a read-only copy serves every file, deletion is transactional and nothing is orphaned (Q-FILES1; 07 says "disk first", which is why it is the owner's call); `disk` (`<data.dir>/files`) and `s3` (MinIO, Garage, SeaweedFS, Ceph RGW) are options behind the same seam. 4 the stored content type is sniffed from the bytes, never the declared one; a sniffed type outside `Upload.Accept` is `Invalid type`; the stored name is the client's base name, cleaned, with the extension forced to the sniffed kind. 5 only images and PDF are accepted in M4; no SVG (Q-FILES3) and no office formats, because nothing needs them yet and each needs its own sanitiser. 6 metadata is stripped from JPEG, PNG and WebP at upload (GPS, timestamps, device, comments; orientation kept) unless the module sets `KeepMetadata`, because phone photos of an incident must not carry where and when they were taken into a report nobody wanted to geotag (Q-FILES5). 7 files audits itself: `files.file.created` and `files.file.deleted` in the caller's transaction, and `files.file.read` on `Open` for a `sensitive` file always and a `personal` file when the actor is not the subject (no audit, no read); the module audits its record, not the bytes. 8 `Open` of a `personal` or `sensitive` file with no actor is `Unauthenticated` (fail closed); `public` and `internal` need none. 9 serving goes through `Serve`: sniffed `Content-Type`, `nosniff`, `Content-Disposition: inline` for images and PDF (`attachment` for any later kind), `Content-Security-Policy: default-src 'none'`, `Cache-Control` by level (`no-store` for personal and sensitive), `ETag` = SHA-256, ranges and conditional requests through `http.ServeContent`, and a write deadline of `ServeTimeout`. 10 limits are a regional's settings (`files.max_bytes`, `files.quota_bytes`, `files.max_per_record`) checked while streaming; the route body cap is the constant `UploadCeiling`, applied by the api shell per route (Proposed change 1). 11 every upload is spooled to `<data.dir>/files/tmp` first (bounded memory, sniff, strip, hash, known length for S3); temp files older than `OrphanAge` are swept. 12 a file's lifetime is its record's: no `files.retention*` key; the module's retention job deletes the record and calls `DeleteByRecord`; the only background work is the orphan sweep (`disk`/`s3` blobs and temp files with no row older than `OrphanAge`), registered with `jobs` as `files.sweep-orphans`, daily. 13 export and deletion: files is a `people.Source` named `files` appended after the modules: `ExportPerson` returns every file whose subject is the person with its content (base64) and the metadata of files they uploaded about others (Q-FILES6); `DeletePerson` anonymises `uploaded_by` and refuses with `Conflict in_use` when a module left a file whose subject is the person. 14 the logo stays a settings feature (Q-FILES8): settings owns `GET|PUT|DELETE /api/settings/logo` and stores the file under `Ref{Kind: "settings.logo", ID: fixed}` at level `public` with `Accept: files.Brand` and `Replace: true`; files only stores and serves it (Proposed change 6). 15 no virus scanning, no thumbnails, no resizing, no deduplication, no per-file sharing links in M4. 16 regional differences are the three settings and the store choice; no code path differs per regional.

**Errs kinds callers handle.** `Create`: `Internal` (not inside a `Write` transaction), `Invalid` with field codes `record` `required|malformed|scope`, `level` `one_of`, `name` `too_long {max}|control_chars`, `file` `required|too_large {max}|type|malformed`, `subject` `unknown`; `Conflict` with codes `quota {max}`, `too_many {max}`; `Unavailable` wrapping `db.ErrReadOnly`; `Unavailable` (blob store unreachable, temp dir unwritable); the `Recorder`'s kind; `Timeout`, `Canceled`. `Get`/`ByRecord`: `NotFound` (unknown ID or another owner's), `Invalid` (ref), store kinds. `Open`: `NotFound`, `Unauthenticated` (decision 8), the `Recorder`'s kind, `Unavailable` wrapping `ErrBlobMissing`, `Unavailable` (file stored in a store this process is not configured for), store kinds. `Delete`/`DeleteByRecord`: `NotFound` (`Delete` only), `Internal` (no `Write` transaction), `Unavailable` wrapping `db.ErrReadOnly`, store kinds. `ReadUpload`: `Invalid` wrapping `httpx.ErrUnsupportedMediaType` (not multipart), `Invalid` (`file` `required`, `fields` `too_many {max}`, `<field>` `too_long {max}`, `file` `order`), `*http.MaxBytesError` unchanged, `Timeout`, `Canceled`. `ExportPerson`: `Invalid` (zero ID), `Unavailable` wrapping `ErrBlobMissing`, store kinds. `DeletePerson`: `Invalid`, `Internal` (no `Write` transaction), `Conflict in_use`, `Unavailable` wrapping `db.ErrReadOnly`. `SweepOrphans`: `Unavailable` wrapping `db.ErrReadOnly`, `Unavailable`. `Ping`: `Unavailable`, `Timeout`. `Scoped`: `Invalid`. `New`/`Options.Validate`: `Invalid` with fields `files.db`, `files.clock`, `files.logger`, `files.tracer`, `files.audit`, `files.actor`, `files.blob`, `files.temp_dir`. Sentinel: `ErrBlobMissing` (check `errors.Is` before the kind).

**Owns.** Tables `core.file` (name is `personal` when the file is; the rest IDs and sizes), `core.file_chunk` (bytes, the file's level); settings area `files` (three keys); config keys `files.store`, `files.disk_dir`, `files.s3_endpoint`, `files.s3_bucket`, `files.s3_region`, `files.s3_prefix`, `files.s3_path_style`, `files.s3_access_key`, `files.s3_secret_key` (proposed); the directory `<data.dir>/files/` (`tmp/`, and the `disk` store's tree); audit area `files`; job `files.sweep-orphans`; spans `files.create`, `files.open`, `files.delete`, `files.sweep`, `files.blob.<op>` (Client); metrics `bms.files.uploads{outcome}`, `bms.files.reads{outcome}`, `bms.files.swept{what}`, `bms.files.blob_failures{op}`. No permissions, routes, signals or screens.

**Expects of dependencies.** `app`: part 10 (`build`: the `Blob` from `cfg.Files`, `New`, `audit.Scoped("files")`, the actor closure, `PolicySource` over a `files` `Reader`; `provision`: `a.deps.Files = svc.Scoped(m.Name)`; `link`: `people.Source{Name: "files"}` after the modules, a lazy `files.Store` scoped `settings` for the logo; `migrate` creates `<data.dir>/files/tmp` (0700); `check` prints `files: <store> ok` from `Ping`; a job registration when `jobs` is wired). `api`: `Route.MaxBodyBytes` and `Route.Timeout` honoured; `Cache-Control: no-store` only when the handler set none; multipart and binary fragments in OpenAPI. `module-contract`: `Deps.Files files.Store`, the two `Route` fields, `moduletest` supplying `filestest.NewFake`. `db`: `Transact`, `TxMode`, `AfterCommit`, `Wrap`, `ErrReadOnly`, `ReadOnly`, `Set`; `files` in the core set order after `jobs`. `audit`: a `Recorder` scoped `files`; `Ref`; `Record` outside a transaction spools on a read-only copy. `authz`: `ActorFunc`. `settings`: `filesettings.Settings` as core area `files`; the logo routes, screen field and `Public.Logo` (Proposed change 6). `people`: `Source` after the modules; `core.person` for the two foreign keys. `jobs`: a daily job; `files` works with none registered. `telemetry`: typed attributes; the metric rows. `web-shell`: `upload` in `@/api`, `ui/FileField`, `ui/Attachments`, the header logo, four field codes (Proposed change 7). `config`: the `files.*` keys (Proposed change 3).

**Open questions:** Q-FILES1 to Q-FILES7, each with a recommendation, at the end. **Proposed changes elsewhere:** 12, listed at the end; none applied.

## Purpose

Keep the bytes a module attaches to one of its records, with the record's permissions deciding who may read them.

## Responsibilities

- Accept an upload from a module's or core part's handler: stream it to a temp file under a size limit, sniff its type against what the caller accepts, strip image metadata, hash it, and store it with one metadata row inside the caller's transaction.
- Keep the metadata (`core.file`): the owning record, the subject person, the uploader, the sensitivity level the owner declared, the cleaned name, the sniffed type, size and SHA-256.
- Keep the bytes behind one `Blob` seam with three implementations: PostgreSQL chunks, local disk, S3-compatible.
- Hand the bytes back to the owning module (`Open`) and audit that read when the level requires it.
- Write the safe HTTP response for a file (`Serve`): correct type, no sniffing, no script execution, right caching, ranges.
- Delete a file, or every file of a record, inside the caller's transaction, and remove the bytes.
- Export and delete what it holds about a person, as a `people.Source`.
- Sweep orphaned blobs and temp files (a daily job on `jobs`).
- Declare the regional's limits as settings and report storage health to `bms check`.

## Not responsible for

- Deciding who may see a file: the owning module checks its own permission on its own route before calling `Open` (09 §10, "under the record's permissions"); `authz` holds the rules.
- HTTP routes and screens for attachments: the owning module (certificate on a qualification, photos on an incident) and `settings` (the logo). Files exports `ReadUpload` and `Serve` so those handlers stay thin.
- The body cap and request deadline on an upload or download route: the `api` shell, from `Route.MaxBodyBytes` and `Route.Timeout` (Proposed change 1).
- Retention of a file: the owning module's retention job deletes the record and its files (settings decision 11); files keeps no retention key.
- Virus or malware scanning: out of scope. Every accepted type is an image or a PDF served with `nosniff` and a restrictive CSP, and never executed by the server. A scanner seam (ClamAV is OSI) can be added later as an optional core implementation without changing `Store`.
- Thumbnails, resizing, image conversion, HEIC: later sub-abstractions when a consumer needs them. A phone asked for `image/*` uploads JPEG.
- Deduplication: two uploads of the same bytes are two files; each belongs to one record and dies with it.
- Sharing links, signed URLs, public galleries: later, if a module needs them.
- The web upload component and the API client's multipart support: `web-shell` (Proposed change 7).
- Backups: the operator's; `docs/operating.md` says what to back up per store (Proposed change 11).
- Scheduling the orphan sweep: `jobs`.

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Catalogue | say which kinds of file exist, how each is recognised from its bytes, and how it is served | `Kind`, `Accept`, `Detect`; table under Types |
| Intake | turn a request body into a verified, stripped, hashed temp file | `ReadUpload`, the spool, the size counter, `imgmeta` |
| Metadata | keep one row per file and enforce ownership, levels and audit | `core.file`, `Store`, `Scoped`, the audit entries |
| Blob stores | keep the bytes behind one seam with transactional and external implementations | `Blob`; `postgres`, `disk`, `s3`; `blobtest.Conformance` |
| Serving | write the safe HTTP response for a file | `Serve` |
| Person data | export and delete what files holds about a person | `ExportPerson`, `DeletePerson`, the `people.Source` |
| Hygiene | remove blobs and temp files that no row refers to; report health | `SweepOrphans`, `Ping`, the `check` line |

## Interface

```go
package files

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/model"
)

type fileTag struct{}

// FileID identifies one stored file.
type FileID = model.ID[fileTag]

// Kind is one accepted file type (the catalogue under Types). The zero Kind is invalid.
type Kind uint8

const (
	PNG Kind = iota + 1
	JPEG
	GIF
	WebP
	PDF
)

func (k Kind) String() string      // token: "png", "jpeg", "gif", "webp", "pdf"
func (k Kind) ContentType() string // the served Content-Type
func (k Kind) Extension() string   // the forced extension: ".png", ".jpg", ".gif", ".webp", ".pdf"
func (k Kind) Inline() bool        // true for every M4 kind (decision 9)
func (k Kind) Validate() error

// Accept is the set of kinds a caller takes. An empty Accept is Invalid.
type Accept []Kind

var (
	Images    = Accept{PNG, JPEG, GIF, WebP}
	Documents = Accept{PDF}
	Brand     = Accept{PNG, JPEG, WebP} // the logo; SVG waits on Q-FILES3
)

// Detect recognises a kind from the first SniffBytes of a file. It cannot fail: an
// unrecognised head answers (0, false).
func Detect(head []byte) (Kind, bool)

// StoreKind names a Blob implementation.
type StoreKind uint8

const (
	StorePostgres StoreKind = iota + 1
	StoreDisk
	StoreS3
)

func ParseStoreKind(s string) (StoreKind, error) // Invalid unless "postgres", "disk", "s3"
func (k StoreKind) String() string

// File is one attachment's metadata. Name is personal data when Level is personal or
// sensitive (a certificate is often named after its holder); LogValue renders the ID only.
type File struct {
	ID          FileID
	Record      audit.Ref      // the owning record; Kind's area is the owner (decision 1)
	Subject     model.PersonID // whom the file is about; zero for nobody in particular
	UploadedBy  model.PersonID // zero after the uploader is deleted, or for an operator
	Level       model.Sensitivity
	Name        string // cleaned base name with the kind's extension
	ContentType string // the sniffed type
	Size        int64
	SHA256      [32]byte
	Stripped    bool // metadata was removed at upload
	CreatedAt   time.Time
}

func (f File) Validate() error
func (f File) LogValue() slog.Value

// Upload is what a caller gives Create. Body is read once, to the end; the caller owns it.
type Upload struct {
	Record       audit.Ref
	Subject      model.PersonID
	Level        model.Sensitivity
	Name         string // the client's file name; cleaned; "" becomes "file"
	Accept       Accept
	KeepMetadata bool // keep EXIF/XMP (decision 6); false for anything about people
	Replace      bool // delete the record's existing files in the same transaction
	Body         io.Reader
}

func (u Upload) Validate() error // everything but Body and the bytes

// Content is a file's bytes, seekable for ranges. It is bound to the ctx given to Open and
// must be closed before that ctx ends; Serve does not close it.
type Content = io.ReadSeekCloser

// Store is what a module gets as Deps.Files and a core part gets from Scoped: every method
// sees only the owner's files. Consumers: volunteers (certificates), settings (the logo),
// later incidents and afterreports.
type Store interface {
	// Create stores an upload inside the caller's Write transaction and records
	// files.file.created. Record.Kind's area must be the owner. Consumers: every owner.
	Create(ctx context.Context, up Upload) (File, error)
	// Get answers one file's metadata. Consumers: an owner showing one attachment
	// (volunteers: the certificate card).
	Get(ctx context.Context, id FileID) (File, error)
	// ByRecord lists a record's files, oldest first. Consumers: an owner listing attachments
	// (incidents, afterreports), settings finding the logo, an owner's ExportPerson.
	ByRecord(ctx context.Context, rec audit.Ref) ([]File, error)
	// Open answers the metadata and the bytes, auditing the read when the level requires it
	// (decision 7). Called outside any transaction. Consumers: every owner's content route.
	Open(ctx context.Context, id FileID) (File, Content, error)
	// Delete removes one file inside the caller's Write transaction and records
	// files.file.deleted; external bytes go after commit. Consumers: an owner removing one
	// attachment; settings removing the logo.
	Delete(ctx context.Context, id FileID) error
	// DeleteByRecord removes every file of a record (same rules). Consumers: an owner
	// deleting a record, its retention job, its DeletePerson.
	DeleteByRecord(ctx context.Context, rec audit.Ref) (int, error)
}

// Multipart is one parsed upload request: the text fields that came before the file part,
// and the file part as a stream. The file part must be last (decision 11, Behavior).
type Multipart struct {
	Fields       map[string]string // at most MaxFields, each at most MaxFieldBytes
	Name         string            // the part's file name as sent
	DeclaredType string            // the part's Content-Type as sent; a hint, never trusted
	Body         io.Reader         // the file part; valid until the request body is closed
}

// ReadUpload parses a multipart/form-data request whose file part is named field.
// Consumers: every upload handler (volunteers, settings, later modules).
func ReadUpload(r *http.Request, field string) (*Multipart, error)

// Serve writes f's bytes with the headers under Behavior ("Serving"), honouring Range,
// If-None-Match and If-Modified-Since through http.ServeContent. It never fails: a bad
// range is a 416 and a client that is gone ends the write. Consumers: every content route.
func Serve(w http.ResponseWriter, r *http.Request, f File, c Content)

// Blob keeps bytes under a key. Implementations: postgres (transactional), disk, s3.
type Blob interface {
	// Put stores exactly size bytes from r (a temp file) under key, replacing nothing: a
	// key that exists is Conflict. A transactional store writes inside ctx's transaction.
	Put(ctx context.Context, key string, size int64, r io.Reader) error
	// Open answers the bytes; a missing key is Unavailable wrapping ErrBlobMissing.
	Open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	// Delete removes key; a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// List calls fn for every key, in no order; fn's error stops the walk and is returned.
	List(ctx context.Context, fn func(key string, modified time.Time) error) error
	// Ping checks the store can be written (disk: the dir; s3: a HEAD on the bucket;
	// postgres: nothing beyond the pool).
	Ping(ctx context.Context) error
	Kind() StoreKind
	// Transactional is true when Put and Delete take part in ctx's transaction (postgres).
	Transactional() bool
}

// ErrBlobMissing: the row exists and the bytes do not (an unsynced disk copy, a bucket
// restored from an older backup). Unavailable: retrying after a sync may help.
var ErrBlobMissing = errors.New("files: blob missing")

// Policy is the regional's limits (the three settings).
type Policy struct {
	MaxBytes     int64 // per file
	QuotaBytes   int64 // total stored; 0 = unlimited
	MaxPerRecord int
}

func (p Policy) Validate() error
func DefaultPolicy() Policy // 25 MiB, 0, 20

// PolicySource reads the policy at use-case time (app: over a settings.Reader scoped files).
type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

type Deletion struct{ Deleted, Anonymized int }
type Swept struct{ Blobs, TempFiles int }

type Options struct {
	DB      db.DB
	Clock   clock.Clock
	Logger  *slog.Logger
	Tracer  telemetry.Tracer
	Meter   telemetry.Meter
	Audit   audit.Recorder // scoped "files"
	Actor   authz.ActorFunc
	Blob    Blob
	TempDir string       // <data.dir>/files/tmp; created by bms migrate
	Policy  PolicySource // nil = DefaultPolicy
}

func (o Options) Validate() error
func New(o Options) (*Service, error)

type Service struct{ /* unexported */ }

// Scoped answers a Store that sees only owner's files (owner: a module name or core area).
func (s *Service) Scoped(owner string) (Store, error)
// Ping reports whether the blob store and the temp dir are usable (bms check).
func (s *Service) Ping(ctx context.Context) error
// SweepOrphans deletes external blobs with no row and temp files, both older than
// OrphanAge; a no-op for a transactional store beyond the temp files. The jobs job.
func (s *Service) SweepOrphans(ctx context.Context) (Swept, error)
// ExportPerson: the people.Source export (shape under Behavior). No transaction, no audit.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
// DeletePerson: the people.Source deletion, inside people's Write transaction.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (Deletion, error)
```

Blob constructors:

```go
package postgres // internal/core/files/postgres
var Migrations fs.FS
func NewBlob(d db.DB) (*Blob, error) // chunks in core.file_chunk; Transactional() true

package disk // internal/core/files/disk
func New(dir string) (*Blob, error) // Invalid when dir is empty; the dir is created at Ping

package s3 // internal/core/files/s3
type Config struct {
	Endpoint  string // host[:port], https unless PathStyle dev endpoints say otherwise
	Bucket    string
	Region    string
	Prefix    string // "" or "a/b/"
	PathStyle bool
	AccessKey secret.Secret
	SecretKey secret.Secret
	Timeout   time.Duration // per call; default 30 s
}
func (c Config) Validate() error
func New(c Config) (*Blob, error) // builds the client; no network
```

Test helpers:

```go
package filestest
func NewFake(owner string) *Fake      // an in-memory Store with the real validation and ownership rules
func (f *Fake) Files() []files.File   // what was created and not deleted
func (f *Fake) Fail(err error)        // every later call answers err
func NewBlob() *MemBlob               // an in-memory Blob (Transactional false)

package blobtest
func Conformance(t *testing.T, factory func(t *testing.T) files.Blob) // the Blob contract
```

### Method contracts

| Method | Consumer | Pre | Post |
|---|---|---|---|
| `Create` | every owner's upload use case | inside a `Write` transaction; `Upload.Validate` nil; `Record.Kind`'s area = owner; `Subject` zero or an existing person; `Body` non-empty | temp spool, sniff, strip, hash; policy checked; `Replace` deletes the record's files first; row inserted; blob put (transactional store: in the transaction; external: before the insert); `files.file.created` recorded in the transaction; `File` returned |
| `Get` | an owner showing one attachment | — | metadata, or `NotFound` for an unknown ID or another owner's |
| `ByRecord` | an owner listing a record's files; settings; exports | `rec` valid and in the owner's area | files oldest first; empty list when none; a ref in another area is `Invalid scope` |
| `Open` | an owner's content route | outside any transaction; for `personal`/`sensitive` an actor | `files.file.read` recorded when due (before the content is returned); `Content` open; the caller closes it |
| `Delete` | an owner removing an attachment | inside a `Write` transaction | row deleted; transactional blob deleted with it; external blob deleted in `AfterCommit` (a failure is logged, counted, and left to the sweep); `files.file.deleted` recorded |
| `DeleteByRecord` | an owner deleting a record, its retention job, its `DeletePerson` | inside a `Write` transaction | as `Delete` for each; count returned; zero is fine |
| `ReadUpload` | upload handlers | `Content-Type: multipart/form-data` with a boundary | text parts before the file collected; the file part streamed; a text part after the file is `Invalid file order` |
| `Serve` | content routes | `f` and `c` from `Open` | headers under Behavior; `http.ServeContent` |
| `Scoped` | `app` (per module and for settings) | owner is a module name or core area | a `Store` |
| `Ping` | `app` (`bms check`) | — | nil when the temp dir is writable and `Blob.Ping` is nil |
| `SweepOrphans` | `jobs` (daily); `bms check` does not run it | writable copy | external blobs with no row and temp files older than `OrphanAge` deleted; `files.sweep.ran` recorded with the counts in `Reason` |
| `ExportPerson` | `people` (`Source`) | — | the JSON under Behavior |
| `DeletePerson` | `people` (`Source`, inside its transaction) | inside a `Write` transaction | `uploaded_by` cleared on the person's uploads (counted `Anonymized`); any file with `subject = person` is `Conflict in_use` (the owner forgot to delete it) |

## Types

Defined here: `FileID`, `Kind`, `Accept`, `StoreKind`, `File`, `Upload`, `Content`, `Multipart`, `Policy`, `Deletion`, `Swept`. Uses `model.ID`, `model.PersonID`, `model.Sensitivity`, `audit.Ref`, `authz.ActorFunc`, `secret.Secret`.

The catalogue (decision 4, 5, 6, 9):

| Kind | Sniff (on the first 512 bytes) | Content-Type | Extension | Inline | Metadata stripped |
|---|---|---|---|---|---|
| `PNG` | `\x89PNG\r\n\x1a\n` | `image/png` | `.png` | yes | `eXIf`, `tEXt`, `zTXt`, `iTXt`, `tIME` chunks dropped |
| `JPEG` | `\xff\xd8\xff` | `image/jpeg` | `.jpg` | yes | every APPn and COM segment dropped except APP0 (JFIF), APP2 (ICC) and APP14 (Adobe); when the original carried an Exif Orientation other than 1, a minimal APP1 holding only that tag is written back |
| `GIF` | `GIF87a` / `GIF89a` | `image/gif` | `.gif` | yes | comment extensions dropped |
| `WebP` | `RIFF....WEBP` | `image/webp` | `.webp` | yes | `EXIF` and `XMP ` chunks dropped |
| `PDF` | `%PDF-` | `application/pdf` | `.pdf` | yes | none: a document's own metadata (author, title) is part of the document the person chose to upload |

`http.DetectContentType` recognises the four image kinds; PDF is a prefix check. Anything else, including HTML, SVG, XML, text, zip-based formats and HEIC, is `Invalid file type`, whatever the declared type or extension said.

## Data

Migration set `db.Set{Name: "files", Schema: "core"}`, `postgres.Migrations`. `people`'s set runs first (`core.person`).

`core.file`

| Column | Type | Sensitivity | Notes |
|---|---|---|---|
| `id` | uuid pk | — | `FileID` |
| `owner` | text not null | internal | the area of `record_kind`; indexed with `record_kind, record_id` |
| `record_kind` | text not null | internal | `audit.Ref.Kind` (`<area>.<ident>`) |
| `record_id` | uuid not null | internal | `audit.Ref.ID` |
| `subject_person_id` | uuid null, fk `core.person(id)` on delete restrict | internal (an ID) | whom the file is about; indexed |
| `uploaded_by` | uuid null, fk `core.person(id)` on delete restrict | internal (an ID) | cleared by `DeletePerson`; indexed |
| `level` | smallint not null | — | `model.Sensitivity`; check 1..4 |
| `name` | text not null | follows `level` (at least internal; `personal` or `sensitive` with the file) | ≤ 255 bytes, no control characters, no `/` or `\` |
| `content_type` | text not null | internal | from the catalogue |
| `size_bytes` | bigint not null | internal | > 0 |
| `sha256` | bytea not null | internal | 32 bytes; the `ETag` |
| `store` | text not null | internal | `postgres`, `disk`, `s3`: which `Blob` holds the bytes |
| `blob_key` | text not null | internal | the key in that store (the file ID in M4) |
| `stripped` | boolean not null | — | |
| `created_at` | timestamptz not null | internal | |

Indexes: `(owner, record_kind, record_id, created_at)`; `(subject_person_id)`; `(uploaded_by)`. No `updated_at`: rows are immutable.

`core.file_chunk` (the `postgres` blob store)

| Column | Type | Sensitivity | Notes |
|---|---|---|---|
| `file_id` | uuid not null, fk `core.file(id)` on delete cascade | — | |
| `seq` | integer not null | — | 0-based; pk `(file_id, seq)` |
| `data` | bytea not null | the file's level | ≤ `ChunkBytes` (1 MiB); every chunk but the last is exactly `ChunkBytes` |

Retention: a row lives as long as its record (decision 12). A file whose `subject_person_id` is set is deleted by its owner's `DeletePerson` before files' own runs; files' `DeletePerson` is the backstop that refuses to let people finish otherwise. Nothing here is kept after deletion (audit rows are audit's).

Files on disk, under the operator's control: `<data.dir>/files/tmp/<file-id>` (0600 in 0700; the spool; swept after `OrphanAge`), and for the `disk` store `<files.disk_dir>/<aa>/<bb>/<file-id>` (the first two bytes of the ID as hex, 0600 in 0700, written as `<name>.part` then renamed, fsynced with its directory). The `s3` store writes `<prefix><file-id>`.

Why each personal column is collected: `name` is what the person will recognise the file by on a phone screen and in their export; `uploaded_by` says who put a document into a safety record (a legal trail the owner module needs); `subject_person_id` is what makes export and deletion complete without trusting every module to remember its files.

## Permissions

Files declares no permission. Every operation runs under the permission of the owning record, checked by the owner before the call; the owner's spec names it. For the known consumers:

| Operation | Permission (checked by) | Scope |
|---|---|---|
| `Create`, `Delete`, `DeleteByRecord` on a volunteers qualification | the qualification's change permission (`volunteers`, its spec) | the person themself for an upload; the department's leads, or any lead for an org-wide qualification (D40) |
| `Open` a certificate | the qualification's read permission (`volunteers`) | the person, or a lead who may verify it |
| `Create`, `Delete` the logo | `settings.manage` (`settings`) | site |
| `Open` the logo | none: `AccessPublic` (`settings`) | — |
| `ExportPerson`, `DeletePerson` | `people.data.export`, `people.data.delete` or the person's own request (`people`) | site |
| `SweepOrphans` | none: a job run as the system (`jobs`) | — |

Files adds two checks of its own that no permission can lift: the owner scope (decision 1) and the actor requirement on `Open` of a `personal` or `sensitive` file (decision 8).

## Signals

None published: no consumer needs "a file was uploaded"; the owning record's own signal carries the event. None subscribed: deletion reaches files through `DeleteByRecord` (the owner) and the `people.Source` (the backstop), not through `people.person-deleted`, because both must run inside the deleting transaction.

## Settings

Core area `files`, `filesettings.Settings []settings.Decl` (leaf package, like `auditsettings`); read through `PolicySource`, which `app` implements over a `Reader` scoped `files`.

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `files.max_bytes` | Int | `26214400` (25 MiB) | 1048576..1073741824 | `settings.manage` |
| `files.quota_bytes` | Int | `0` (unlimited) | 0..9223372036854775807 | `settings.manage` |
| `files.max_per_record` | Int | `20` | 1..200 | `settings.manage` |

Validation errors are settings' (`range`). Catalogue keys `core.files.setting.<key>.title|description`. A `max_bytes` above `UploadCeiling` cannot be set (the range stops there), so the route cap is always at least the policy.

Configuration (process, `config.md`, Proposed change 3): `files.store` (`BMS_FILES_STORE`, enum `postgres|disk|s3`, default per Q-FILES1), `files.disk_dir` (`BMS_FILES_DISK_DIR`, default `<data.dir>/files/store`), `files.s3_endpoint`, `files.s3_bucket`, `files.s3_region` (default `us-east-1`, what MinIO and Garage accept), `files.s3_prefix` (default empty), `files.s3_path_style` (bool, default `true`: every self-hosted store uses it), `files.s3_access_key` and `files.s3_secret_key` (secrets, `*_FILE` accepted). `s3` requires endpoint, bucket, access key and secret key (`Invalid` naming the variable); `disk` requires `files.disk_dir`; the other store's keys are ignored but still validated for shape.

## UI (if any)

None owned. What the shell needs for the consumers is proposed to `web-shell` (Proposed change 7): `upload(route, {file, fields}, opts)` in `@/api` (multipart over `fetch`, same `ApiError`, progress optional), `ui/FileField` (a phone-first file input: `accept` from the kinds, `capture` for the camera, shows name and size, the `too_large`, `type`, `quota`, `too_many` field codes), `ui/Attachments` (a list of `File` rows with a download link and a remove button the screen enables), and the header logo (`<img src="/api/settings/logo?v=<id>">` when `Public.Logo` is set, with the regional's name as `alt`). The owners' screens are theirs.

## Behavior

**Create.** 1 `Upload.Validate`; `Record.Kind`'s area must equal the scoped owner (`Invalid record scope`), `Level.Validate`, `Accept` non-empty, `Name` cleaned (`path.Base` after replacing `\` with `/`, trimmed, control characters refused, ≤ `MaxNameBytes`; empty becomes `file`). 2 `db.TxMode(ctx)` must be `Write` (`Internal` otherwise: a file must commit with its record). 3 the policy is read. 4 the body is streamed to `<TempDir>/<new id>` through a counter: the first `SniffBytes` are sniffed with `Detect` and checked against `Accept` before the rest is read (`Invalid file type`, read stops); more than `MaxBytes` bytes is `Invalid file too_large {max}` (read stops; the temp file is removed); an empty body is `Invalid file required`. 5 for JPEG, PNG, GIF and WebP with `KeepMetadata` false, `imgmeta.Strip` rewrites the temp file; a structure the stripper cannot parse is `Invalid file malformed` (a PDF is never rewritten). 6 SHA-256 and size are taken from the final temp file. 7 inside the transaction: `Replace` deletes the record's files; the record's count against `MaxPerRecord` (`Conflict too_many {max}`), the sum of `size_bytes` plus this size against `QuotaBytes` when set (`Conflict quota {max}`; both are read in the transaction without a lock, so two concurrent uploads may overshoot by one file, which the spec accepts); `subject` must exist (the foreign key; `Invalid subject unknown` from `23503`); the row is inserted; a transactional blob is put in the same transaction after the insert; an external blob is put *before* the insert so a crash leaves at most an orphan the sweep removes, never a row without bytes. 8 `files.file.created` is recorded (`Record` = `audit.RefOf("files.file", id)`, `Subject`, `Level`, `Reason` `record:<kind>:<id>`). 9 the temp file is removed whatever the outcome (deferred; a removal failure is logged once and left to the sweep). `UploadedBy` is the actor when there is one, else zero (an operator command); `Create` never fails for want of an actor because the owner's use case already required one. Never logged: the name, the declared type, the bytes.

**Open.** 1 the row is loaded (`NotFound` for an unknown ID or another owner). 2 for `personal` and `sensitive`: an actor is required (`Unauthenticated` without one; the superuser counts); for `sensitive` always, and for `personal` when the actor's person is not `Subject`, `files.file.read` is recorded (`Fields: ["content"]`, `Level`, `Subject`, `Reason` as above) before the bytes are opened; a `Record` failure fails the read (audit decision 3). On a read-only copy that entry spools (audit decision 4), so a lead can still view a certificate on site. 3 `store` must equal `Blob.Kind()` (`Unavailable` otherwise: "stored in <kind>, this process is configured for <kind>"; the operator migrates or reconfigures). 4 `Blob.Open`; `ErrBlobMissing` is returned as is (wrapped, `Unavailable`), counted (`bms.files.reads{outcome="missing"}`) and logged once with the file ID. The `Content` carries the ctx it was opened under; the postgres reader fetches chunks lazily (one `SELECT data ... WHERE file_id = $1 AND seq = $2` per chunk, seeking by chunk index), so memory is one chunk; the disk reader is the `*os.File`; the s3 reader issues a ranged `GET` per seek.

**Serving.** `Serve` sets: `Content-Type` = `f.ContentType` (the shell already sets `X-Content-Type-Options: nosniff`); `Content-Disposition: inline; filename*=UTF-8''<name>` (plus an ASCII `filename=` fallback, through `mime.FormatMediaType`) for a kind whose `Inline()` is true, `attachment` otherwise; `Content-Security-Policy: default-src 'none'` (no script, no frame content, no loads from the file, whatever the browser decides to render it as); `ETag: "<sha256 hex>"`; `Last-Modified` = `CreatedAt`; `Cache-Control`: `private, no-store` for `personal` and `sensitive`, `private, max-age=0, must-revalidate` for `internal`, `public, max-age=86400` for `public`; `Content-Length` and ranges through `http.ServeContent` (`206`, `416`, `304`). It then extends the connection's write deadline to now + `ServeTimeout` with `http.NewResponseController(w).SetWriteDeadline` (a phone on a venue link downloading a 20 MB PDF outlives the shell's 30 s). The shell's `no-store` default must yield to a `Cache-Control` the handler set (Proposed change 1). Files never adds `Access-Control-*` (api D6).

**ReadUpload.** `r.MultipartReader()` (`Invalid` wrapping `httpx.ErrUnsupportedMediaType` when the request is not multipart); parts are read in order: a text part is collected into `Fields` (more than `MaxFields` is `Invalid fields too_many {max}`; longer than `MaxFieldBytes` is `Invalid <field> too_long {max}`); the first part named `field` with a file name is returned as the stream and parsing stops there (any part after it is unread; a text part the handler needs after the file is `Invalid file order` when the handler asks for it, so the rule "fields first, file last" is enforced on the client in `ui/FileField`); no file part is `Invalid file required`. The shell's `MaxBytesReader` (from `Route.MaxBodyBytes`) still caps the whole body at `UploadCeiling`; the policy cap is the one a person normally hits, as a `400` with the max in `params`.

**Delete.** Inside a `Write` transaction (`Internal` otherwise); the row is deleted (a transactional blob goes with it by cascade); for an external blob `db.AfterCommit` deletes it, and a failure there is logged once with the file ID, counted (`bms.files.blob_failures{op="delete"}`) and left to the sweep; `files.file.deleted` is recorded. `DeleteByRecord` does the same per row and returns the count (0 with no error for none).

**Sweep.** On a writable copy only (`Unavailable` wrapping `db.ErrReadOnly` otherwise; jobs skips): temp files older than `OrphanAge` are removed; for an external store, `Blob.List` is walked and every key that is older than `OrphanAge` and has no `core.file` row with that `blob_key` and `store` is deleted (the age keeps a blob written before its row's commit); counts go into `files.sweep.ran` (`Reason` `blobs:<n>,temp:<n>`, actor system) and `bms.files.swept{what}`. Registered with `jobs` as `files.sweep-orphans`, daily; the job's design is jobs'. With no `jobs` wired (`Deps.Jobs` nil in an older assembly) nothing runs and `bms check` says so.

**Export.** `ExportPerson(person)` returns one JSON object:

```json
{
  "about":    [{"id": "...", "record": {"kind": "volunteers.qualification", "id": "..."},
                "name": "...", "content_type": "application/pdf", "size": 1234,
                "sha256": "<hex>", "created_at": "...", "content": "<base64>"}],
  "uploaded": [{"id": "...", "record": {...}, "name": "...", "content_type": "...",
                "size": 1234, "sha256": "<hex>", "created_at": "..."}]
}
```

`about` is every file whose `subject_person_id` is the person, with content (decision 13, Q-FILES6); `uploaded` is every file the person uploaded whose subject is someone else or nobody, metadata only. Runs in no transaction; a missing blob fails the export (`Unavailable` wrapping `ErrBlobMissing`) rather than ship an incomplete one. people's export document is assembled in memory, so an export is as large as the person's files; the spec accepts this for M4 and notes the archive format as a later change (Q-FILES6).

**Deletion.** `DeletePerson(person)` inside people's transaction: `UPDATE core.file SET uploaded_by = NULL WHERE uploaded_by = $1` (counted `Anonymized`); then `SELECT count(*) ... WHERE subject_person_id = $1`; a count above zero is `Conflict in_use` with the owners named in the message (`volunteers: 2`), which rolls back people's whole deletion (people decision 11) and tells the operator which module failed to clean up. `Deleted` is always 0: files deletes nothing of its own here, by design.

**Read-only copies.** Every read works (`Get`, `ByRecord`, `Open`, `Serve`; the audit entry spools). `Create`, `Delete`, `DeleteByRecord`, `DeletePerson` fail `Unavailable` wrapping `db.ErrReadOnly` from the row statement (the api answers `503 read_only`); an external blob is never written on a read-only copy because the row's `TxMode` check and the first statement come first. Where the bytes are: `postgres`: replicated with the data, every file is there; `s3`: a shared bucket, every file is there; `disk`: only what the operator's sync copied, a gap is `ErrBlobMissing` (`503`, `Retry-After: 5`), and the operator docs say to sync `files.disk_dir` with the data (Proposed change 11). This is the main reason for the recommendation in Q-FILES1.

**Ownership and privacy.** A `Store` from `Scoped(owner)` filters every statement by `owner`, so no handler can serve another module's file by guessing an ID (decision 1). `File.LogValue` is the ID; names, declared types and bytes never reach logs, spans, metrics or error messages; `telemetrytest` proves it. Error messages name field codes only.

**Concurrency and lifecycle.** `*Service` and every `Blob` are safe for concurrent use; `Create` holds one temp file per call. Nothing to close: the postgres store uses the pool; disk holds no handles between calls; the s3 client is stateless. `Start` registers the job (never writes); there is no `Stop`. `New` does no I/O, so `bms openapi` and `bms check` build against `db.None()`; `Ping` is `check`'s I/O.

**Idempotency and retries.** `Create` is not idempotent (two calls are two files; the owner's `Replace` or its own uniqueness rule prevents doubles); `Delete`, `DeleteByRecord`, `Blob.Delete` and the sweep are idempotent; the sweep retries what `AfterCommit` could not delete. Files retries nothing; the caller retries `Unavailable` where its spec says.

**Store choice at runtime.** One `Blob` per process (`files.store`); the `store` column lets a later `bms files migrate <from> <to>` move bytes without a schema change. In M4 a file in another store is `Unavailable` on `Open`.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Options.Validate`/`New` | a nil `DB`, `Clock`, `Logger`, `Tracer`, `Audit`, `Actor` or `Blob`, or an empty `TempDir` | Invalid (`files.<field>`) | fix the assembly | `TestNew_MissingDependency_ReturnsInvalid` |
| F2 | `Scoped` | owner is not a module name or core area | Invalid | fix the assembly | `TestScoped_BadOwner_ReturnsInvalid` |
| F3 | `Create` | called outside a `Write` transaction | Internal | fix the owner | `TestCreate_NoWriteTx_ReturnsInternal` |
| F4 | `Create` | `Record.Kind`'s area is not the owner | Invalid (`record`, `scope`) | fix the owner | `TestCreate_ForeignRecord_ReturnsInvalidScope` |
| F5 | `Create` | zero or malformed `Record`, empty `Accept`, bad `Level` | Invalid (`record` `required|malformed`, `level` `one_of`) | fix the owner | `TestCreate_BadUpload_ReturnsInvalid` |
| F6 | `Create` | name over `MaxNameBytes` or with control characters | Invalid (`name` `too_long|control_chars`) | show the field error | `TestCreate_BadName_ReturnsInvalid` |
| F7 | `Create` | empty body | Invalid (`file`, `required`) | show "choose a file" | `TestCreate_EmptyBody_ReturnsInvalidRequired` |
| F8 | `Create` | the sniffed kind is not in `Accept` (an HTML file named `.png`, an SVG, a zip) | Invalid (`file`, `type`) | show "this kind of file isn't accepted" | `TestCreate_UnacceptedType_ReturnsInvalidType` |
| F9 | `Create` | bytes over `Policy.MaxBytes` | Invalid (`file`, `too_large {max}`) | show the limit | `TestCreate_OverMaxBytes_ReturnsInvalidTooLarge` |
| F10 | `Create` | the image cannot be parsed for stripping | Invalid (`file`, `malformed`) | show "the file is damaged" | `TestCreate_CorruptImage_ReturnsInvalidMalformed` |
| F11 | `Create` | the record already holds `MaxPerRecord` files | Conflict (`too_many {max}`) | show the limit | `TestCreate_RecordFull_ReturnsConflictTooMany` |
| F12 | `Create` | the quota would be exceeded | Conflict (`quota {max}`) | tell an admin | `TestCreate_OverQuota_ReturnsConflictQuota` |
| F13 | `Create` | `Subject` names no person (`23503`) | Invalid (`subject`, `unknown`) | fix the owner | `TestCreate_UnknownSubject_ReturnsInvalid` (integration) |
| F14 | `Create` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | api `503 read_only` | `TestCreate_ReadOnly_ReturnsUnavailableReadOnly` |
| F15 | `Create` | temp dir unwritable or full | Unavailable | api 503; operator | `TestCreate_TempDirUnwritable_ReturnsUnavailable` |
| F16 | `Create` | `Blob.Put` fails (disk full, bucket unreachable) | Unavailable | api 503; nothing stored (the row is never inserted) | `TestCreate_BlobPutFails_ReturnsUnavailableNoRow` |
| F17 | `Create` | the audit `Record` fails | the Recorder's kind | the transaction rolls back | `TestCreate_AuditFails_RollsBack` |
| F18 | `Create` | ctx deadline while streaming | Timeout / Canceled | api 504 / nothing; temp file removed | `TestCreate_Deadline_ReturnsTimeoutAndCleansTemp` |
| F19 | `Get` | unknown ID, or another owner's file | NotFound | 404 | `TestGet_OtherOwner_ReturnsNotFound` |
| F20 | `ByRecord` | a ref outside the owner's area | Invalid (`record`, `scope`) | fix the owner | `TestByRecord_ForeignArea_ReturnsInvalid` |
| F21 | `Open` | unknown ID or another owner's | NotFound | 404 | `TestOpen_OtherOwner_ReturnsNotFound` |
| F22 | `Open` | `personal`/`sensitive` file with no actor | Unauthenticated | 401; fix the owner's route | `TestOpen_SensitiveNoActor_ReturnsUnauthenticated` |
| F23 | `Open` | the audit `Record` fails | the Recorder's kind | no bytes are returned | `TestOpen_AuditFails_ReturnsErrorNoContent` |
| F24 | `Open` | the row exists, the blob does not | Unavailable wrapping `ErrBlobMissing` | api 503, `Retry-After`; operator syncs | `TestOpen_BlobMissing_ReturnsUnavailableSentinel` |
| F25 | `Open` | the file's `store` is not the configured one | Unavailable | operator reconfigures or migrates | `TestOpen_OtherStore_ReturnsUnavailable` |
| F26 | `Open` | called inside a `Read` transaction for an audited level | Internal (from audit) | fix the owner | `TestOpen_InReadTx_ReturnsInternal` |
| F27 | `Delete`/`DeleteByRecord` | outside a `Write` transaction | Internal | fix the owner | `TestDelete_NoWriteTx_ReturnsInternal` |
| F28 | `Delete` | unknown or another owner's | NotFound | 404 | `TestDelete_OtherOwner_ReturnsNotFound` |
| F29 | `Delete` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestDelete_ReadOnly_ReturnsUnavailable` |
| F30 | `Delete` | external `Blob.Delete` fails after commit | none returned (logged once, counted, swept later) | nothing | `TestDelete_BlobDeleteFails_LogsAndCounts` |
| F31 | `ReadUpload` | not `multipart/form-data` | Invalid wrapping `httpx.ErrUnsupportedMediaType` | api 415 | `TestReadUpload_NotMultipart_ReturnsUnsupportedMediaType` |
| F32 | `ReadUpload` | no file part | Invalid (`file`, `required`) | 400 | `TestReadUpload_NoFilePart_ReturnsInvalidRequired` |
| F33 | `ReadUpload` | too many or too long text parts | Invalid (`fields` `too_many`, `<field>` `too_long`) | 400 | `TestReadUpload_FieldLimits_ReturnsInvalid` |
| F34 | `ReadUpload` | body over the route cap | `*http.MaxBytesError` unchanged | api 413 | `TestReadUpload_OverRouteCap_Returns413` |
| F35 | `Serve` | an unsatisfiable `Range` | none (416 written by `ServeContent`) | — | `TestServe_BadRange_416` |
| F36 | `Serve` | the client is gone mid-write | none (write ends) | — | `TestServe_ClientGone_Stops` |
| F37 | `ExportPerson` | zero ID | Invalid | fix the caller | `TestExportPerson_ZeroID_ReturnsInvalid` |
| F38 | `ExportPerson` | a subject file's blob is missing | Unavailable wrapping `ErrBlobMissing` | people reports the export failed | `TestExportPerson_BlobMissing_ReturnsUnavailable` |
| F39 | `DeletePerson` | a file with `subject = person` remains | Conflict (`in_use`) | people rolls back and names the owner | `TestDeletePerson_SubjectFileRemains_ReturnsConflictInUse` |
| F40 | `DeletePerson` | outside a `Write` transaction; read-only copy | Internal; Unavailable wrapping `db.ErrReadOnly` | fix the caller; 503 | `TestDeletePerson_NoWriteTx_ReturnsInternal`, `TestDeletePerson_ReadOnly_ReturnsUnavailable` |
| F41 | `SweepOrphans` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | jobs skips the run | `TestSweep_ReadOnly_ReturnsUnavailable` |
| F42 | `SweepOrphans` | `Blob.List` fails | Unavailable | jobs retries next day | `TestSweep_ListFails_ReturnsUnavailable` |
| F43 | `Ping` | temp dir unwritable, disk dir uncreatable, bucket unreachable or forbidden | Unavailable | `bms check` prints it | `TestPing_StoreUnusable_ReturnsUnavailable` |
| F44 | `disk.New`/`s3.Config.Validate` | empty dir; missing endpoint, bucket or keys | Invalid (the config variable) | exit 2 | `TestS3Config_Missing_ReturnsInvalid` |
| F45 | `Blob.Put` (every store) | the key exists | Conflict | never expected (fresh IDs); conformance | `TestBlob_PutTwice_ReturnsConflict` (blobtest) |
| F46 | `Blob.Open` (every store) | missing key | Unavailable wrapping `ErrBlobMissing` | see F24 | `TestBlob_OpenMissing_ReturnsSentinel` (blobtest) |
| F47 | `Blob.Put` (every store) | fewer or more bytes than `size` | Invalid; nothing kept | fix the caller | `TestBlob_PutShortRead_ReturnsInvalidAndKeepsNothing` (blobtest) |
| F48 | any | ctx done before a statement or blob call | Timeout / Canceled | as the kind says | `TestStore_ContextDone_ReturnsTimeout` |

`Detect`, `Kind` methods and `DefaultPolicy` cannot fail: they are pure lookups over a fixed table.

## Lock-in check

External technology wrapped: object storage behind `Blob`. The shipped implementations are PostgreSQL (`postgres`, the recommended default) and the local filesystem (`disk`), both free of any vendor; the `s3` implementation speaks the S3 API through `github.com/minio/minio-go/v7` (Apache-2.0) and is tested in integration against MinIO (AGPL-3.0, self-hosted, in `lib/testkit` and `deploy/compose`); Garage, SeaweedFS and Ceph RGW are other OSI-licensed stores that speak the same API, and AWS S3 and its commercial lookalikes work without a code change. Every `Blob` passes `blobtest.Conformance`. No feature of the core needs `s3`.

## Test plan

- **Unit (no infrastructure):** the service over `dbtest.NewFake()` for transaction mode and read-only behaviour, `filestest.NewBlob()` for bytes, `audittest.NewFake("files")`, `telemetrytest.New(t)` (no name, declared type or bytes in any record or span; the fake's seed markers never appear), a fake clock, an `ActorFunc` the test sets. Table tests over F1–F12, F14–F18, F19–F23, F25–F33, F35–F42, F48. The catalogue: a fixture per kind plus HTML-named-`.png`, SVG, zip, HEIC, PDF-with-leading-junk, all refused. The stripper: JPEG with GPS and Orientation 6 (GPS gone, orientation kept, pixels byte-identical), PNG with `eXIf` and `tEXt`, WebP with `EXIF`, corrupt structures (F10), `KeepMetadata` leaves bytes identical. `ReadUpload`: fields before and after the file, limits, non-multipart. `Serve`: every header per level, `inline` for images and PDF, `ETag`/`304`, `Range`/`206`, `416`, the CSP, no `Access-Control-*`, the write deadline extended (a `ResponseController`-aware recorder). Ownership: two scoped stores, each blind to the other's files (F4, F19–F21, F28). `DeletePerson` and `ExportPerson` shapes.
- **Blob conformance (`blobtest.Conformance`):** put/open/delete/list, `Conflict` on a repeated key, `ErrBlobMissing`, short and long reads (F45–F47), seeking, concurrent puts; run for `filestest.MemBlob` and `disk` (on `t.TempDir()`) in unit tests, and for `postgres` (`dbtest.TempDB`) and `s3` (MinIO, `BMS_TEST_S3_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY`) under `//go:build integration`.
- **Integration (`//go:build integration`):** migrations apply and `CheckSchema` passes; `Create` with the postgres store commits chunks atomically with the row and rolls back on an audit failure (F17); the foreign keys (F13; `DeletePerson` RESTRICT backstop F39); `Open` on a read-only `TempDB` (`default_transaction_read_only`) serves and spools the audit entry; the sweep over disk and MinIO with an orphan older and younger than `OrphanAge`; the chunk reader on a 3.5 MiB file with seeks across chunk boundaries.
- **Privacy:** `telemetrytest` capture over every path with a seeded name containing a marker; the audit entries carry IDs, levels and tokens only; the export of a person with one subject file and one uploaded file.
- **Consumers' tests:** `moduletest.Conformance` fills a nil `Deps.Files` with `filestest.NewFake(manifest.Name)`; a module's `seeded_person_delete_counts_and_is_idempotent` passes only when its `DeletePerson` left no subject file in the fake (the fake's `DeletePerson` applies F39).
- **End-to-end (app):** with the logo (Proposed change 6) in place: upload a PNG as settings admin, see it in the header as a participant, replace it, remove it; an HTML file renamed `.png` is refused with `type`.

## Open questions

- **Q-FILES1. Which blob store is the shipped default?** 07 and 09 §10 say "local disk first". *Recommendation:* `postgres` (the chunk store) as the default, with `disk` and `s3` as options. Reasons: one `pg_dump` backs up everything (privacy-data: "backups contain everything"); a read-only or on-site copy has every file without a second sync mechanism ("files follow the data to the on-site copy", 09 §10); a file commits and dies with its record, so there is nothing to sweep; the volumes a regional holds (hundreds of certificates and photos, a few gigabytes) are well inside what PostgreSQL handles, and `files.max_bytes` keeps single files bounded. If the owner prefers `disk`, nothing in the spec changes except the default of `files.store` and the operator docs.
- **Q-FILES2. Does the `s3` store ship in M4?** *Recommendation:* specify it here, build `postgres` and `disk` in M4, and build `s3` (with its MinIO integration test and compose service) when a regional asks for it or when Phase 1 packaging needs it. The seam is proven by two implementations already.
- **Q-FILES3. SVG logos?** Many regionals have a vector logo. SVG can carry scripts and external references; accepting it needs a sanitiser (parse, allow-list elements and attributes, refuse `script`, `foreignObject`, event handlers and external `href`s). *Recommendation:* not in M4; `Brand` is PNG, JPEG and WebP; add `SVG` (public files only, sanitised) as a later catalogue entry.
- **Q-FILES4. The limits' defaults:** `files.max_bytes` 25 MiB, `files.quota_bytes` unlimited, `files.max_per_record` 20. *Recommendation:* keep them; a phone photo is 3–12 MB and a scanned certificate under 5 MB; a regional on a small disk sets a quota.
- **Q-FILES5. Strip image metadata by default?** GPS, timestamps and device identifiers leave a phone photo unless removed; the owner module may keep them with `KeepMetadata` (art placement photos, later). *Recommendation:* strip by default, keep only orientation; incidents never keeps.
- **Q-FILES6. Does a person's export include the bytes of files about them?** *Recommendation:* yes, inline as base64 in the one JSON document people already produces (a data-access request is for a copy, and a certificate is a copy of the person's data). The cost is export size and memory for a rare operation; when a regional needs it, people's export becomes an archive (JSON plus files) in a later change to `people.md`, without changing this interface.
- **Q-FILES7. The logo lives with `settings`** (its routes, screen field and audit actions; files stores and serves it), not as a files feature. *Recommendation:* yes (decision 14); it keeps files a service with no product surface of its own and keeps the regional's branding in one place. Approving this approves Proposed change 6.

## Proposed changes elsewhere

1. **`plan/design/api.md` (row 16):** (a) `module.Route` (and the core `Route` types `app` converts) gain `MaxBodyBytes int64` and `Timeout time.Duration` (zero = the shell's defaults; a route may only raise them, up to 1 GiB and 10 minutes); the pipeline applies the route's cap in `MaxBytesReader` and the route's timeout to the ctx deadline and the connection's read and write deadlines through `http.ResponseController`. (b) `Cache-Control: no-store` on `/api/` is set only when the handler set no `Cache-Control` (files sets its own per level). (c) The "File uploads and downloads" row in "Not responsible for" points here and lists what the shell now provides. (d) OpenAPI: an upload route documents a `multipart/form-data` request body and a content route a binary response (`image/*`, `application/pdf`), so the generated client types them. (e) Failure modes: a route cap above the ceiling or a timeout above 10 minutes is `Invalid` at `New`.
2. **`plan/design/module-contract.md` (row 4):** `Deps.Files files.Store` (scoped per module: the module's name is the owner); the two `Route` fields in 1(a) with `Validate` checking their ceilings; `moduletest.Conformance` fills a nil `Deps.Files` with `filestest.NewFake(manifest.Name)`, and the leak rule scans the fake's `Files()` names for the seed markers (a module must not copy a legal name into a file name). The `Files` row in the `Deps` table moves from "Later" to M4.
3. **`plan/design/config.md` (row 3):** a `Files` section with the keys under Settings (`BMS_FILES_STORE` enum `postgres|disk|s3`, default per Q-FILES1; `BMS_FILES_DISK_DIR` default `<data.dir>/files/store`; `BMS_FILES_S3_ENDPOINT`, `_BUCKET`, `_REGION` (`us-east-1`), `_PREFIX`, `_PATH_STYLE` (`true`), `_ACCESS_KEY` and `_SECRET_KEY` (secrets, `*_FILE`)); the rule that `s3` requires its four, `disk` its dir; the "Later keys" row loses "S3-compatible access keys (`files`)".
4. **`plan/design/app.md` (row 18):** part 10 `files`: `declare` adds `filesettings.Settings` as core area `files` and the migration set; `build` makes the `Blob` from `cfg.Files` (`postgres.NewBlob(a.pool)`, `disk.New`, `s3.New`), calls `files.New` with `audit.Scoped("files")`, the actor closure, `TempDir` `<data.dir>/files/tmp` and a `PolicySource` over a `files` `Reader`; `provision` sets `a.deps.Files = svc.Scoped(m.Name)`; `link` appends `people.Source{Name: "files", Exporter: adapter}` after the modules and gives settings a lazy `files.Store` scoped `settings`; `migrate` creates `<data.dir>/files/tmp` (0700) and, for `disk`, `files.disk_dir`; `check` prints `files: <store> ok` or the `Ping` error; the job registration once `jobs` exists. The files table under "Owns" gains the two directories.
5. **`plan/design/db.md` (row 5):** the pending proposed change 10 list marks `files` as exporting `Set{Name: "files", Schema: "core"}` with `postgres.Migrations`, after `jobs` in the core set order.
6. **`plan/design/settings.md` (row 15):** the logo, as D39 deferred it here: `Options.Files files.Store` (scoped `settings`, lazy from `app`); routes `GET /api/settings/logo` (`AccessPublic`; `ByRecord(logoRef)`, `Open`, `files.Serve`; `404 not_found` when none), `PUT /api/settings/logo` (`settings.manage`; `ReadUpload(r, "file")`, `Create{Record: logoRef, Level: model.Public, Accept: files.Brand, Replace: true}`; `Route.MaxBodyBytes = files.UploadCeiling`, `Route.Timeout = files.UploadTimeout`; a logo over `MaxLogoBytes = 2 MiB` is `Invalid file too_large` before the policy cap), `DELETE /api/settings/logo` (`settings.manage`; `DeleteByRecord`); `logoRef = audit.Ref{Kind: files.LogoRecordKind, ID: sha256("settings.logo")[:16]}` (the fixed-ID pattern of departments-events decision 25); `Public` gains `Logo string` (the file ID, `""` when none) so the shell can version the image URL; audit actions `settings.logo.changed|removed`; the `/settings` regional area gains a logo field (`ui/FileField`, `accept` PNG/JPEG/WebP, preview, remove); "Not responsible for" drops "the logo (comes with `files`)"; decision 8 is marked superseded by this change.
7. **`plan/design/web-shell.md` (row 17):** `@/api` gains `upload(route, {file, fields}, opts)` (multipart through the same `fetch` path, fields before the file, the same `ApiError`); `ui/FileField` and `ui/Attachments` (under UI above); the header renders `/api/settings/logo?v=<Public.logo>` when set, the regional's name as `alt` and as text otherwise; catalogue keys `field.too_large`, `field.type`, `field.quota`, `field.too_many`, `field.order`, `field.malformed` (if absent); the e2e smoke covers the logo round trip. ESLint keeps `fetch` inside `@/api`.
8. **`plan/design/people.md` (row 8):** a `Source` named `files` is linked after the modules (`app`); the export document gains `"files": {...}` (the shape under Behavior); the deletion report's `Modules["files"]` carries `Anonymized`; decision 11's order reads "modules in build order, then files, then authz, then identity, then people's rows"; a module's `DeletePerson` must delete its files with subject = the person or people's deletion fails `Conflict in_use` naming the module.
9. **`plan/design/jobs.md` (row 14, Draft in parallel):** a daily job `files.sweep-orphans` calling `(*files.Service).SweepOrphans`, skipped on a read-only copy, run as the system actor; files needs nothing else from jobs.
10. **`plan/design/telemetry.md` (row 15b):** metric rows `bms.files.uploads{outcome: ok|invalid|conflict|unavailable|timeout}`, `bms.files.reads{outcome: ok|missing|denied|unavailable}`, `bms.files.swept{what: blob|temp}`, `bms.files.blob_failures{op: put|delete|list}`; span names `files.create`, `files.open`, `files.delete`, `files.sweep`, `files.blob.put|open|delete|list` (Client).
11. **`docs/operating.md` (not yet written):** per store, what to back up (`postgres`: the database; `disk`: the database and `files.disk_dir` together, from the same moment; `s3`: the database and the bucket's own versioning or replication), how a read-only or on-site copy gets the files (`postgres` and `s3`: nothing to do; `disk`: sync `files.disk_dir` with the data, or choose another store), the temp dir, the daily sweep, and the lines `bms check` prints.
12. **`plan/07-abstractions.md`:** the `files` entry becomes: *Purpose:* keep the bytes a module attaches to one of its records, with the record's permissions deciding who may read them. *Owns:* the `Store` seam modules get as `Deps.Files`, the metadata (`core.file`), the `Blob` seam with the `postgres` (recommended default), `disk` and `s3` stores, the catalogue of accepted kinds (images and PDF), metadata stripping, the safe `Serve`, the audit of content reads, the person-data export and deletion backstop, the orphan sweep, settings `files.max_bytes|quota_bytes|max_per_record`, config `files.*`. *Doesn't do:* permissions (the owning module), routes and screens (the owner; the logo is settings'), retention (the owner's job), virus scanning, thumbnails. Also `09-core-features.md` §10 moves from Later to M4 with this wording, and `plan/09`'s milestone table row for `files`. The `go-conventions`, `add-module` and `privacy-data` skills gain the module rule: from M4 `deps.Files` is a `files.Store` scoped to the module; `Create`, `Delete` and `DeleteByRecord` run inside the module's `Write` transaction with the record; the module serves content from its own route after its permission check with `files.Open` and `files.Serve`, and never through a public route unless the file's level is `public`; the level and `Subject` declared at upload come from the module's spec; `DeletePerson` deletes every file whose subject is the person; tests use `filestest.NewFake(Manifest.Name)`.
