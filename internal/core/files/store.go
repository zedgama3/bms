package files

import (
	"context"
	"io"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/objects"
)

// Content is a file's bytes, seekable for ranges. It is bound to the ctx given to Open and
// must be closed before that ctx ends; Serve does not close it.
type Content = io.ReadSeekCloser

// Store is what a module gets as Deps.Files and a core part gets from Scoped: every method
// sees only the owner's files, and every object it writes goes to the owner's bucket.
// Consumers: volunteers (certificates), regional (the logo), later incidents and afterreports.
type Store interface {
	// Stage spools, sniffs, strips, hashes and puts the upload in the owner's bucket. Called
	// outside any transaction (Internal inside one). Consumers: every owner's upload
	// handler, before it opens its transaction. A Body read error that is already a
	// classified *errs.Error is returned unchanged (regional's MaxLogoBytes reader relies on
	// it).
	Stage(ctx context.Context, up Upload) (Staged, error)
	// Attach binds a Staged object to a record inside the caller's Write transaction,
	// inserting the row and recording files.file.created. Record.Kind's area must be the
	// owner. Consumers: every owner's upload use case.
	Attach(ctx context.Context, st Staged, a Attachment) (File, error)
	// Discard removes a Staged object that will not be attached; idempotent, a no-op once
	// attached. Consumers: every upload handler (deferred).
	Discard(ctx context.Context, st Staged) error
	// Get answers one file's metadata. Consumers: an owner showing one attachment.
	Get(ctx context.Context, id FileID) (File, error)
	// ByRecord lists a record's files, oldest first. Consumers: an owner listing
	// attachments, regional finding the logo, an owner's ExportPerson. It may be called
	// inside a Read transaction (no audit, no store I/O).
	ByRecord(ctx context.Context, rec audit.Ref) ([]File, error)
	// Open answers the metadata and the bytes, auditing the read when the level requires it
	// (decision 10). Called outside any transaction. Consumers: every owner's content route.
	Open(ctx context.Context, id FileID) (File, Content, error)
	// Delete removes one file inside the caller's Write transaction, writes its tombstone
	// and records files.file.deleted; the object goes after commit. Consumers: an owner
	// removing one attachment; regional removing the logo.
	Delete(ctx context.Context, id FileID) error
	// DeleteByRecord removes every file of a record (same rules). Consumers: an owner
	// deleting a record, its retention job, its DeletePerson.
	DeleteByRecord(ctx context.Context, rec audit.Ref) (int, error)
}

// scoped is a Store for one owner.
type scoped struct {
	s      *Service
	owner  string
	bucket objects.Bucket
}

var _ Store = (*scoped)(nil)
