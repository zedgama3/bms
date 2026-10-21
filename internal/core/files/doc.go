// Package files is the attachment service: it keeps the bytes a module attaches to one of its
// records, with the record's permissions deciding who may read them (files.md; D43, D44).
//
// The bytes live in an object store (internal/core/objects: local disk by default, or any
// S3-compatible store), never in the database. What stays in PostgreSQL is one small metadata
// row per file (core.file: owner, record, subject, level, name, type, size, checksum, where
// the object is) and one row per pending object deletion (core.file_tombstone).
//
// An upload is two calls. Stage, outside any transaction, spools the request body to a temp
// file, sniffs its type against what the caller accepts, strips image metadata, hashes it and
// puts it in the owner's bucket. Attach, inside the owner's Write transaction with its record,
// inserts the row and records the audit entry. A Staged object never attached is an orphan the
// daily sweep removes. Deleting a file writes a tombstone in the transaction and removes the
// object after commit; the hourly reap retries until it is gone.
//
// A module gets a Store scoped to its own name (Service.Scoped), so it sees only its own
// files, and serves a file from its own route after its own permission check with Open and
// Serve. Files owns no routes, screens or permissions, and no URL ever reaches the object
// store directly.
package files
