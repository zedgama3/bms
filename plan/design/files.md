# Files

- **Status:** Implemented (2026-10-20); Approved (2026-10-20, D44; redesigned 2026-10-20 per D43: no file contents in the database; a small S3-like object store the core and every feature use)
- **Amended 2026-10-28 (D52(e)):** the lifecycle tests (stage, attach, open, discard, reap, sweep) also run against the disk store on `t.TempDir()`, not only `objectstest.Mem`.
- **Package:** internal/core/objects (the object store contract: the simplified S3), internal/core/objects/disk, internal/core/objects/s3 (its implementations), internal/core/objects/objectstest; internal/core/files (attachments: catalogue, intake, metadata, serving, person data, hygiene), internal/core/files/postgres (the metadata rows and migrations; no bytes), internal/core/files/filesettings (the setting declarations), internal/core/files/filestest   **UI:** none of its own (the shell pieces are proposed to `web-shell`)
- **Amendment 2026-10-20 (architect, after D44; folding four gaps found while building):** decision 25 (`Serve` sets no `Cache-Control`; the shell's `no-store` stands; Proposed change 1(b) withdrawn; Q-FILES11, answered no, D45), decision 26 (`files.quota_bytes` is site-wide), decision 27 (`ExportPerson`'s `uploaded` entries omit `name` unless the file has no subject and is `public` or `internal`). No interface change.
- **Amendment 2026-10-21 (D47, owner approval of the settings logo amendment; settings.md Proposed change 16):** two contract lines, no code change: `Stage` returns a classified `*errs.Error` from `Upload.Body` unchanged (settings' `MaxLogoBytes` counting reader relies on it); `ByRecord` may be called inside a `Read` transaction (no audit, no store I/O; settings reads the logo in its snapshot load). "Used by" names `settings` as the consumer of `LogoRecordKind`, `Brand`, `ReadUpload`, `Serve`.
- **Amendment 2026-10-28 (approved, owner, D53, demo.md Proposed change 9):** note only, no interface change: `app` may wrap `PolicySource` to lower the limits (demo mode).
- **Depends on specs:** `model`, `lib-errs`, `lib-core`, `lib-secret`, `config` (through `app`), `db`, `audit` (the `Recorder`, `Ref`), `authz` (`ActorFunc` only), `settings` (`Decl`, a scoped `Reader` through `app`), `telemetry`, `api` (`httpx`, the per-route body cap and timeout proposed below), `module-contract` (`Deps.Files`), `people` (a `Source` for export and deletion), `jobs` (Approved: two scheduled jobs `app` registers), `app` (part 10)   **Used by:** `settings` (the regional logo: `LogoRecordKind`, `Brand`, `ReadUpload`, `Serve`), `volunteers` (certificate copies for qualifications, 09 §5), later `incidents` (photos and documents on an incident), `afterreports` (photos), any module that attaches a file to a record; `app` (the object store for any later core need for bytes)

## Summary

**Purpose.** Keep the bytes a module attaches to one of its records, with the record's permissions deciding who may read them. Two packages. `internal/core/objects` is the **object store**: a simplified S3 (a bucket, a key, Put/Get/Head/Delete/List, a SHA-256 verified at Put) that never lives in the database (D43), implemented on local disk (the default) and on any S3-compatible store; imports `model`, `lib`, `core/telemetry`. `internal/core/files` is the **attachment service** on top of it: the metadata row, ownership, levels, audit, intake, safe serving, export and deletion; imports `model`, `lib`, `core/objects`, `core/db`, `core/audit`, `core/authz` (`ActorFunc`), `core/telemetry`. Neither imports `core/identity`, `core/settings`, `core/module`, `core/jobs`, `api`, `app`, `config` or a module. Files owns no routes, screens or permissions: the owning module (or core part) serves a file from its own route after its own permission check; the stores never decide who may read.

**Exported (`objects`).** `Bucket` (`[a-z][a-z0-9]{0,31}`; an owner's name; `Validate`), `Key` (≤ 255 bytes of `[A-Za-z0-9._/-]`, no empty, `.` or `..` segment; `Validate`), `Kind` (`Disk`, `S3`; `ParseKind`, `String`), `Info{Bucket, Key, Size int64, SHA256 [32]byte, ContentType string, Modified time.Time}`, `Entry{Key, Size, Modified}`, `PutInput{Bucket, Key, Size, SHA256, ContentType, Body io.Reader}` (+`Validate`), `Object{Info; Body io.ReadSeekCloser}`. `Store`: `Put(ctx, PutInput) (Info, error)` (a key that exists is `Conflict`; a size or checksum mismatch is `Invalid` and nothing is kept), `Get(ctx, Bucket, Key) (Object, error)`, `Head(ctx, Bucket, Key) (Info, error)`, `Delete(ctx, Bucket, Key) error` (idempotent), `List(ctx, Bucket, prefix string, func(Entry) error) error`, `Ping(ctx) error`, `Kind() Kind`. Sentinels `ErrMissing` (`Unavailable`), `ErrPublic` (`Invalid`: the bucket answers an anonymous read). `disk.New(dir) (*Store, error)`; `s3.New(s3.Config) (*Store, error)` with `Config{Endpoint, Bucket, Region, Prefix string, PathStyle bool, AccessKey, SecretKey secret.Secret, Timeout time.Duration}`; `objectstest.NewMem() *Mem`, `objectstest.Conformance(t, func(t) objects.Store)`. Constants `MaxKeyBytes = 255`, `MaxBucketBytes = 32`, `MaxContentTypeBytes`, `ProbeKey Key = ".bms-probe"`; `s3.DefaultRegion`, `s3.DefaultTimeout`, `s3.MaxTimeout`; `files.Accept.Has`; `filesettings` `Area`, `Key*`, `Default*`, `Min*`, `Max*`. The disk store refuses keys ending `.meta` or `.part` (its own sidecar files).

**Exported (`files`).**
- `FileID` (= `model.ID[...]`); `Kind` (`PNG`, `JPEG`, `GIF`, `WebP`, `PDF`; `String`, `ContentType`, `Extension`, `Inline`); `Accept []Kind` with `Images`, `Documents` (PDF), `Brand` (PNG, JPEG, WebP).
- `File{ID, Record audit.Ref, Subject, UploadedBy model.PersonID, Level model.Sensitivity, Name, ContentType string, Size int64, SHA256 [32]byte, Stripped bool, CreatedAt}` (+`Validate`, `LogValue` = ID only). `Upload{Name string, Accept Accept, KeepMetadata bool, Body io.Reader}` (+`Validate`): the bytes. `Staged{Bucket objects.Bucket, Key objects.Key, Name, ContentType string, Size int64, SHA256 [32]byte, Stripped bool, StagedAt time.Time}`: an object put, not yet attached; no row exists. `Attachment{Record audit.Ref, Subject model.PersonID, Level model.Sensitivity, Replace bool}` (+`Validate`): the record. `Content` (= `io.ReadSeekCloser`, bound to `Open`'s ctx). `Multipart{Fields map[string]string, Name, DeclaredType string, Body io.Reader}`. `Policy{MaxBytes, QuotaBytes int64, MaxPerRecord int}` (+`Validate`), `DefaultPolicy()`; `PolicySource{Policy(ctx) (Policy, error)}`. `Deletion{Deleted, Anonymized int}`; `Swept{Tombstones, Orphans, TempFiles int}`; `Verified{Checked, Missing, Mismatched, Tombstones int}`.
- `Store` (= `module.Deps.Files`, scoped to one owner by `(*Service).Scoped(owner)`): `Stage(ctx, Upload) (Staged, error)` (outside any transaction; a classified `Body` read error is returned unchanged, D47), `Attach(ctx, Staged, Attachment) (File, error)` (inside the caller's `Write` transaction), `Discard(ctx, Staged) error`, `Get(ctx, FileID) (File, error)`, `ByRecord(ctx, audit.Ref) ([]File, error)` (usable inside a `Read` transaction, D47), `Open(ctx, FileID) (File, Content, error)`, `Delete(ctx, FileID) error`, `DeleteByRecord(ctx, audit.Ref) (int, error)`.
- Package functions: `ReadUpload(r *http.Request, field string) (*Multipart, error)`; `Serve(w http.ResponseWriter, r *http.Request, f File, c Content)` (never fails; headers under Behavior); `Detect(head []byte) (Kind, bool)`.
- `Options{DB, Clock, Logger, Tracer, Meter, Audit audit.Recorder (scoped "files"), Actor authz.ActorFunc, Objects objects.Store, TempDir string, Policy PolicySource (nil = DefaultPolicy)}` (+`Validate`); `New(Options) (*Service, error)` (no I/O; works against `db.None()`). `*Service` (safe for concurrent use; nothing to close): `Scoped(owner) (Store, error)`, `Start(ctx) error` (the probe; fails only on `ErrPublic`), `Ping(ctx) error`, `Reap(ctx) (Swept, error)` (hourly job), `SweepOrphans(ctx) (Swept, error)` (daily job), `Verify(ctx) (Verified, error)` (`bms files verify`), `ExportPerson(ctx, model.PersonID) (json.RawMessage, error)`, `DeletePerson(ctx, model.PersonID) (Deletion, error)`.
- Constants: `MaxNameBytes = 255`, `MaxFields = 32`, `MaxFieldBytes = 4096`, `SniffBytes = 512`, `UploadCeiling = 1 << 30` (the route body cap), `UploadTimeout = 10 * time.Minute`, `ServeTimeout = 10 * time.Minute`, `OrphanAge = 24 * time.Hour`, `MaxFilesPerRecordCeiling = 200`, `JobReap = "files.reap"`, `ReapEvery = time.Hour`, `JobSweepOrphans = "files.sweep-orphans"`, `SweepEvery = 24 * time.Hour`, `JobTimeout = 30 * time.Minute`, `LogoRecordKind = "settings.logo"`.
- Settings (core area `files`, `filesettings.Settings []settings.Decl`): `files.max_bytes` (Int, default 26214400, 1048576..1073741824), `files.quota_bytes` (Int, default 0 = unlimited, 0..), `files.max_per_record` (Int, default 20, 1..200). Config (Proposed change 3): `files.store` (`disk|s3`, default `disk`), `files.dir` (default `<data.dir>/files`), `files.s3_*`. Audit actions `files.file.created|deleted|read`, `files.sweep.ran`, `files.verify.ran`. `postgres.Migrations fs.FS`; `Set = db.Set{Name: "files", Schema: "core"}`. No permissions, routes, signals or screens.
- `filestest.NewFake(owner) *Fake` (an in-memory `Store` validating like the real one: `Files()`, `Staged()`, `Fail(err)`).

**Key decisions.**
1 one `core.file` row per attachment, immutable (replace = delete + create); the owning record is an `audit.Ref` whose area must equal the scoped owner, so a module can create, list, open and delete only its own files (another owner's file is `NotFound`).
2 (D43) **bytes never live in the database**: they live in an object store behind `objects.Store`, a simplified S3; what stays in PostgreSQL is one small metadata row per file (ownership, record, subject, level, name, type, size, checksum, where the object is), because ownership scoping, quotas, listing by record, audit, export and deletion are joins with the owner's transaction, and the row is what makes "every file belongs to a record" checkable.
3 the object store is an **in-process Go component** with two implementations, `disk` (the default: `<files.dir>/store`, plain files, one volume with the rest of `data.dir`) and `s3` (MinIO, Garage, SeaweedFS, Ceph RGW, AWS), not a second process of our own and not a built-in S3-protocol server: one container for a volunteer operator (Phase 1), no second secret, port or health check, and no new network surface to lock down; whoever wants the files on their own host runs a mature OSI object store through `s3` instead of one we would write (alternatives under Behavior, "Why in-process").
4 buckets are logical: one per owner (the module name or core area), a directory on disk and a key prefix in one real S3 bucket, so an operator creates one bucket, never one per module; there is no bucket-management API.
5 a key is the file ID: keys carry no name and no personal data, and the layout is the same in every store.
6 an upload is **two calls**: `Stage` (outside any transaction: spool, sniff, strip, hash, `Put`) then `Attach` (inside the owner's `Write` transaction with its record: the row and the audit entry), so a database connection is held only for the row, never for a slow phone upload or the `Put`; nothing "pending" exists in the database (a `Staged` is a value in the handler's hands), and an object never attached is an orphan the sweep removes after `OrphanAge`.
7 the stored content type is sniffed from the bytes, never the declared one; a sniffed type outside `Upload.Accept` is `Invalid type`; the stored name is the client's base name, cleaned, with the extension forced to the sniffed kind.
8 only images and PDF are accepted in M4; no SVG (Q-FILES3) and no office formats, because nothing needs them yet and each needs its own sanitiser.
9 metadata is stripped from JPEG, PNG, GIF and WebP at upload (GPS, timestamps, device, comments; orientation kept) unless the module sets `KeepMetadata` (Q-FILES5).
10 files audits itself: `files.file.created` and `files.file.deleted` in the caller's transaction, and `files.file.read` on `Open` for a `sensitive` file always and a `personal` file when the actor is not the subject (no audit, no read); the module audits its record, not the bytes.
11 `Open` of a `personal` or `sensitive` file with no actor is `Unauthenticated` (fail closed); `public` and `internal` need none.
12 serving goes through `Serve`: sniffed `Content-Type`, `nosniff`, `Content-Disposition: inline` for images and PDF, `Content-Security-Policy: default-src 'none'`, `Cache-Control` by level, `ETag` = SHA-256, ranges and conditional requests through `http.ServeContent`, a write deadline of `ServeTimeout`; no presigned or direct store URLs ever: every byte passes the owner's permission check.
13 limits are a regional's settings checked while streaming; the route body cap is the constant `UploadCeiling`, applied by the api shell per route (Proposed change 1).
14 every upload is spooled to `<files.dir>/tmp` first (bounded memory, sniff, strip, hash, known length and checksum for `Put`).
15 a file's lifetime is its record's: no `files.retention*` key; the module's retention job deletes the record and calls `DeleteByRecord`.
16 **deletion without a shared transaction**: deleting a row inserts a `core.file_tombstone` in the same transaction; `AfterCommit` deletes the object and then the tombstone; whatever fails is retried by `files.reap` (hourly) until the object is gone, so "deleted means deleted" has a checkable state (no tombstone outstanding), and a daily `files.sweep-orphans` walks the store for objects with neither row nor tombstone (a crash between `Put` and `Attach`) and temp files older than `OrphanAge`.
17 the other orphan direction, a row whose object is missing, is **never deleted automatically** (on a read-only copy it is a sync lag, after a restore it is the operator's problem): `Open` answers `Unavailable` wrapping `objects.ErrMissing`, and `bms files verify` reports every such row by ID.
18 export and deletion: files is a `people.Source` named `files` after the modules: `ExportPerson` returns every file whose subject is the person with its content (base64) and the metadata of files they uploaded about others (Q-FILES6); `DeletePerson` anonymises `uploaded_by` and refuses with `Conflict in_use` when a module left a file whose subject is the person.
19 the logo stays a settings feature (Q-FILES7): settings owns `GET|PUT|DELETE /api/settings/logo` and stores it under `Ref{Kind: "settings.logo", ID: fixed}` at level `public`; files only stores and serves it (Proposed change 6).
20 a store that answers an anonymous read (`ErrPublic`, found by the start-up probe) fails `Start`: every certificate in it is exposed and the process must not add more; an unreachable store does not fail `Start` (reads of metadata and the rest of bms keep working; file operations answer `503`).
21 modules get `files.Store`, never `objects.Store`: without the row nothing enforces ownership, level, audit, export or deletion; a module that needs bytes without a record makes the bytes a record of its own; `objects.Store` is the core's and `app`'s.
22 the sensitivity level of a file is declared by the owner at `Attach` from its spec today; the forthcoming field-level privacy-tags abstraction (D43) is expected to let the owner derive it from the record field's tag instead, without changing this interface.
23 no virus scanning, thumbnails, resizing, deduplication or sharing links in M4.
24 regional differences are the three settings and the store choice; no code path differs per regional.
25 (2026-10-20) `Serve` sets no `Cache-Control`: every file response on `/api/` carries the shell's `no-store` (api decision 12 as settled in D40), including the public logo; a certificate or an incident photo is viewed on shared devices and over venue proxies, and the logo is one small fetch per app boot, the cost the owner already accepted for `/api/settings/public`; `ETag` and `304` stay (api F58); the owner declined a browser cache for the logo (Q-FILES11, answered no, D45); `Serve` never sets the header.
26 (2026-10-20) `files.quota_bytes` is **site-wide**: the sum of `size_bytes` over every `core.file` row of every owner, because it bounds the one volume or bucket the operator pays for; fairness between owners is `files.max_per_record`, and a per-owner quota is a later setting if a regional asks.
27 (2026-10-20) in `ExportPerson`, an `uploaded` entry carries `name` only when the file has no subject and its level is `public` or `internal`; otherwise the field is absent, because a file name chosen by the uploader describes the subject (`jane-ics100.pdf`) and the name's sensitivity follows the file's level (Data).

**Errs kinds callers handle.** `Stage`: `Internal` (called inside a transaction), `Invalid` with field codes `name` `too_long {max}|control_chars`, `file` `required|too_large {max}|type|malformed`, `accept` `required`; `Unavailable` (store unreachable, temp dir unwritable, checksum refused by the store); `Timeout`, `Canceled`. `Attach`: `Internal` (not inside a `Write` transaction; a `Staged` used twice), `Invalid` with `record` `required|malformed|scope`, `level` `one_of`, `subject` `unknown`, `staged` `expired|foreign|malformed` (the key is not a file ID); `Conflict` with codes `quota {max}`, `too_many {max}`; `Unavailable` wrapping `db.ErrReadOnly`; the `Recorder`'s kind. `Discard`: `Unavailable`. `Get`/`ByRecord`: `NotFound` (unknown ID or another owner's), `Invalid` (ref), store kinds.
`Open`: `NotFound`, `Unauthenticated` (decision 11), the `Recorder`'s kind, `Unavailable` wrapping `objects.ErrMissing`, `Unavailable` (object held by a store this process is not configured for; store unreachable), store kinds. `Delete`/`DeleteByRecord`: `NotFound` (`Delete` only), `Internal` (no `Write` transaction), `Unavailable` wrapping `db.ErrReadOnly`, store kinds. `ReadUpload`: `Invalid` wrapping `httpx.ErrUnsupportedMediaType`, `Invalid` (`file` `required|order`, `fields` `too_many {max}`, `<field>` `too_long {max}`), `*http.MaxBytesError` unchanged, `Timeout`, `Canceled`. `ExportPerson`: `Invalid`, `Unavailable` wrapping `objects.ErrMissing`, store kinds. `DeletePerson`: `Invalid`, `Internal`, `Conflict in_use`, `Unavailable` wrapping `db.ErrReadOnly`. `Start`: `Unavailable` wrapping `objects.ErrPublic` (the sentinel is kept; `Ping` stays `Invalid`).
`Reap`/`SweepOrphans`/`Verify`: `Unavailable` wrapping `db.ErrReadOnly` (the two jobs), `Unavailable`. `Ping`: `Unavailable`, `Invalid` wrapping `ErrPublic`, `Timeout`. `Scoped`: `Invalid`. `New`/`Options.Validate`: `Invalid` with fields `files.db`, `files.clock`, `files.logger`, `files.tracer`, `files.audit`, `files.actor`, `files.objects`, `files.temp_dir`. `objects.Store`: `Put` `Conflict`, `Invalid` (`size`, `checksum`, `bucket`, `key`), `Unavailable`; `Get`/`Head` `Unavailable` wrapping `ErrMissing`; `Ping` `Unavailable`, `Invalid` wrapping `ErrPublic`. Sentinels: `objects.ErrMissing`, `objects.ErrPublic` (check `errors.Is` before the kind).

**Owns.** Tables `core.file` (metadata; `name` is `personal` when the file is; the rest IDs, tokens and sizes) and `core.file_tombstone` (store, bucket, key, no personal data); no table holds bytes. Settings area `files` (three keys); config keys `files.store`, `files.dir`, `files.s3_endpoint`, `files.s3_bucket`, `files.s3_region`, `files.s3_prefix`, `files.s3_path_style`, `files.s3_access_key`, `files.s3_secret_key` (proposed); the directory `<files.dir>/` (`tmp/`, `store/<bucket>/...`); audit area `files`; jobs `files.reap`, `files.sweep-orphans` (registered by `app`); command `bms files verify` (proposed); spans `files.stage`, `files.attach`, `files.open`, `files.delete`, `files.reap`, `files.sweep`, `files.verify`, `objects.<op>` (Client); metrics `bms.files.uploads{outcome}`, `bms.files.reads{outcome}`, `bms.files.swept{what}`, `bms.files.tombstones` (gauge), `bms.objects.failures{op}`. No permissions, routes, signals or screens.

**Expects of dependencies.** `app`: part 10 (`build`: the `objects.Store` from `cfg.Files`, `files.New`, `audit.Scoped("files")`, the actor closure, `PolicySource` over a `files` `Reader` (in demo mode wrapped by `core/demo` to lower the limits, D53); `provision`: `a.deps.Files = svc.Scoped(m.Name)`; `link`: `people.Source{Name: "files"}` after the modules, a lazy `files.Store` scoped `settings` for the logo; `start`: `svc.Start`; the two `jobs.Decl`s through `runner.Scoped("files")`; `migrate` creates `<files.dir>/tmp` and `<files.dir>/store` (0700); `check` prints `files: <store> ok` from `Ping`; `bms files verify`). `api`: `Route.MaxBodyBytes` and `Route.Timeout` honoured; `Cache-Control: no-store` on every file response (`Serve` sets none, decision 25); multipart and binary fragments in OpenAPI. `module-contract`: `Deps.Files files.Store`, the two `Route` fields, `moduletest` supplying `filestest.NewFake`. `db`: `Transact`, `TxMode`, `AfterCommit`, `Wrap`, `ErrReadOnly`, `ReadOnly`, `Set`; `files` in the core set order after `jobs`. `audit`: a `Recorder` scoped `files`; `Ref`; `Record` outside a transaction spools on a read-only copy. `authz`: `ActorFunc`. `settings`: `filesettings.Settings` as core area `files`; the logo (Proposed change 6). `people`: `Source` after the modules; `core.person` for the two foreign keys. `jobs`: `Decl{Name, Every, Timeout, Handler}`, `Every` ≥ 24 h allowed (`MaxEvery` 168 h), jobs run on the writing copy only, `InJob` for the system actor. `telemetry`: typed attributes; the metric rows. `web-shell`: `upload` in `@/api`, `ui/FileField`, `ui/Attachments`, the header logo, field codes (Proposed change 7). `config`: the `files.*` keys (Proposed change 3). `module-volunteers` (Approved): `UploadCertificate` becomes `Stage` then `Attach` (Proposed change 13).

**Open questions:** none. Q-FILES11 (should the browser cache the logo?) was answered no (owner, 2026-10-21, D45): decision 25 stands and the logo is not browser-cached. Q-FILES1 and Q-FILES-SHAPE (the object store is built into bms as `objects.Store`, disk by default) were answered by D43; Q-FILES2 to Q-FILES10 were settled with the recommendations (owner, 2026-10-20, D44): both stores ship in M4, no SVG logo, the limits' defaults, metadata stripped by default, the export carries the bytes inline, the logo lives with `settings`, the operator syncs a copy's objects, modules get `files.Store` only, a public bucket fails `Start`. **Proposed changes elsewhere:** 14, listed at the end; all applied 2026-10-20 (D44) except 1(b), withdrawn by decision 25 (api.md's Proposed change 18 note should say so), and the `docs/operating.md` and `deploy/` parts of 8 and 11 (targets not written).

**Amended 2026-10-28 (D52(e)).** Lifecycle tests also run against the disk store, not only `Mem`.

## Purpose

Keep the bytes a module attaches to one of its records, with the record's permissions deciding who may read them.

## Responsibilities

- Keep bytes outside the database, under a bucket and a key, behind one object-store contract (`objects.Store`) with a local-disk and an S3-compatible implementation that pass one conformance suite.
- Accept an upload from a module's or core part's handler: stream it to a temp file under a size limit, sniff its type against what the caller accepts, strip image metadata, hash it, and put it in the object store (`Stage`); then bind it to the owner's record with one metadata row inside the owner's transaction (`Attach`).
- Keep the metadata (`core.file`): the owning record, the subject person, the uploader, the sensitivity level the owner declared, the cleaned name, the sniffed type, size, SHA-256, and which store, bucket and key hold the bytes.
- Hand the bytes back to the owning module (`Open`) and audit that read when the level requires it.
- Write the safe HTTP response for a file (`Serve`): correct type, no sniffing, no script execution, right caching, ranges.
- Delete a file, or every file of a record, inside the caller's transaction, and remove the bytes afterwards with a tombstone that guarantees the removal is retried until done.
- Export and delete what it holds about a person, as a `people.Source`.
- Reap tombstones (hourly), sweep orphaned objects and temp files (daily), verify rows against objects on demand, and report storage health to `bms check`.
- Declare the regional's limits as settings.

## Not responsible for

- Deciding who may see a file: the owning module checks its own permission on its own route before calling `Open` (09 §10, "under the record's permissions"); `authz` holds the rules. The object store never decides anything: it has no notion of a person.
- HTTP routes and screens for attachments: the owning module (certificate on a qualification, photos on an incident) and `settings` (the logo). Files exports `ReadUpload` and `Serve` so those handlers stay thin.
- The body cap and request deadline on an upload or download route: the `api` shell, from `Route.MaxBodyBytes` and `Route.Timeout` (Proposed change 1).
- Retention of a file: the owning module's retention job deletes the record and its files (settings decision 11); files keeps no retention key.
- Running an object store over the network: MinIO, Garage, SeaweedFS, Ceph RGW or a cloud provider do that; bms speaks to them through `objects/s3`. bms ships no S3 server and no file daemon of its own (decision 3).
- Keeping an on-site or read-only copy's objects in step with the writer's: the operator's sync in Phase 1 (`docs/operating.md`, Proposed change 11) and the later `switchover` spec (Q-FILES8). Files makes the gap visible (`ErrMissing`, `Verify`) and never hides it.
- Backups: the operator's; `docs/operating.md` says what to back up per store and in what order (Proposed change 11).
- Virus or malware scanning: out of scope. Every accepted type is an image or a PDF served with `nosniff` and a restrictive CSP, and never executed by the server. A scanner seam (ClamAV is OSI) can be added later as an optional core implementation without changing `Store`.
- Thumbnails, resizing, image conversion, HEIC: later sub-abstractions when a consumer needs them. A phone asked for `image/*` uploads JPEG.
- Deduplication: two uploads of the same bytes are two files; each belongs to one record and dies with it.
- Sharing links, signed URLs, public galleries: later, if a module needs them; never a URL that bypasses the owner's permission check.
- The web upload component and the API client's multipart support: `web-shell` (Proposed change 7).
- Scheduling the two jobs: `jobs`; registering them: `app` (as it registers `audit.purge`, jobs decision 11).
- Deriving a file's level from a tagged record field: the forthcoming privacy-tags abstraction (D43); files takes the level it is given.

## Sub-abstractions

| Name | Purpose (one line) | Notes |
|---|---|---|
| Object store | keep bytes under a bucket and a key, outside the database, behind one seam any store can implement | `objects.Store`; `disk`, `s3`; `objectstest.Conformance`; the simplified S3 |
| Catalogue | say which kinds of file exist, how each is recognised from its bytes, and how it is served | `Kind`, `Accept`, `Detect`; table under Types |
| Intake | turn a request body into a verified, stripped, hashed object | `ReadUpload`, `Stage`, the spool, the size counter, `imgmeta` |
| Metadata | bind one row to one object and enforce ownership, levels and audit | `core.file`, `Attach`, the `Store` reads and deletes, `Scoped` |
| Serving | write the safe HTTP response for a file | `Serve` |
| Person data | export and delete what files holds about a person | `ExportPerson`, `DeletePerson`, the `people.Source` |
| Hygiene | finish deletions, remove what no row refers to, report what no object backs | `core.file_tombstone`, `Reap`, `SweepOrphans`, `Verify`, `Ping`, `Start` |

## Interface

### The object store

```go
package objects // internal/core/objects

import (
	"context"
	"errors"
	"io"
	"time"
)

// Bucket is an owner's namespace: [a-z][a-z0-9]{0,31}. Implementations map it to a
// directory (disk) or a key prefix inside one real bucket (s3).
type Bucket string

func (b Bucket) Validate() error

// Key names one object inside a bucket: 1..MaxKeyBytes bytes of [A-Za-z0-9._/-], with no
// empty, "." or ".." segment. files uses the file ID.
type Key string

func (k Key) Validate() error

const (
	MaxKeyBytes    = 255
	MaxBucketBytes = 32
	// ProbeKey is the object Ping writes to prove the store is writable and then reads
	// anonymously to prove it is not public (s3 only; disk never checks it).
	ProbeKey Key = ".bms-probe"
)

// Kind names an implementation.
type Kind uint8

const (
	Disk Kind = iota + 1
	S3
)

func ParseKind(s string) (Kind, error) // Invalid unless "disk" or "s3"
func (k Kind) String() string

// Info is what the store knows about one object. SHA256 is the checksum verified at Put.
type Info struct {
	Bucket      Bucket
	Key         Key
	Size        int64
	SHA256      [32]byte
	ContentType string
	Modified    time.Time
}

// Entry is one List row: what a listing can give cheaply in every store.
type Entry struct {
	Key      Key
	Size     int64
	Modified time.Time
}

// PutInput is a complete object: the caller knows the size and the checksum up front
// (files spools first). Body is read exactly Size bytes; the caller owns it.
type PutInput struct {
	Bucket      Bucket
	Key         Key
	Size        int64
	SHA256      [32]byte
	ContentType string
	Body        io.Reader
}

func (p PutInput) Validate() error // everything but the bytes

// Object is Info plus the bytes, seekable for ranges. The caller closes Body.
type Object struct {
	Info
	Body io.ReadSeekCloser
}

// Store is the simplified S3 the core and app use. Every implementation passes
// objectstest.Conformance and is safe for concurrent use.
type Store interface {
	// Put stores exactly in.Size bytes under (Bucket, Key), verifying in.SHA256 as it goes:
	// a key that exists is Conflict; a short or long Body or a checksum mismatch is Invalid
	// and nothing is kept. Consumer: files.Stage.
	Put(ctx context.Context, in PutInput) (Info, error)
	// Get answers the object; a missing one is Unavailable wrapping ErrMissing.
	// Consumers: files.Open, files.ExportPerson.
	Get(ctx context.Context, b Bucket, k Key) (Object, error)
	// Head answers Info without the bytes; missing as Get. Consumer: files.Verify.
	Head(ctx context.Context, b Bucket, k Key) (Info, error)
	// Delete removes the object; a missing one is not an error. Consumers: files.Discard,
	// the AfterCommit of files.Delete, files.Reap, files.SweepOrphans.
	Delete(ctx context.Context, b Bucket, k Key) error
	// List calls fn for every object in b whose key starts with prefix, in no order; fn's
	// error stops the walk and is returned. Consumer: files.SweepOrphans.
	List(ctx context.Context, b Bucket, prefix string, fn func(Entry) error) error
	// Ping proves the store is usable: disk creates the root if needed and writes and
	// removes a temp file; s3 puts ProbeKey, reads it back anonymously (Invalid wrapping
	// ErrPublic when that succeeds) and deletes it. Consumers: files.Ping, files.Start.
	Ping(ctx context.Context) error
	Kind() Kind
}

// ErrMissing: no object under that key. Unavailable, because on a read-only copy the
// object may arrive with the next sync.
var ErrMissing = errors.New("objects: no such object")

// ErrPublic: the store answered an anonymous read of ProbeKey. Invalid: the bucket policy
// is wrong, and every object in it is exposed.
var ErrPublic = errors.New("objects: bucket is publicly readable")
```

```go
package disk // internal/core/objects/disk
// New answers a store rooted at dir (<files.dir>/store). Invalid when dir is empty or
// relative. No I/O: the root is created by bms migrate and checked by Ping.
func New(dir string) (*Store, error)

package s3 // internal/core/objects/s3
type Config struct {
	Endpoint  string        // host[:port]; https unless PathStyle dev endpoints say otherwise
	Bucket    string        // the one real bucket; logical buckets are prefixes inside it
	Region    string        // default "us-east-1" (what MinIO and Garage accept)
	Prefix    string        // "" or "a/b/"
	PathStyle bool          // default true: every self-hosted store uses it
	AccessKey secret.Secret
	SecretKey secret.Secret
	Timeout   time.Duration // per call; default 30 s
}
func (c Config) Validate() error
func New(c Config) (*Store, error) // builds the client; no network

package objectstest
func NewMem() *Mem                                                   // in-memory Store for unit tests
func Conformance(t *testing.T, factory func(t *testing.T) objects.Store) // the Store contract
```

### Attachments

```go
package files // internal/core/files

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/objects"
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
func (k Kind) Inline() bool        // true for every M4 kind (decision 12)
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

// Upload is the bytes a caller gives Stage. Body is read once, to the end; the caller owns it.
type Upload struct {
	Name         string // the client's file name; cleaned; "" becomes "file"
	Accept       Accept
	KeepMetadata bool // keep EXIF/XMP (decision 9); false for anything about people
	Body         io.Reader
}

func (u Upload) Validate() error // everything but Body and the bytes

// Staged is an object that Stage put and nobody has attached: a value, not a row. It is
// valid for the Store that made it, for OrphanAge; after that the sweep may remove the
// object. The handler defers Discard; Discard after Attach is a no-op.
type Staged struct {
	Bucket      objects.Bucket
	Key         objects.Key
	Name        string
	ContentType string
	Size        int64
	SHA256      [32]byte
	Stripped    bool
	StagedAt    time.Time
}

func (s Staged) Validate() error

// Attachment is the record a Staged object is bound to.
type Attachment struct {
	Record  audit.Ref
	Subject model.PersonID
	Level   model.Sensitivity
	Replace bool // delete the record's existing files in the same transaction
}

func (a Attachment) Validate() error

// Content is a file's bytes, seekable for ranges. It is bound to the ctx given to Open and
// must be closed before that ctx ends; Serve does not close it.
type Content = io.ReadSeekCloser

// Store is what a module gets as Deps.Files and a core part gets from Scoped: every method
// sees only the owner's files, and every object it writes goes to the owner's bucket.
// Consumers: volunteers (certificates), settings (the logo), later incidents and afterreports.
type Store interface {
	// Stage spools, sniffs, strips, hashes and puts the upload in the owner's bucket.
	// Called outside any transaction (Internal inside one). Consumers: every owner's upload
	// handler, before it opens its transaction. A read error from up.Body that is already a
	// classified *errs.Error is returned unchanged (settings' MaxLogoBytes reader relies on it).
	Stage(ctx context.Context, up Upload) (Staged, error)
	// Attach binds a Staged object to a record inside the caller's Write transaction,
	// inserting the row and recording files.file.created. Record.Kind's area must be the
	// owner. Consumers: every owner's upload use case.
	Attach(ctx context.Context, st Staged, a Attachment) (File, error)
	// Discard removes a Staged object that will not be attached; idempotent, a no-op once
	// attached. Consumers: every upload handler (deferred).
	Discard(ctx context.Context, st Staged) error
	// Get answers one file's metadata. Consumers: an owner showing one attachment
	// (volunteers: the certificate card).
	Get(ctx context.Context, id FileID) (File, error)
	// ByRecord lists a record's files, oldest first. Consumers: an owner listing attachments
	// (incidents, afterreports), settings finding the logo, an owner's ExportPerson. May be
	// called inside a Read transaction (no audit, no store I/O; settings reads the logo in
	// its snapshot load).
	ByRecord(ctx context.Context, rec audit.Ref) ([]File, error)
	// Open answers the metadata and the bytes, auditing the read when the level requires it
	// (decision 10). Called outside any transaction. Consumers: every owner's content route.
	Open(ctx context.Context, id FileID) (File, Content, error)
	// Delete removes one file inside the caller's Write transaction, writes its tombstone
	// and records files.file.deleted; the object goes after commit. Consumers: an owner
	// removing one attachment; settings removing the logo.
	Delete(ctx context.Context, id FileID) error
	// DeleteByRecord removes every file of a record (same rules). Consumers: an owner
	// deleting a record, its retention job, its DeletePerson.
	DeleteByRecord(ctx context.Context, rec audit.Ref) (int, error)
}

// Multipart is one parsed upload request: the text fields that came before the file part,
// and the file part as a stream. The file part must be last (Behavior).
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

// Policy is the regional's limits (the three settings).
type Policy struct {
	MaxBytes     int64 // per file
	QuotaBytes   int64 // total stored; 0 = unlimited
	MaxPerRecord int
}

func (p Policy) Validate() error
func DefaultPolicy() Policy // 25 MiB, 0, 20

// PolicySource reads the policy at use-case time (app: over a settings.Reader scoped files).
// app may wrap it to lower the limits (demo mode caps uploads at 2 MiB, D53); the interface
// does not change.
type PolicySource interface {
	Policy(ctx context.Context) (Policy, error)
}

type Deletion struct{ Deleted, Anonymized int }
type Swept struct{ Tombstones, Orphans, TempFiles int }
type Verified struct{ Checked, Missing, Mismatched, Tombstones int }

type Options struct {
	DB      db.DB
	Clock   clock.Clock
	Logger  *slog.Logger
	Tracer  telemetry.Tracer
	Meter   telemetry.Meter
	Audit   audit.Recorder // scoped "files"
	Actor   authz.ActorFunc
	Objects objects.Store
	TempDir string       // <files.dir>/tmp; created by bms migrate
	Policy  PolicySource // nil = DefaultPolicy
}

func (o Options) Validate() error
func New(o Options) (*Service, error)

type Service struct{ /* unexported */ }

// Scoped answers a Store that sees only owner's files and writes to bucket owner.
func (s *Service) Scoped(owner string) (Store, error)
// Start runs the store probe once (serve). Only a positive ErrPublic fails it (decision 20);
// an unreachable store is logged and left to Ping and the routes.
func (s *Service) Start(ctx context.Context) error
// Ping reports whether the temp dir and the object store are usable (bms check, Start).
func (s *Service) Ping(ctx context.Context) error
// Reap deletes the objects of outstanding tombstones and then the tombstones. The hourly job.
func (s *Service) Reap(ctx context.Context) (Swept, error)
// SweepOrphans removes temp files and objects older than OrphanAge that no row and no
// tombstone refers to. The daily job.
func (s *Service) SweepOrphans(ctx context.Context) (Swept, error)
// Verify heads every row's object and reports rows whose object is missing or whose
// checksum differs, and tombstones outstanding; it deletes nothing. bms files verify.
func (s *Service) Verify(ctx context.Context) (Verified, error)
// ExportPerson: the people.Source export (shape under Behavior). No transaction, no audit.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
// DeletePerson: the people.Source deletion, inside people's Write transaction.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (Deletion, error)
```

```go
package postgres // internal/core/files/postgres
var Migrations fs.FS // core.file, core.file_tombstone; no bytes

package filestest
func NewFake(owner string) *Fake     // an in-memory Store with the real validation, ownership and staging rules
func (f *Fake) Files() []files.File  // what was attached and not deleted
func (f *Fake) Staged() []files.Staged // staged and neither attached nor discarded
func (f *Fake) Fail(err error)       // every later call answers err
```

### Method contracts

| Method | Consumer | Pre | Post |
|---|---|---|---|
| `objects.Put` | `files.Stage` | `PutInput.Validate` nil; `Body` yields exactly `Size` bytes | the object exists with `Info` as given, or nothing exists; the checksum was verified by the store (`s3`: `x-amz-checksum-sha256`; a store that ignores it is hashed client-side and the object deleted on mismatch) |
| `objects.Get`/`Head` | `files.Open`, `ExportPerson` / `Verify` | — | the object, or `Unavailable` wrapping `ErrMissing` |
| `objects.Delete` | `Discard`, `Delete`'s `AfterCommit`, `Reap`, `SweepOrphans` | — | the key is gone; nil when it already was |
| `objects.List` | `SweepOrphans` | — | every entry under the prefix, once each; the walk honours ctx |
| `objects.Ping` | `files.Ping`, `files.Start` | — | nil when writable and (s3) not anonymously readable |
| `Stage` | every owner's upload handler | outside any transaction (`Internal` inside); `Upload.Validate` nil; `Body` non-empty | temp spool, sniff, strip, hash; `MaxBytes` enforced; quota pre-checked (advisory); object put under `(owner, new id)`; temp file removed; `Staged` returned; no row |
| `Attach` | every owner's upload use case | inside a `Write` transaction; `st` from this owner's `Stage` within `OrphanAge`; `Attachment.Validate` nil; `Record.Kind`'s area = owner; `Subject` zero or an existing person | `Replace` deletes the record's files first; `MaxPerRecord` and `QuotaBytes` enforced; row inserted; `files.file.created` recorded in the transaction; `File` returned |
| `Discard` | upload handlers (deferred) | — | the object is gone unless a row refers to it; idempotent |
| `Get` | an owner showing one attachment | — | metadata, or `NotFound` for an unknown ID or another owner's |
| `ByRecord` | an owner listing a record's files; settings; exports | `rec` valid and in the owner's area | files oldest first; empty list when none; a ref in another area is `Invalid scope`; usable inside a `Read` transaction (no audit, no store I/O) |
| `Open` | an owner's content route | outside any transaction; for `personal`/`sensitive` an actor | `files.file.read` recorded when due (before the content is returned); `Content` open; the caller closes it |
| `Delete` | an owner removing an attachment | inside a `Write` transaction | row deleted, tombstone inserted, `files.file.deleted` recorded, all in the transaction; after commit the object and then the tombstone are deleted (a failure is logged, counted, and left to `Reap`) |
| `DeleteByRecord` | an owner deleting a record, its retention job, its `DeletePerson` | inside a `Write` transaction | as `Delete` for each; count returned; zero is fine |
| `ReadUpload` | upload handlers | `Content-Type: multipart/form-data` with a boundary | text parts before the file collected; the file part streamed; a text part after the file is `Invalid file order` |
| `Serve` | content routes | `f` and `c` from `Open` | headers under Behavior; `http.ServeContent` |
| `Scoped` | `app` (per module and for settings) | owner is a module name or core area | a `Store` whose bucket is the owner |
| `Start` | `app` (`serve`) | — | the probe ran; `Unavailable` wrapping `ErrPublic` only on a positive finding |
| `Ping` | `app` (`bms check`, `Start`) | — | nil when the temp dir is writable and `objects.Ping` is nil |
| `Reap` | `jobs` (hourly, via `app`); `bms files verify` does not run it | writing copy | every tombstone's object deleted and the tombstone removed, or its `attempts` incremented; counts in `files.sweep.ran` |
| `SweepOrphans` | `jobs` (daily, via `app`) | writing copy | temp files and objects older than `OrphanAge` with no row and no tombstone deleted; counts recorded |
| `Verify` | `app` (`bms files verify`) | — | every row headed; IDs of missing and mismatched rows logged; counts returned and recorded in `files.verify.ran`; nothing changed |
| `ExportPerson` | `people` (`Source`) | — | the JSON under Behavior |
| `DeletePerson` | `people` (`Source`, inside its transaction) | inside a `Write` transaction | `uploaded_by` cleared on the person's uploads (counted `Anonymized`); any file with `subject = person` is `Conflict in_use` (the owner forgot to delete it) |

## Types

Defined here: `objects.Bucket`, `Key`, `Kind`, `Info`, `Entry`, `PutInput`, `Object`; `files.FileID`, `Kind`, `Accept`, `File`, `Upload`, `Staged`, `Attachment`, `Content`, `Multipart`, `Policy`, `Deletion`, `Swept`, `Verified`. Uses `model.ID`, `model.PersonID`, `model.Sensitivity`, `audit.Ref`, `authz.ActorFunc`, `secret.Secret`.

The catalogue (decisions 7, 8, 9, 12):

| Kind | Sniff (on the first 512 bytes) | Content-Type | Extension | Inline | Metadata stripped |
|---|---|---|---|---|---|
| `PNG` | `\x89PNG\r\n\x1a\n` | `image/png` | `.png` | yes | `eXIf`, `tEXt`, `zTXt`, `iTXt`, `tIME` chunks dropped |
| `JPEG` | `\xff\xd8\xff` | `image/jpeg` | `.jpg` | yes | every APPn and COM segment dropped except APP0 (JFIF), APP2 (ICC) and APP14 (Adobe); when the original carried an Exif Orientation other than 1, a minimal APP1 holding only that tag is written back |
| `GIF` | `GIF87a` / `GIF89a` | `image/gif` | `.gif` | yes | comment extensions dropped |
| `WebP` | `RIFF....WEBP` | `image/webp` | `.webp` | yes | `EXIF` and `XMP ` chunks dropped |
| `PDF` | `%PDF-` | `application/pdf` | `.pdf` | yes | none: a document's own metadata (author, title) is part of the document the person chose to upload |

`http.DetectContentType` recognises the four image kinds; PDF is a prefix check. Anything else, including HTML, SVG, XML, text, zip-based formats and HEIC, is `Invalid file type`, whatever the declared type or extension said.

## Data

**What stays in PostgreSQL and why.** Only metadata: one row per attachment and one row per pending object deletion. The row is what ties a file to a record inside the owner's transaction, what the owner scope filters on, what quotas and per-record counts are computed from, what carries the level and the subject for audit, export and deletion, and what a read-only copy needs to show that an attachment exists even before its bytes arrive. Bytes never: no `bytea` column holds file content (D43).

Migration set `db.Set{Name: "files", Schema: "core"}`, `postgres.Migrations`. `people`'s set runs first (`core.person`).

`core.file`

| Column | Type | Sensitivity | Notes |
|---|---|---|---|
| `id` | uuid pk | — | `FileID`; also the object key |
| `owner` | text not null | internal | the area of `record_kind`; also the bucket; indexed with `record_kind, record_id` |
| `record_kind` | text not null | internal | `audit.Ref.Kind` (`<area>.<ident>`) |
| `record_id` | uuid not null | internal | `audit.Ref.ID` |
| `subject_person_id` | uuid null, fk `core.person(id)` on delete restrict | internal (an ID) | whom the file is about; indexed |
| `uploaded_by` | uuid null, fk `core.person(id)` on delete restrict | internal (an ID) | cleared by `DeletePerson`; indexed |
| `level` | smallint not null | — | `model.Sensitivity`; check 1..4; set by the owner (decision 22: the privacy-tags abstraction may later supply it) |
| `name` | text not null | follows `level` (at least internal; `personal` or `sensitive` with the file) | ≤ 255 bytes, no control characters, no `/` or `\` |
| `content_type` | text not null | internal | from the catalogue |
| `size_bytes` | bigint not null | internal | > 0 |
| `sha256` | bytea not null | internal | 32 bytes; the `ETag`; what `Verify` compares |
| `store` | text not null | internal | `disk` or `s3`: the `objects.Kind` that holds the object |
| `bucket` | text not null | internal | = `owner` in M4; kept as a column so a later move can rename |
| `object_key` | text not null | internal | = `id` as text in M4; unique with `(store, bucket)` |
| `stripped` | boolean not null | — | |
| `created_at` | timestamptz not null | internal | |

Indexes: `(owner, record_kind, record_id, created_at)`; `(subject_person_id)`; `(uploaded_by)`; unique `(store, bucket, object_key)` (a `Staged` attached twice is caught here). No `updated_at`: rows are immutable.

`core.file_tombstone` (decision 16)

| Column | Type | Sensitivity | Notes |
|---|---|---|---|
| `id` | uuid pk | — | |
| `store` | text not null | — | |
| `bucket` | text not null | — | |
| `object_key` | text not null | — | unique with `(store, bucket)` |
| `created_at` | timestamptz not null | — | |
| `attempts` | integer not null default 0 | — | incremented by `Reap`; never reset; the error is logged, not stored |

No personal data: a tombstone names a key, which is a file ID.

Retention: a `core.file` row lives as long as its record (decision 15). A file whose `subject_person_id` is set is deleted by its owner's `DeletePerson` before files' own runs; files' `DeletePerson` is the backstop that refuses to let people finish otherwise. A tombstone lives until its object is confirmed gone. Nothing here is kept after deletion (audit rows are audit's).

**Objects and files on disk**, under the operator's control (`<files.dir>` = `files.dir`, default `<data.dir>/files`, 0700):

- `<files.dir>/tmp/<file-id>`: the spool (0600); removed when `Stage` returns; swept after `OrphanAge`.
- `disk` store: `<files.dir>/store/<bucket>/<aa>/<bb>/<key>` and `<key>.meta` (0600 in 0700 directories), where `aa`/`bb` are the first two bytes of SHA-256(key) in hex, so any key shape fans out. `.meta` is a small JSON document `{size, sha256, content_type, modified}`. `Put` writes `<key>.part`, fsyncs, writes `<key>.meta.part`, fsyncs, renames the meta, then renames the data last and fsyncs the directory: a data file's existence means the object is complete. `List` enumerates data files; `Head` reads `.meta`. Plain files: rsync, restic, tar and `ls` all work on them, which is the operator's sync and backup path.
- `s3` store: `<prefix><bucket>/<key>` in the one real bucket, `Content-Type` and `x-amz-checksum-sha256` set at upload; `Head` reads the checksum back (`ChecksumMode`); a store that does not return it answers `Info.SHA256` zero, and `Verify` counts such rows as checked but not compared (`bms.files.verify{outcome="unchecked"}`).

Why each personal column is collected: `name` is what the person will recognise the file by on a phone screen and in their export; `uploaded_by` says who put a document into a safety record (a legal trail the owner module needs); `subject_person_id` is what makes export and deletion complete without trusting every module to remember its files.

## Permissions

Files declares no permission. Every operation runs under the permission of the owning record, checked by the owner before the call; the owner's spec names it. The object store knows no permissions at all. For the known consumers:

| Operation | Permission (checked by) | Scope |
|---|---|---|
| `Stage`, `Attach`, `Delete`, `DeleteByRecord` on a volunteers qualification record | the qualification record's change permission (`volunteers`, its spec: the person themself for a self-upload when `self_upload` is on; the department's leads) | the record's department |
| `Open` a certificate | the qualification's read permission (`volunteers`) | the person, or a lead who may verify it |
| `Stage`, `Attach`, `Delete` the logo | `settings.manage` (`settings`) | site |
| `Open` the logo | none: `AccessPublic` (`settings`) | — |
| `ExportPerson`, `DeletePerson` | `people.data.export`, `people.data.delete` or the person's own request (`people`) | site |
| `Reap`, `SweepOrphans` | none: jobs run as the system (`jobs.InJob`) | — |
| `Verify` | none: an operator command on the host | — |

Files adds three checks of its own that no permission can lift: the owner scope (decision 1), the actor requirement on `Open` of a `personal` or `sensitive` file (decision 11), and the rule that no URL ever reaches the object store directly (decision 12).

## Signals

None published: no consumer needs "a file was uploaded"; the owning record's own signal carries the event. None subscribed: deletion reaches files through `DeleteByRecord` (the owner) and the `people.Source` (the backstop), not through `people.person-deleted`, because both must run inside the deleting transaction.

## Settings

Core area `files`, `filesettings.Settings []settings.Decl` (leaf package, like `auditsettings`); read through `PolicySource`, which `app` implements over a `Reader` scoped `files`.

| Key | Type | Default | Valid range | Who can change it |
|---|---|---|---|---|
| `files.max_bytes` | Int | `26214400` (25 MiB) | 1048576..1073741824 | `settings.manage` |
| `files.quota_bytes` | Int | `0` (unlimited) | 0..9223372036854775807 | `settings.manage` |

`files.quota_bytes` is site-wide (decision 26): the bound is `sum(size_bytes)` over every `core.file` row, across every owner and level, not per owner, per module or per person; a row deleted but not yet reaped no longer counts. `files.max_bytes` and `files.max_per_record` are per file and per record.
| `files.max_per_record` | Int | `20` | 1..200 | `settings.manage` |

Validation errors are settings' (`range`). Catalogue keys `core.files.setting.<key>.title|description`. A `max_bytes` above `UploadCeiling` cannot be set (the range stops there), so the route cap is always at least the policy.

Configuration (process, `config.md`, Proposed change 3): `files.store` (`BMS_FILES_STORE`, enum `disk|s3`, default `disk`), `files.dir` (`BMS_FILES_DIR`, default `<data.dir>/files`; absolute after resolution; holds `tmp/` always and `store/` for `disk`), `files.s3_endpoint` (`host[:port]` or `http(s)://host[:port]`; no scheme means `https`, a loopback host means `http`), `files.s3_bucket`, `files.s3_region` (default `us-east-1`), `files.s3_prefix` (default empty), `files.s3_path_style` (bool, default `true`), `files.s3_access_key` and `files.s3_secret_key` (secrets, `*_FILE` accepted). `s3` requires endpoint, bucket, access key and secret key (`Invalid` naming the variable); the other store's keys are ignored but still validated for shape. There is no `postgres` value: a configuration naming one is `Invalid one_of`.

## UI (if any)

None owned. What the shell needs for the consumers is proposed to `web-shell` (Proposed change 7): `upload(route, {file, fields}, opts)` in `@/api` (multipart over `fetch`, same `ApiError`, progress optional), `ui/FileField` (a phone-first file input: `accept` from the kinds, `capture` for the camera, shows name and size, the `too_large`, `type`, `quota`, `too_many` field codes), `ui/Attachments` (a list of `File` rows with a download link and a remove button the screen enables; a `503` with `Retry-After` on download renders "not available on this copy yet"), and the header logo (`<img src="/api/settings/logo?v=<id>">` when `Public.Logo` is set, with the regional's name as `alt`). The owners' screens are theirs.

## Behavior

**Why in-process (decision 3).** Three shapes were weighed. (a) *An in-process component with an S3-like Go interface*, backed by local disk by default and by any S3-compatible store as an option: the chosen one. (b) *A separate small process or container of our own* with its own HTTP API and a shared secret between it and bms: a second thing for a volunteer operator to run, upgrade, back up, secure and monitor, against the Phase 1 goal of one container and one database; a secret to manage; a network surface that must not be public; and no gain a regional needs (process isolation of image parsing is minor: files decodes no pixels, it copies segments; scaling is not a need; the on-site copy would lose its files exactly when the internet is gone, the opposite of 09's "files follow the data"). Anyone who wants the bytes on another host already has this shape, built and maintained by others: MinIO, Garage or SeaweedFS behind `s3`. (c) *A built-in server speaking a subset of the real S3 API*, so one adapter serves both: everything in (b) plus implementing SigV4, XML bodies, `ListObjectsV2` paging and multipart upload well enough for a real client, and bms talking to itself over loopback. The two things it would buy are had more cheaply: "one adapter against every backend" by the Go contract plus `objectstest.Conformance`, and "standard tools can reach the files" by the `disk` layout being plain files (rsync, restic, tar) and by `s3` for people with S3 tooling. If a regional later needs the files served over the network by bms itself (an on-site pull sync, Q-FILES8), a thin server over `objects.Store` can be added behind the same contract.

**Stage.** 1 `Upload.Validate`: `Accept` non-empty, `Name` cleaned (`path.Base` after replacing `\` with `/`, trimmed, control characters refused, ≤ `MaxNameBytes`; empty becomes `file`). 2 `db.TxMode(ctx)` must report no transaction (`Internal` otherwise: a handler must not hold a connection through an upload). 3 the policy is read; the current site-wide total (every owner, decision 26) against `QuotaBytes` is read (no transaction) and a total already at the quota fails early with `Conflict quota {max}` (advisory: `Attach` is the enforcing check). 4 the body is streamed to `<TempDir>/<new id>` through a counter: the first `SniffBytes` are sniffed with `Detect` and checked against `Accept` before the rest is read (`Invalid file type`, read stops); more than `MaxBytes` bytes is `Invalid file too_large {max}` (read stops; the temp file is removed); an empty body is `Invalid file required`; a read error from `Upload.Body` that is already a classified `*errs.Error` is returned unchanged (a caller's own limit, e.g. settings' `MaxLogoBytes` reader). 5 for JPEG, PNG, GIF and WebP with `KeepMetadata` false, `imgmeta.Strip` rewrites the temp file; a structure the stripper cannot parse is `Invalid file malformed` (a PDF is never rewritten). 6 SHA-256 and size are taken from the final temp file. 7 `objects.Put{Bucket: owner, Key: id, Size, SHA256, ContentType, Body: the temp file}`; a store failure is `Unavailable` and nothing exists anywhere. 8 the temp file is removed whatever the outcome (deferred; a removal failure is logged once and left to the sweep). 9 `Staged` is returned with `StagedAt` = now. Never logged: the name, the declared type, the bytes. The `disk` store may move the temp file into place with a rename when both are on one filesystem (an implementation freedom `Put` allows for a `*os.File` body); the contract is the same.

**Attach.** 1 `Attachment.Validate`; `Record.Kind`'s area must equal the scoped owner (`Invalid record scope`); `Level.Validate`; `Staged.Validate`, `st.Bucket` must be the owner (`Invalid staged foreign`) and `st.StagedAt` within `OrphanAge` (`Invalid staged expired`: the sweep may have taken it). 2 `db.TxMode(ctx)` must be `Write` (`Internal` otherwise: a file must commit with its record). 3 the policy is read. 4 inside the transaction: `Replace` deletes the record's files (as `DeleteByRecord`); the record's count against `MaxPerRecord` (`Conflict too_many {max}`), the sum of `size_bytes` plus this size against `QuotaBytes` when set (`Conflict quota {max}`; both are read in the transaction without a lock, so two concurrent uploads may overshoot by one file, which the spec accepts); `subject` must exist (the foreign key; `Invalid subject unknown` from `23503`); the row is inserted (a unique violation on `(store, bucket, object_key)` is a `Staged` used twice: `Internal`). 5 `files.file.created` is recorded (`Record` = `audit.RefOf("files.file", id)`, `Subject`, `Level`, `Reason` `record:<kind>:<id>`). `UploadedBy` is the actor when there is one, else zero (an operator command). If the transaction rolls back after `Attach`, the object stays an orphan until the daily sweep (or the handler's deferred `Discard`, which runs after the transaction ended and finds no row).

**Discard.** `objects.Delete(st.Bucket, st.Key)` unless a `core.file` row with that `(store, bucket, object_key)` exists (a read outside any transaction); a store failure is `Unavailable`, logged once, and the object is left to the sweep. Idempotent.

**Open.** 1 the row is loaded (`NotFound` for an unknown ID or another owner). 2 for `personal` and `sensitive`: an actor is required (`Unauthenticated` without one; the superuser counts); for `sensitive` always, and for `personal` when the actor's person is not `Subject`, `files.file.read` is recorded (`Fields: ["content"]`, `Level`, `Subject`, `Reason` as above) before the bytes are opened; a `Record` failure fails the read (audit decision 3). On a read-only copy that entry spools (audit decision 4), so a lead can still view a certificate on site. 3 `store` must equal `Objects.Kind()` (`Unavailable` otherwise: "stored in <kind>, this process is configured for <kind>"; a later `bms files migrate <from> <to>` moves objects without a schema change). 4 `objects.Get`; `ErrMissing` is returned as is (wrapped, `Unavailable`), counted (`bms.files.reads{outcome="missing"}`) and logged once with the file ID. The `Content` carries the ctx it was opened under; the `disk` reader is the `*os.File`; the `s3` reader issues a ranged `GET` per seek.

**Serving.** `Serve` sets: `Content-Type` = `f.ContentType` (the shell already sets `X-Content-Type-Options: nosniff`); `Content-Disposition: inline; filename*=UTF-8''<name>` (plus an ASCII `filename=` fallback, through `mime.FormatMediaType`) for a kind whose `Inline()` is true, `attachment` otherwise; `Content-Security-Policy: default-src 'none'`; `ETag: "<sha256 hex>"`; `Last-Modified` = `CreatedAt`; no `Cache-Control` of its own, at any level (decision 25): the shell set `no-store` before the handler ran (api decision 12, D40) and that is what the client gets, for a `sensitive` incident photo and for the `public` logo alike; `Content-Length` and ranges through `http.ServeContent` (`206`, `416`, `304`: a client that kept the `ETag` itself may revalidate, api F58). It then extends the connection's write deadline to now + `ServeTimeout` with `http.NewResponseController(w).SetWriteDeadline` (a phone on a venue link downloading a 20 MB PDF outlives the shell's 30 s). A `Serve` that sets `Cache-Control` is a review finding (`TestServe_SetsNoCacheControl`). Files never adds `Access-Control-*` (api D6) and never redirects to a store URL.

**ReadUpload.** `r.MultipartReader()` (`Invalid` wrapping `httpx.ErrUnsupportedMediaType` when the request is not multipart); parts are read in order: a text part is collected into `Fields` (more than `MaxFields` is `Invalid fields too_many {max}`; longer than `MaxFieldBytes` is `Invalid <field> too_long {max}`); the first part named `field` with a file name is returned as the stream and parsing stops there (any part after it is unread; a text part the handler needs after the file is `Invalid file order` when the handler asks for it, so the rule "fields first, file last" is enforced on the client in `ui/FileField`); no file part is `Invalid file required`. The shell's `MaxBytesReader` (from `Route.MaxBodyBytes`) still caps the whole body at `UploadCeiling`; the policy cap is the one a person normally hits, as a `400` with the max in `params`.

**The upload handler's shape** (what every owner writes; the volunteers spec's `UploadCertificate` follows it, Proposed change 13):

```go
mp, err := files.ReadUpload(r, "file")                       // 415 / 400
st, err := s.files.Stage(ctx, files.Upload{Name: mp.Name, Accept: files.Documents, Body: mp.Body})
defer s.files.Discard(ctx, st)                                // no-op once attached; error logged by the handler
err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
	rec, err := s.loadAndCheck(ctx, id)                      // the owner's permission and state checks
	f, err := s.files.Attach(ctx, st, files.Attachment{Record: rec.Ref(), Subject: rec.Person, Level: model.Personal, Replace: true})
	return s.store.SetCertificate(ctx, rec.ID, f.ID)
})
```

**Delete.** Inside a `Write` transaction (`Internal` otherwise); the row is deleted and a tombstone `(store, bucket, object_key)` inserted in the same transaction; `files.file.deleted` is recorded; `db.AfterCommit` deletes the object and then the tombstone, each failure logged once with the file ID and counted (`bms.objects.failures{op="delete"}`), leaving the tombstone to `Reap`. `DeleteByRecord` does the same per row and returns the count (0 with no error for none). The guarantee: once the transaction commits, the object is deleted within the hour or the tombstone stays outstanding and visible (`Verify`, the gauge `bms.files.tombstones`); there is no state in which the row is gone and nothing remembers the object.

**Reap** (`files.reap`, hourly, `JobTimeout`). On the writing copy only (`Unavailable` wrapping `db.ErrReadOnly` otherwise; jobs never runs it there anyway, jobs decision 4): tombstones oldest first, in batches of 1000: `objects.Delete`, then delete the tombstone; a store failure increments `attempts` and moves on; the run ends when none remain or the deadline nears; counts go into `files.sweep.ran` (`Reason` `tombstones:<n>`, actor system through `InJob`; recorded only when something was removed) and `bms.files.swept{what="tombstone"}`; the outstanding count is the gauge. Outstanding tombstones with `attempts` ≥ 24 are logged at warn level once per run with their count, never their keys in bulk.

**SweepOrphans** (`files.sweep-orphans`, daily, `JobTimeout`). On the writing copy only: temp files older than `OrphanAge` are removed; for every known bucket (the distinct `owner` values plus every provisioned owner), `objects.List` is walked and every entry older than `OrphanAge` whose key is neither a `core.file` row nor a tombstone for that `(store, bucket)` is deleted (the age keeps a `Staged` object alive long enough for any handler to attach it; `ProbeKey` is skipped). Counts into `files.sweep.ran` (`Reason` `orphans:<n>,temp:<n>`; recorded only when something was removed) and `bms.files.swept{what}`. A `List` failure ends the run `Unavailable` (jobs retries by its policy). The sweep never deletes a row.

**Verify** (`bms files verify`). Outside any transaction, on any copy: every `core.file` row in batches, `objects.Head` each; a missing object is logged with the file ID at warn and counted `Missing`; a checksum present and different from `sha256` is logged and counted `Mismatched`; outstanding tombstones are counted. The command prints the four counts and exits 0 when `Missing` and `Mismatched` are zero, 1 otherwise; `files.verify.ran` records the counts in `Reason`. It is the tool for after a restore, a store move or a sync, and the proof that "rows without objects" are visible rather than silently deleted (decision 17).

**Start and Ping.** `Start` (serve) calls `Ping` under a 10 s deadline: `Unavailable` wrapping `objects.ErrPublic` fails `Start` (decision 20: the process must not add files to an exposed bucket; app exits 1 with the message "the S3 bucket answers anonymous reads; fix its policy"); any other failure (unreachable, forbidden, temp dir unwritable) is logged once at error level with the kind and `Start` returns nil, because the rest of bms must serve while an operator fixes storage. `Ping` (check) returns every failure; `bms check` prints `files: disk ok` / `files: s3 <endpoint> ok` or the error. The `s3` probe: `Put(ProbeKey)` with a 16-byte body in the configured prefix, an anonymous `GET` of the same URL with a fresh unauthenticated client (`ErrPublic` on `200`), then `Delete`; a `403` or `404` on the anonymous read is the healthy answer. The `disk` probe creates the root if missing, writes and removes `<root>/.bms-probe`.

**Store unreachable.** Reads of metadata (`Get`, `ByRecord`) and every screen that lists attachments keep working; `Stage` fails `Unavailable` before anything is written; `Open` fails `Unavailable` (the api answers `503`, `Retry-After: 5`) after the audit entry was recorded (the attempt is still a read attempt; `bms.files.reads{outcome="unavailable"}`); `Delete` succeeds (the row and tombstone commit) and the object waits for `Reap`; `DeletePerson` succeeds for the same reason; `ExportPerson` fails `Unavailable` rather than ship an incomplete export; the jobs fail `Unavailable` and are retried by jobs' policy; `/readyz` is unaffected (the database decides readiness). Nothing in files retries a store call itself.

**Read-only and on-site copies.** Every read works (`Get`, `ByRecord`, `Open`, `Serve`; the audit entry spools). `Stage` works only in that `TxMode` says no transaction; `Attach`, `Delete`, `DeleteByRecord`, `DeletePerson` fail `Unavailable` wrapping `db.ErrReadOnly` from the row statement (the api answers `503 read_only`), so a handler on a read-only copy should not reach `Stage` at all (the shell already hides write actions there); a `Staged` object made by mistake is swept. Where the bytes are decides what a copy can serve: `disk`: only what the operator's sync copied into the copy's `<files.dir>/store` (rsync or restic of plain files alongside the database replication; the order and commands in `docs/operating.md`); a gap is `ErrMissing` (`503`, `Retry-After: 5`, the shell's "not available on this copy yet") and `bms files verify` lists it. `s3`: a shared bucket when both copies reach it; an on-site copy without internet needs a local store kept in step (MinIO or Garage replication, or `rclone sync`), again the operator's, until the `switchover` spec designs a bms-native pull (Q-FILES8). Because the metadata row is replicated with the database, a copy always knows that a file exists, how big it is and what it is, even before the bytes arrive.

**Backups without a shared snapshot** (for `docs/operating.md`, Proposed change 11). `disk`: dump the database **first**, then copy `<files.dir>/store`: a file uploaded between the two is an object without a row (a harmless orphan the sweep removes), never a row without an object; a file deleted between the two is a row in the dump whose object is gone, which `Verify` reports after a restore. `s3`: the database dump plus the bucket's own versioning or replication; the same order. Backups contain every file at its level (privacy-data: "backups contain everything").

**Export.** `ExportPerson(person)` returns one JSON object:

```json
{
  "about":    [{"id": "...", "record": {"kind": "volunteers.qualification-record", "id": "..."},
                "name": "...", "content_type": "application/pdf", "size": 1234,
                "sha256": "<hex>", "created_at": "...", "content": "<base64>"}],
  "uploaded": [{"id": "...", "record": {...}, "name": "...", "content_type": "...",
                "size": 1234, "sha256": "<hex>", "created_at": "..."}]
}
```

`about` is every file whose `subject_person_id` is the person, with content (decision 18, Q-FILES6); `uploaded` is every file the person uploaded whose subject is someone else or nobody, metadata only. In `uploaded`, `name` is present only when the file has no subject **and** its level is `public` or `internal`; for a file about someone else, and for any `personal` or `sensitive` file, the field is absent (not empty): the name describes the subject's data and takes the file's level (decision 27; `privacy-data`: a person's export holds what is about them, never another person's data they happened to handle). The IDs, the record reference, the type, the size, the checksum and the time are the uploader's own trail and stay. Both lists are ordered by `created_at`. Runs in no transaction; a missing object fails the export (`Unavailable` wrapping `objects.ErrMissing`) rather than ship an incomplete one. people's export document is assembled in memory, so an export is as large as the person's files; the spec accepts this for M4 and notes the archive format as a later change (Q-FILES6).

**Deletion.** `DeletePerson(person)` inside people's transaction: `UPDATE core.file SET uploaded_by = NULL WHERE uploaded_by = $1` (counted `Anonymized`); then `SELECT count(*) ... WHERE subject_person_id = $1`; a count above zero is `Conflict in_use` with the owners named in the message (`volunteers: 2`), which rolls back people's whole deletion (people decision 11) and tells the operator which module failed to clean up. `Deleted` is always 0: files deletes nothing of its own here, by design. The modules' `DeleteByRecord` calls earlier in the same transaction wrote tombstones, so the person's bytes are removed after commit and retried by `Reap`: "delete everything about person X" is complete when no tombstone for those keys remains.

**Ownership and privacy.** A `Store` from `Scoped(owner)` filters every statement by `owner` and writes only to bucket `owner`, so no handler can serve another module's file by guessing an ID (decision 1), and a module cannot name another's bucket (there is no bucket parameter on `files.Store`). Keys are file IDs: nothing in the store's listing or its paths reveals a name or a person. `File.LogValue` is the ID; names, declared types and bytes never reach logs, spans, metrics or error messages; `telemetrytest` proves it. Error messages name field codes only. The level is the owner's declaration today; when the privacy-tags abstraction (forthcoming, D43) lands, the owner derives it from the record field's tag and passes it here unchanged.

**Concurrency and lifecycle.** `*Service`, every `Store` from `Scoped` and every `objects.Store` are safe for concurrent use; `Stage` holds one temp file per call. Nothing to close: disk holds no handles between calls; the s3 client is stateless; the database is the pool's. `Start` probes and never writes a row; `app` registers the two `jobs.Decl`s (`Reap` with `Every: ReapEvery`, `SweepOrphans` with `Every: SweepEvery`, both `Timeout: JobTimeout`) through `runner.Scoped("files")`, as it does for `audit.purge`; there is no `Stop`. `New` does no I/O, so `bms openapi` and `bms check` build against `db.None()`; `Ping` is `check`'s I/O.

**Idempotency and retries.** `Stage` is not idempotent (two calls are two objects); `Attach` is guarded by the unique index; `Discard`, `Delete`, `DeleteByRecord`, `objects.Delete`, `Reap` and the sweep are idempotent; `Reap` retries what `AfterCommit` could not delete. Files retries nothing in-line; the caller retries `Unavailable` where its spec says; jobs retries the two jobs by its policy.

**Store choice at runtime.** One `objects.Store` per process (`files.store`); the `store`, `bucket` and `object_key` columns let a later `bms files migrate <from> <to>` (copy every object, flip the column, delete the source) move bytes without a schema change. In M4 a file in another store is `Unavailable` on `Open`.

### Accepted implementation deviations (build, 2026-10-20)

Reviewer-accepted details of the build; the contracts above are read with these.

- **S3 endpoint.** `files.s3_endpoint` accepts `host[:port]` or `http(s)://host[:port]`; with no scheme it is `https`, and a loopback host is `http`.
- **Disk keys.** The disk store refuses keys ending `.meta` or `.part` (its sidecar and partial-write files); noted on `objects.Key`.
- **Attach.** Code `staged: malformed` (the key is not a file ID). `Attach` does no store I/O, so a forged `Staged` with a well-formed key is not detected there; `Verify` and `Open` catch a missing object.
- **Sweeps.** `Reap` and `SweepOrphans` record `files.sweep.ran` only when something was removed. The orphan sweep reads the known keys with `known_object_keys.sql` and subtracts in Go.
- **Audit.** File entries carry `Fields ["content"]` and the file's level.
- **Migrate.** `bms migrate` creates the files directories after the migrations and warns (does not fail) if it cannot.
- **Start.** A positive `ErrPublic` finding is `Unavailable` with the sentinel kept (not `Invalid`).
- **After-commit removal.** `Delete`'s removal of the object after commit uses one 5 s budget for all its objects and stops at the first failure (the tombstones remain for `Reap`).
- **Extras.** Exported beyond the lists above: `objects.MaxContentTypeBytes`, `s3.DefaultRegion`, `s3.DefaultTimeout`, `s3.MaxTimeout`, `files.Accept.Has`, and `filesettings` `Area`, `Key*`, `Default*`, `Min*`, `Max*`.

## Failure modes

| # | Method | Condition | errs kind | Caller is expected to | Test |
|---|---|---|---|---|---|
| F1 | `Options.Validate`/`New` | a nil `DB`, `Clock`, `Logger`, `Tracer`, `Audit`, `Actor` or `Objects`, or an empty `TempDir` | Invalid (`files.<field>`) | fix the assembly | `TestNew_MissingDependency_ReturnsInvalid` |
| F2 | `Scoped` | owner is not a module name or core area, or not a valid `Bucket` | Invalid | fix the assembly | `TestScoped_BadOwner_ReturnsInvalid` |
| F3 | `Stage` | called inside a transaction | Internal | fix the owner | `TestStage_InTx_ReturnsInternal` |
| F4 | `Stage` | empty `Accept`, name over `MaxNameBytes` or with control characters | Invalid (`accept` `required`; `name` `too_long|control_chars`) | fix the owner / show the field error | `TestStage_BadUpload_ReturnsInvalid` |
| F5 | `Stage` | empty body | Invalid (`file`, `required`) | show "choose a file" | `TestStage_EmptyBody_ReturnsInvalidRequired` |
| F6 | `Stage` | the sniffed kind is not in `Accept` (an HTML file named `.png`, an SVG, a zip) | Invalid (`file`, `type`) | show "this kind of file isn't accepted" | `TestStage_UnacceptedType_ReturnsInvalidType` |
| F7 | `Stage` | bytes over `Policy.MaxBytes` | Invalid (`file`, `too_large {max}`) | show the limit | `TestStage_OverMaxBytes_ReturnsInvalidTooLarge` |
| F8 | `Stage` | the image cannot be parsed for stripping | Invalid (`file`, `malformed`) | show "the file is damaged" | `TestStage_CorruptImage_ReturnsInvalidMalformed` |
| F9 | `Stage` | the site-wide total (every owner, decision 26) already at the quota (advisory pre-check) | Conflict (`quota {max}`) | tell an admin | `TestStage_AtQuota_ReturnsConflictQuota` |
| F10 | `Stage` | temp dir unwritable or full | Unavailable | api 503; operator | `TestStage_TempDirUnwritable_ReturnsUnavailable` |
| F11 | `Stage` | `objects.Put` fails (disk full, store unreachable, checksum refused) | Unavailable; nothing stored | api 503 | `TestStage_PutFails_ReturnsUnavailableNothingKept` |
| F12 | `Stage` | ctx deadline while streaming | Timeout / Canceled; temp file removed | api 504 / nothing | `TestStage_Deadline_ReturnsTimeoutAndCleansTemp` |
| F13 | `Attach` | called outside a `Write` transaction | Internal | fix the owner | `TestAttach_NoWriteTx_ReturnsInternal` |
| F14 | `Attach` | `Record.Kind`'s area is not the owner | Invalid (`record`, `scope`) | fix the owner | `TestAttach_ForeignRecord_ReturnsInvalidScope` |
| F15 | `Attach` | zero or malformed `Record`, bad `Level` | Invalid (`record` `required|malformed`, `level` `one_of`) | fix the owner | `TestAttach_BadAttachment_ReturnsInvalid` |
| F16 | `Attach` | the `Staged` is another owner's bucket, or older than `OrphanAge` | Invalid (`staged` `foreign|expired`) | fix the owner / ask the person to upload again | `TestAttach_ForeignStaged_ReturnsInvalid`, `TestAttach_ExpiredStaged_ReturnsInvalid` |
| F17 | `Attach` | the same `Staged` attached twice (unique index) | Internal | fix the owner | `TestAttach_Twice_ReturnsInternal` (integration) |
| F18 | `Attach` | the record already holds `MaxPerRecord` files | Conflict (`too_many {max}`) | show the limit | `TestAttach_RecordFull_ReturnsConflictTooMany` |
| F19 | `Attach` | the site-wide total plus this file would exceed the quota (decision 26) | Conflict (`quota {max}`) | tell an admin | `TestAttach_OverQuota_ReturnsConflictQuota` |
| F20 | `Attach` | `Subject` names no person (`23503`) | Invalid (`subject`, `unknown`) | fix the owner | `TestAttach_UnknownSubject_ReturnsInvalid` (integration) |
| F21 | `Attach` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | api `503 read_only`; the handler's `Discard` removes the object | `TestAttach_ReadOnly_ReturnsUnavailableReadOnly` |
| F22 | `Attach` | the audit `Record` fails | the Recorder's kind | the transaction rolls back; the object is swept or discarded | `TestAttach_AuditFails_RollsBack` |
| F23 | `Discard` | `objects.Delete` fails | Unavailable (logged once; left to the sweep) | log and continue | `TestDiscard_DeleteFails_ReturnsUnavailable` |
| F24 | `Discard` | the object is attached (a row exists) | none: no-op | — | `TestDiscard_AfterAttach_IsNoop` |
| F25 | `Get` | unknown ID, or another owner's file | NotFound | 404 | `TestGet_OtherOwner_ReturnsNotFound` |
| F26 | `ByRecord` | a ref outside the owner's area | Invalid (`record`, `scope`) | fix the owner | `TestByRecord_ForeignArea_ReturnsInvalid` |
| F27 | `Open` | unknown ID or another owner's | NotFound | 404 | `TestOpen_OtherOwner_ReturnsNotFound` |
| F28 | `Open` | `personal`/`sensitive` file with no actor | Unauthenticated | 401; fix the owner's route | `TestOpen_SensitiveNoActor_ReturnsUnauthenticated` |
| F29 | `Open` | the audit `Record` fails | the Recorder's kind | no bytes are returned | `TestOpen_AuditFails_ReturnsErrorNoContent` |
| F30 | `Open` | the row exists, the object does not | Unavailable wrapping `objects.ErrMissing` | api 503, `Retry-After`; operator syncs or verifies | `TestOpen_ObjectMissing_ReturnsUnavailableSentinel` |
| F31 | `Open` | the file's `store` is not the configured one | Unavailable | operator reconfigures or migrates | `TestOpen_OtherStore_ReturnsUnavailable` |
| F32 | `Open` | the store is unreachable | Unavailable | api 503, `Retry-After` | `TestOpen_StoreUnreachable_ReturnsUnavailable` |
| F33 | `Open` | called inside a `Read` transaction for an audited level | Internal (from audit) | fix the owner | `TestOpen_InReadTx_ReturnsInternal` |
| F34 | `Delete`/`DeleteByRecord` | outside a `Write` transaction | Internal | fix the owner | `TestDelete_NoWriteTx_ReturnsInternal` |
| F35 | `Delete` | unknown or another owner's | NotFound | 404 | `TestDelete_OtherOwner_ReturnsNotFound` |
| F36 | `Delete` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | 503 | `TestDelete_ReadOnly_ReturnsUnavailable` |
| F37 | `Delete` | `objects.Delete` fails after commit | none returned (logged once, counted; the tombstone stays for `Reap`) | nothing | `TestDelete_ObjectDeleteFails_LeavesTombstone` |
| F38 | `ReadUpload` | not `multipart/form-data` | Invalid wrapping `httpx.ErrUnsupportedMediaType` | api 415 | `TestReadUpload_NotMultipart_ReturnsUnsupportedMediaType` |
| F39 | `ReadUpload` | no file part | Invalid (`file`, `required`) | 400 | `TestReadUpload_NoFilePart_ReturnsInvalidRequired` |
| F40 | `ReadUpload` | too many or too long text parts | Invalid (`fields` `too_many`, `<field>` `too_long`) | 400 | `TestReadUpload_FieldLimits_ReturnsInvalid` |
| F41 | `ReadUpload` | body over the route cap | `*http.MaxBytesError` unchanged | api 413 | `TestReadUpload_OverRouteCap_Returns413` |
| F42 | `Serve` | an unsatisfiable `Range` | none (416 written by `ServeContent`) | — | `TestServe_BadRange_416` |
| F43 | `Serve` | the client is gone mid-write | none (write ends) | — | `TestServe_ClientGone_Stops` |
| F44 | `ExportPerson` | zero ID | Invalid | fix the caller | `TestExportPerson_ZeroID_ReturnsInvalid` |
| F45 | `ExportPerson` | a subject file's object is missing or the store is unreachable | Unavailable (wrapping `ErrMissing` when missing) | people reports the export failed | `TestExportPerson_ObjectMissing_ReturnsUnavailable` |
| F46 | `DeletePerson` | a file with `subject = person` remains | Conflict (`in_use`) | people rolls back and names the owner | `TestDeletePerson_SubjectFileRemains_ReturnsConflictInUse` |
| F47 | `DeletePerson` | outside a `Write` transaction; read-only copy | Internal; Unavailable wrapping `db.ErrReadOnly` | fix the caller; 503 | `TestDeletePerson_NoWriteTx_ReturnsInternal`, `TestDeletePerson_ReadOnly_ReturnsUnavailable` |
| F48 | `Reap`/`SweepOrphans` | read-only copy | Unavailable wrapping `db.ErrReadOnly` | jobs does not run them there | `TestReap_ReadOnly_ReturnsUnavailable` |
| F49 | `Reap` | `objects.Delete` fails for a tombstone | none for that row (`attempts` incremented, counted); the run continues | — | `TestReap_DeleteFails_IncrementsAttempts` |
| F50 | `SweepOrphans` | `objects.List` fails | Unavailable | jobs retries | `TestSweep_ListFails_ReturnsUnavailable` |
| F51 | `SweepOrphans` | an object younger than `OrphanAge` without a row | none: kept | — | `TestSweep_YoungOrphan_Kept` |
| F52 | `Verify` | a row's object missing / checksum differs | none returned; counted `Missing` / `Mismatched`, IDs logged; the command exits 1 | operator restores or syncs | `TestVerify_MissingAndMismatched_Counted` |
| F53 | `Verify` | the store is unreachable | Unavailable | command exits 1 | `TestVerify_StoreUnreachable_ReturnsUnavailable` |
| F54 | `Start` | the probe object is anonymously readable | Unavailable wrapping `objects.ErrPublic` | app exits 1 | `TestStart_PublicBucket_ReturnsInvalid` |
| F55 | `Start` | the store is unreachable or the temp dir unwritable | none returned (logged once at error) | serve continues | `TestStart_StoreUnreachable_LogsAndContinues` |
| F56 | `Ping` | temp dir unwritable, root uncreatable, store unreachable or forbidden, bucket public | Unavailable / Invalid wrapping `ErrPublic` | `bms check` prints it | `TestPing_StoreUnusable_ReturnsUnavailable`, `TestPing_PublicBucket_ReturnsInvalid` |
| F57 | `disk.New`/`s3.Config.Validate` | empty or relative dir; missing endpoint, bucket or keys | Invalid (the config variable) | exit 2 | `TestDiskNew_RelativeDir_ReturnsInvalid`, `TestS3Config_Missing_ReturnsInvalid` |
| F58 | `objects.Put` (every store) | the key exists | Conflict | never expected (fresh IDs); conformance | `TestObjects_PutTwice_ReturnsConflict` (objectstest) |
| F59 | `objects.Put` (every store) | fewer or more bytes than `Size`, or a SHA-256 that does not match | Invalid (`size` / `checksum`); nothing kept | fix the caller | `TestObjects_PutShortRead_ReturnsInvalidKeepsNothing`, `TestObjects_PutBadChecksum_ReturnsInvalidKeepsNothing` (objectstest) |
| F60 | `objects.Put`/`Get`/`Head`/`Delete`/`List` | an invalid `Bucket` or `Key` | Invalid (`bucket` / `key`) | fix the caller | `TestObjects_BadKey_ReturnsInvalid` (objectstest) |
| F61 | `objects.Get`/`Head` (every store) | missing key | Unavailable wrapping `ErrMissing` | see F30 | `TestObjects_GetMissing_ReturnsSentinel` (objectstest) |
| F62 | `objects.*` (`s3`) | endpoint unreachable, credentials refused, bucket absent | Unavailable | 503; `bms check` | `TestS3_Unreachable_ReturnsUnavailable` (integration) |
| F63 | any | ctx done before a statement or store call | Timeout / Canceled | as the kind says | `TestStore_ContextDone_ReturnsTimeout` |

`Detect`, `Kind` methods, `DefaultPolicy`, `Bucket.Validate` and `Key.Validate` on well-formed input, and `objects.Kind.String` cannot fail: they are pure checks over fixed rules.

## Lock-in check

External technology wrapped: object storage behind `objects.Store`. The shipped default, `disk`, is the local filesystem, free of any vendor and readable by every ordinary tool; the `s3` implementation speaks the S3 API through `github.com/minio/minio-go/v7` (Apache-2.0) and is tested in integration against SeaweedFS (Apache-2.0, self-hosted, in `deploy/compose` behind the profile `s3`; D45 replaced MinIO as the development server); Garage (AGPL-3.0), SeaweedFS (Apache-2.0) and Ceph RGW (LGPL) speak the same API, and AWS S3 and its commercial lookalikes work without a code change. Every implementation passes `objectstest.Conformance`. No feature of the core needs `s3`; a regional on one small server never configures it.

## Test plan

- **Unit (no infrastructure):** the service over `dbtest.NewFake()` for transaction mode and read-only behaviour, `objectstest.NewMem()` for bytes, `audittest.NewFake("files")`, `telemetrytest.New(t)` (no name, declared type or bytes in any record or span; the fake's seed markers never appear), a fake clock, an `ActorFunc` the test sets. Table tests over F1–F16, F18–F19, F21–F30, F32–F41, F44–F56, F63. The catalogue: a fixture per kind plus HTML-named-`.png`, SVG, zip, HEIC, PDF-with-leading-junk, all refused. The stripper: JPEG with GPS and Orientation 6 (GPS gone, orientation kept, pixels byte-identical), PNG with `eXIf` and `tEXt`, GIF with a comment, WebP with `EXIF`, corrupt structures (F8), `KeepMetadata` leaves bytes identical. `ReadUpload`: fields before and after the file, limits, non-multipart. `Serve`: every header per level, `inline` for images and PDF, `ETag`/`304`, `Range`/`206`, `416`, the CSP, no `Access-Control-*`, no redirect, the write deadline extended (a `ResponseController`-aware recorder). Ownership: two scoped stores, each blind to the other's files and writing to its own bucket (F14, F16, F25–F27, F35). Staging: `Stage` then `Attach` then `Discard` is a no-op; `Stage` then `Discard` leaves nothing; `Stage` then a rolled-back `Attach` leaves an orphan the sweep removes after `OrphanAge` and keeps before (F51). Tombstones: `Delete` with a failing store leaves a tombstone, `Reap` with a working store clears it (F37, F49). `Verify` with a removed object and a corrupted `.meta` (F52). `DeletePerson` and `ExportPerson` shapes.
- **Object store conformance (`objectstest.Conformance`):** put/head/get/delete/list, bucket isolation (the same key in two buckets), prefix listing, `Conflict` on a repeated key, `ErrMissing`, short and long bodies and a wrong checksum keep nothing (F58–F61), seeking across a 3.5 MiB object, concurrent puts, `Ping` on an empty root; run for `objectstest.Mem` and `disk` (on `t.TempDir()`) in unit tests, and for `s3` (SeaweedFS from `deploy/compose`, `BMS_TEST_S3_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY`) under `//go:build integration`, including the public-bucket probe against a bucket whose policy was set to `download` (F54, F56) and an unreachable endpoint (F62).
- **Integration (`//go:build integration`):** migrations apply and `CheckSchema` passes; `Attach` commits the row with the owner's record and rolls back on an audit failure (F22); the unique index (F17); the foreign keys (F20; `DeletePerson` RESTRICT backstop F46); `Open` on a read-only `TempDB` (`default_transaction_read_only`) serves and spools the audit entry; `Reap` and the sweep over `disk` and MinIO with an orphan older and younger than `OrphanAge`, a tombstone, and `ProbeKey` present; `Verify` after an object is removed behind files' back; the lifecycle run (stage, attach, open, discard, reap, sweep) over the `disk` store on `t.TempDir()` (D52(e); `integration_disk_store_test.go`).
- **Privacy:** `telemetrytest` capture over every path with a seeded name containing a marker; the audit entries carry IDs, levels and tokens only; the disk layout and the s3 listing contain no name (a test walks the store after an upload named with the marker); the export of a person with one subject file and one uploaded file; an `uploaded` entry for a file about someone else, or at level `personal` or `sensitive`, has no `name` key, and one for a subjectless `internal` file has it (`TestExportPerson_UploadedAboutOther_OmitsName`, `TestExportPerson_UploadedNoSubjectInternal_KeepsName`); `Serve` sets no `Cache-Control` and the recorded response carries only what the caller set (`TestServe_SetsNoCacheControl`); the quota counts files of two owners together (`TestAttach_OverQuota_CountsEveryOwner`).
- **Consumers' tests:** `moduletest.Conformance` fills a nil `Deps.Files` with `filestest.NewFake(manifest.Name)`; a module's `seeded_person_delete_counts_and_is_idempotent` passes only when its `DeletePerson` left no subject file in the fake (the fake's `DeletePerson` applies F46); the leak rule scans `Files()` and `Staged()` names for the seed markers.
- **End-to-end (app):** with the logo (Proposed change 6) in place: upload a PNG as settings admin, see it in the header as a participant, replace it, remove it, and `bms files verify` reports zero missing; an HTML file renamed `.png` is refused with `type`; in `readonly` mode the logo still serves and `PUT` answers `503 read_only`.

## Open questions

- **Q-FILES1 (with Q-FILES-SHAPE).** Answered by D43 (2026-10-20), shape confirmed by the owner (2026-10-20, D44): bms ships a built-in S3-like object store, `objects.Store`; file contents are never stored in the database. The shipped default is `disk`; `s3` is the option for a store on another host. The `postgres` chunk store is gone.
- **Q-FILES2. Does the `s3` store ship in M4?** *Recommendation (revised):* yes, both implementations ship in M4. With `postgres` gone, `s3` is the only way to put the bytes on another host, which is the "service" the owner asked for when a regional needs one; the adapter is small over minio-go; and the conformance suite is only proven by a second real implementation. The dev compose gains MinIO behind a profile `s3` (as Dex sits behind `oidc`), so the default `make up` is unchanged. **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES3. SVG logos?** Many regionals have a vector logo. SVG can carry scripts and external references; accepting it needs a sanitiser (parse, allow-list elements and attributes, refuse `script`, `foreignObject`, event handlers and external `href`s). *Recommendation:* not in M4; `Brand` is PNG, JPEG and WebP; add `SVG` (public files only, sanitised) as a later catalogue entry. **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES4. The limits' defaults:** `files.max_bytes` 25 MiB, `files.quota_bytes` unlimited, `files.max_per_record` 20. *Recommendation:* keep them; a phone photo is 3–12 MB and a scanned certificate under 5 MB; a regional on a small disk sets a quota (it now bounds a directory the operator can see grow). **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES5. Strip image metadata by default?** GPS, timestamps and device identifiers leave a phone photo unless removed; the owner module may keep them with `KeepMetadata` (art placement photos, later). *Recommendation:* strip by default, keep only orientation; incidents never keeps. **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES6. Does a person's export include the bytes of files about them?** *Recommendation:* yes, inline as base64 in the one JSON document people already produces (a data-access request is for a copy, and a certificate is a copy of the person's data). The cost is export size and memory for a rare operation; when a regional needs it, people's export becomes an archive (JSON plus files) in a later change to `people.md`, without changing this interface. **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES7. The logo lives with `settings`** (its routes, screen field and audit actions; files stores and serves it), not as a files feature. *Recommendation:* yes (decision 19); it keeps files a service with no product surface of its own and keeps the regional's branding in one place. Approving this approves Proposed change 6. **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES8. How does an on-site or read-only copy get the files?** The metadata row replicates with the database; the objects do not. *Recommendation:* in Phase 1 it is the operator's sync, documented (`disk`: rsync or restic of `<files.dir>/store` on the same schedule as the database replication; `s3`: bucket replication or `rclone sync` to a local store), with `ErrMissing` making a gap visible and `bms files verify` listing it. A bms-native pull (the copy fetching missing objects from the writer over an authenticated route, or a `bms files sync <from>` command) is designed with the `switchover` spec (D27, Q-OFFLINE), where it belongs with the rest of the copy's handover; the contract already has what it needs (`List`, `Head` with checksums). **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES9. Do modules ever get the raw object store?** The owner's words were "a small file service that other features can use". *Recommendation:* features use it through `files.Store`, attaching bytes to their own records (decision 21), because the row is what enforces ownership, level, audit, export and deletion; `objects.Store` is for the core and `app`. If a module later needs bytes that belong to no record (a generated export, a rendered report), the module makes a record for them. Confirming this keeps `module.Deps` at one field, `Files`. **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES10. Fail `Start` on a publicly readable bucket?** Decision 20 makes a positive probe result (`ErrPublic`) fatal at start-up and leaves an unreachable store non-fatal. *Recommendation:* yes; a public bucket is a misconfiguration that exposes every certificate and photo, and principle "the process won't start half-configured" applies, while availability of the rest of bms is kept for every other storage failure. The alternative (log at error and serve) is a one-line change if the owner prefers it. **Settled: accepted as recommended (owner, 2026-10-20, D44).**
- **Q-FILES11 (2026-10-20). Should the logo ever be cached by the browser?** **Answered no (owner, 2026-10-21, D45): the logo is not browser-cached; decision 25 stands (`Serve` sets no `Cache-Control`, everything on `/api/` is `no-store`) and api adds no route-level cache declaration.** The question as asked: Decision 25 settles the code: `Serve` sets no `Cache-Control`, so every file on `/api/` is `no-store`, private attachments and the public logo alike. The one cost is the logo: with `no-store` the browser cannot keep it, so it is fetched once per app boot (the shell loads it once per session, not per screen; `MaxLogoBytes` is 2 MiB, a typical logo far less). The trade-off the owner may want to weigh is that fetch on a phone at the venue against the rule "no handler, no route, sets its own `Cache-Control` on `/api/`" (api decision 12, D40). *Recommendation:* no exception now; if a regional measures it as a problem, api adds the route-level declaration it reserved (`module.Route.Cache`, allowed only on `AccessPublic` routes whose body holds no personal data, `public, max-age=…, immutable` keyed by the file ID in the URL) and settings' `GET /api/settings/logo` declares it; `Serve` still never sets the header, and the shell stays the only writer of `Cache-Control`. The code does what decision 25 says: `no-store`.

## Proposed changes elsewhere

1. (applied 2026-10-20, D44, in api.md as decisions 32 and 33 with F61, F62; part (b) **withdrawn** 2026-10-20 by decision 25: it conflicted with api decision 12 and F58 as settled in D40, and files no longer needs it; api.md's Proposed change 18 note should read "part (b) withdrawn by files decision 25", a one-line edit for `spec-editor`; a later route-level public-cache declaration is Q-FILES11, not this change) **`plan/design/api.md` (row 16):** (a) `module.Route` (and the core `Route` types `app` converts) gain `MaxBodyBytes int64` and `Timeout time.Duration` (zero = the shell's defaults; a route may only raise them, up to 1 GiB and 10 minutes); the pipeline applies the route's cap in `MaxBytesReader` and the route's timeout to the ctx deadline and the connection's read and write deadlines through `http.ResponseController`. (b) `Cache-Control: no-store` on `/api/` is set only when the handler set no `Cache-Control` (files sets its own per level). (c) The "File uploads and downloads" row in "Not responsible for" points here and lists what the shell now provides. (d) OpenAPI: an upload route documents a `multipart/form-data` request body and a content route a binary response (`image/*`, `application/pdf`), so the generated client types them. (e) Failure modes: a route cap above the ceiling or a timeout above 10 minutes is `Invalid` at `New`.
2. (applied 2026-10-20, D44) **`plan/design/module-contract.md` (row 4):** `Deps.Files files.Store` (scoped per module: the module's name is the owner and the bucket); the two `Route` fields in 1(a) with `Validate` checking their ceilings; `moduletest.Conformance` fills a nil `Deps.Files` with `filestest.NewFake(manifest.Name)`, and the leak rule scans the fake's `Files()` and `Staged()` names for the seed markers. The `Files` row in the `Deps` table moves from "Later" to M4. No `Deps.Objects` (Q-FILES9).
3. (applied 2026-10-20, D44) **`plan/design/config.md` (row 3):** a `Files` section with the keys under Settings (`BMS_FILES_STORE` enum `disk|s3`, default `disk`; `BMS_FILES_DIR` default `<data.dir>/files`, resolved to an absolute path; `BMS_FILES_S3_ENDPOINT`, `_BUCKET`, `_REGION` (`us-east-1`), `_PREFIX`, `_PATH_STYLE` (`true`), `_ACCESS_KEY` and `_SECRET_KEY` (secrets, `*_FILE`)); the rule that `s3` requires its four; the "Later keys" row loses "S3-compatible access keys (`files`)".
4. (applied 2026-10-20, D44, row 10 of the part table) **`plan/design/app.md` (row 18):** part 10 `files`: `declare` adds `filesettings.Settings` as core area `files` and the migration set; `build` makes the `objects.Store` from `cfg.Files` (`disk.New(filepath.Join(cfg.Files.Dir, "store"))` or `s3.New`), calls `files.New` with `audit.Scoped("files")`, the actor closure, `TempDir` `<files.dir>/tmp` and a `PolicySource` over a `files` `Reader`; `provision` sets `a.deps.Files = svc.Scoped(m.Name)`; `link` appends `people.Source{Name: "files", Exporter: adapter}` after the modules, gives settings a lazy `files.Store` scoped `settings`, and registers through `runner.Scoped("files")` the two `jobs.Decl`s (`files.reap`, `Every: files.ReapEvery`; `files.sweep-orphans`, `Every: files.SweepEvery`; both `Timeout: files.JobTimeout`; handlers are closures over `svc.Reap` and `svc.SweepOrphans`, as `audit.purge` is wired); `start` calls `svc.Start` and treats `Invalid` wrapping `objects.ErrPublic` as a start failure (exit 1 with the message under Behavior); `migrate` creates `<files.dir>`, `<files.dir>/tmp` and, for `disk`, `<files.dir>/store` (0700); `check` prints `files: <store> ok` or the `Ping` error; a new command `bms files verify` (prints the four counts; exit 0 when nothing is missing or mismatched, 1 otherwise, 2 on config) joins the command table. The files table under "Owns" gains `<files.dir>`.
5. (applied 2026-10-20, D44) **`plan/design/db.md` (row 5):** the pending proposed change 10 list marks `files` as exporting `Set{Name: "files", Schema: "core"}` with `postgres.Migrations` (two tables, no bytes), after `jobs` in the core set order.
6. (applied 2026-10-20, D44, as the amendment in settings.md) **`plan/design/settings.md` (row 15):** the logo, as D39 deferred it here: `Options.Files files.Store` (scoped `settings`, lazy from `app`); routes `GET /api/settings/logo` (`AccessPublic`; `ByRecord(logoRef)`, `Open`, `files.Serve`; `404 not_found` when none), `PUT /api/settings/logo` (`settings.manage`; `ReadUpload(r, "file")`, `Stage{Accept: files.Brand}` then, in a `Write` transaction, `Attach{Record: logoRef, Level: model.Public, Replace: true}`, with a deferred `Discard`; `Route.MaxBodyBytes = files.UploadCeiling`, `Route.Timeout = files.UploadTimeout`; a logo over `MaxLogoBytes = 2 MiB` is `Invalid file too_large` before the policy cap), `DELETE /api/settings/logo` (`settings.manage`; `DeleteByRecord`); `logoRef = audit.Ref{Kind: files.LogoRecordKind, ID: sha256("settings.logo")[:16]}` (the fixed-ID pattern of departments-events decision 25); `Public` gains `Logo string` (the file ID, `""` when none) so the shell can version the image URL; audit actions `settings.logo.changed|removed`; the `/settings` regional area gains a logo field (`ui/FileField`, `accept` PNG/JPEG/WebP, preview, remove); "Not responsible for" drops "the logo (comes with `files`)"; decision 8 is marked superseded by this change.
7. (applied 2026-10-20, D44) **`plan/design/web-shell.md` (row 17):** `@/api` gains `upload(route, {file, fields}, opts)` (multipart through the same `fetch` path, fields before the file, the same `ApiError`); `ui/FileField` and `ui/Attachments` (under UI above, including the "not available on this copy yet" state for a `503` with `Retry-After` on a download); the header renders `/api/settings/logo?v=<Public.logo>` when set, the regional's name as `alt` and as text otherwise; catalogue keys `field.too_large`, `field.type`, `field.quota`, `field.too_many`, `field.order`, `field.malformed` (if absent); the e2e smoke covers the logo round trip. ESLint keeps `fetch` inside `@/api`.
8. (applied 2026-10-20, D44, in people.md; the `bms delete` operator note is carried to `docs/operating.md`, not yet written) **`plan/design/people.md` (row 8):** a `Source` named `files` is linked after the modules (`app`); the export document gains `"files": {...}` (the shape under Behavior); the deletion report's `Modules["files"]` carries `Anonymized`; decision 11's order reads "modules in build order, then files, then authz, then identity, then people's rows"; a module's `DeletePerson` must delete its files with subject = the person or people's deletion fails `Conflict in_use` naming the module; the operator docs for `bms delete` say the bytes leave the store within the hour (`files.reap`) and `bms files verify` shows no tombstone outstanding.
9. (applied 2026-10-20, D44) **`plan/design/jobs.md` (row 14, Approved):** decision 11's list of the first scheduled jobs gains `files.reap` (hourly) and `files.sweep-orphans` (daily), both registered by `app` through `runner.Scoped("files")` with closures over the files service, like `audit.purge`; nothing else changes in jobs.
10. (applied 2026-10-20, D44) **`plan/design/telemetry.md` (row 15b):** metric rows `bms.files.uploads{outcome: ok|invalid|conflict|unavailable|timeout}`, `bms.files.reads{outcome: ok|missing|denied|unavailable}`, `bms.files.swept{what: tombstone|orphan|temp}`, `bms.files.tombstones` (gauge), `bms.files.verify{outcome: ok|missing|mismatched|unchecked}`, `bms.objects.failures{op: put|get|head|delete|list|ping}`; span names `files.stage`, `files.attach`, `files.open`, `files.delete`, `files.reap`, `files.sweep`, `files.verify`, `objects.put|get|head|delete|list|ping` (Client, with `objects.kind`).
11. (pending: target not written; `docs/operating.md` does not exist, and `deploy/` packaging is Phase 1; the compose profile `s3` (SeaweedFS since D45) and `BMS_TEST_S3_*` variables are already in `deploy/compose`; only the operating text waits) **`docs/operating.md` (not yet written) and `deploy/` (Phase 1 packaging):** the files directory is inside `data.dir`, so the one-container deployment mounts one volume for both; per store, what to back up and in what order (database dump first, then `<files.dir>/store`; `s3`: the dump plus the bucket's versioning or replication); how a read-only or on-site copy gets the files (`disk`: sync `<files.dir>/store` with the database replication; `s3`: a shared bucket, or a local replica of it on site) and what a gap looks like (`503`, "not available on this copy yet", `bms files verify`); the S3 bucket must be private (the start-up probe refuses a public one) and reachable only from the bms hosts; the temp dir; the two jobs; the lines `bms check` prints; `bms files verify` after a restore or a move. The dev compose gains a SeaweedFS service (D45; first drafted as MinIO) behind profile `s3` with a private `bms` bucket, a `bmspublic` bucket that allows anonymous reads (for the public-bucket probe test) and the `BMS_TEST_S3_*` variables in `.env.example` (Q-FILES2); built 2026-10-21.
12. (applied 2026-10-20, D44, in `plan/07-abstractions.md`, `plan/09-core-features.md` and the three skills) **`plan/07-abstractions.md`:** the `files` entry becomes: *Purpose:* keep the bytes a module attaches to one of its records, with the record's permissions deciding who may read them.
    *Owns:* the object-store contract `objects.Store` (a simplified S3: bucket, key, Put/Get/Head/Delete/List, SHA-256 verified at Put; never the database, D43) with the `disk` (default) and `s3` implementations and their conformance suite; the `Store` seam modules get as `Deps.Files`; the metadata (`core.file`, `core.file_tombstone`); the catalogue of accepted kinds (images and PDF), metadata stripping, the safe `Serve`, the audit of content reads, the person-data export and deletion backstop, the reap and sweep jobs, `bms files verify`, settings `files.max_bytes|quota_bytes|max_per_record`, config `files.*`.
    *Doesn't do:* permissions (the owning module), routes and screens (the owner; the logo is settings'), retention (the owner's job), running a store over the network (MinIO, Garage, SeaweedFS, Ceph through `s3`), keeping a copy's objects in sync (the operator; later `switchover`), virus scanning, thumbnails. The implementations line reads "disk, s3-compatible (objects)". Also `09-core-features.md` §10 moves from Later to M4 with this wording, and `plan/09`'s milestone table row for `files`.
    The `go-conventions`, `add-module` and `privacy-data` skills gain the module rule: from M4 `deps.Files` is a `files.Store` scoped to the module; an upload is `Stage` before the module's transaction and `Attach` inside its `Write` transaction with the record, with a deferred `Discard`; `Delete` and `DeleteByRecord` run inside the module's `Write` transaction; the module serves content from its own route after its permission check with `files.Open` and `files.Serve`, never through a public route unless the file's level is `public`, and never through a store URL; the level and `Subject` declared at `Attach` come from the module's spec (later from the field's privacy tag); `DeletePerson` deletes every file whose subject is the person; tests use `filestest.NewFake(Manifest.Name)`.
13. (applied 2026-10-20, D44, in module-volunteers.md) **`plan/design/module-volunteers.md` (Approved, D43):** `UploadCertificate` follows the handler shape under Behavior: `ReadUpload`, `Stage` (outside the transaction, `Accept: files.Images` and `files.Documents` as its spec says), then its `Write` transaction with the record checks and `Attach{Record: Ref{Kind: "volunteers.qualification-record", ID}, Subject: person, Level: model.Personal, Replace: true}`, a deferred `Discard`; "Expects of dependencies" for `files` reads `Stage`/`Attach`/`Discard` instead of `Create`. Nothing else in the volunteers spec changes.
14. (applied 2026-10-20, D44, as the rules `minio-seam` and `objects-impl-seam` in `.golangci.yml`) **`.golangci.yml` (depguard):** `internal/core/objects/s3` is the only package that may import `github.com/minio/minio-go/v7`; `internal/core/files` and every module import `core/objects` only through its contract package (no `disk` or `s3` outside `app`).
