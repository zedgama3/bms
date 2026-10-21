// Package rows holds the record types files' service and its PostgreSQL store share. It is a
// leaf: it imports nothing of the project, because files imports postgres to build its store
// and so postgres cannot import the root. UUIDs travel as [16]byte (the zero value is NULL
// or "none"), levels as the model.Sensitivity number, and a zero time means NULL.
package rows

import "time"

// File is one core.file row.
type File struct {
	ID          [16]byte
	Owner       string
	RecordKind  string
	RecordID    [16]byte
	Subject     [16]byte // zero: nobody in particular (NULL)
	UploadedBy  [16]byte // zero: no one (NULL)
	Level       int16
	Name        string
	ContentType string
	Size        int64
	SHA256      [32]byte
	Store       string // "disk" or "s3"
	Bucket      string
	ObjectKey   string
	Stripped    bool
	CreatedAt   time.Time
}

// Deleted is a row a delete removed, with the tombstone written in the same statement.
type Deleted struct {
	File
	TombstoneID [16]byte
}

// InsertResult says whether Insert wrote the row and, when it did not, which limit stopped it.
type InsertResult struct {
	Inserted  bool
	TooMany   bool // the record already holds the maximum number of files
	OverQuota bool // the total plus this file would exceed the quota
}

// Tombstone is one pending object deletion.
type Tombstone struct {
	ID        [16]byte
	Store     string
	Bucket    string
	ObjectKey string
	CreatedAt time.Time
	Attempts  int
}

// VerifyRow is what Verify needs of a row: where its object is and what it should be.
type VerifyRow struct {
	ID        [16]byte
	Store     string
	Bucket    string
	ObjectKey string
	Size      int64
	SHA256    [32]byte
}

// OwnerCount is how many files an owner holds.
type OwnerCount struct {
	Owner string
	Files int64
}
