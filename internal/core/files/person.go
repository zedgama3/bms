package files

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/files/rows"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

type exportRecord struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// exportFile is one entry of the export: the "about" list carries Name and Content, the
// "uploaded" list neither Content nor (unless it is safe) Name.
type exportFile struct {
	ID          string       `json:"id"`
	Record      exportRecord `json:"record"`
	Name        *string      `json:"name,omitempty"`
	ContentType string       `json:"content_type"`
	Size        int64        `json:"size"`
	SHA256      string       `json:"sha256"`
	CreatedAt   time.Time    `json:"created_at"`
	Content     []byte       `json:"content,omitempty"`
}

type exportDoc struct {
	About    []exportFile `json:"about"`
	Uploaded []exportFile `json:"uploaded"`
}

func exportOf(r rows.File, name bool) exportFile {
	e := exportFile{
		ID:          FileID(r.ID).String(),
		Record:      exportRecord{Kind: r.RecordKind, ID: model.ID[fileTag](r.RecordID).String()},
		ContentType: r.ContentType,
		Size:        r.Size,
		SHA256:      hexOf(r.SHA256),
		CreatedAt:   r.CreatedAt,
	}
	if name {
		n := r.Name
		e.Name = &n
	}
	return e
}

// ExportPerson returns one JSON object {"about": [...], "uploaded": [...]}. "about" is every
// file whose subject is the person, with its name and content (base64); "uploaded" is the
// metadata of files the person uploaded about someone else or nobody, and carries a name
// only when the file has no subject and is public or internal (decision 27). It runs in no
// transaction and records no audit entry (people audits the export). It fails errs.Invalid
// for a zero person and errs.Unavailable (wrapping objects.ErrMissing when an object is
// missing) rather than ship an incomplete export.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error) {
	const op = "files.Service.ExportPerson"
	if person.IsZero() {
		return nil, fieldErr(op, errs.Invalid, "person", codeRequired, nil, "must be set")
	}
	doc := exportDoc{About: []exportFile{}, Uploaded: []exportFile{}}
	var afterAt time.Time
	var afterID [16]byte
	for {
		batch, err := s.store.ExportAbout(ctx, person, afterAt, afterID, exportBatch)
		if err != nil {
			return nil, fmt.Errorf("%s: about: %w", op, err)
		}
		for _, r := range batch {
			e := exportOf(r, true)
			if e.Content, err = s.readAll(ctx, op, r); err != nil {
				return nil, err
			}
			doc.About = append(doc.About, e)
		}
		if len(batch) < exportBatch {
			break
		}
		afterAt, afterID = batch[len(batch)-1].CreatedAt, batch[len(batch)-1].ID
	}
	afterAt, afterID = time.Time{}, [16]byte{}
	for {
		batch, err := s.store.ExportUploaded(ctx, person, afterAt, afterID, exportBatch)
		if err != nil {
			return nil, fmt.Errorf("%s: uploaded: %w", op, err)
		}
		for _, r := range batch {
			named := r.Subject == [16]byte{} && r.Level <= int16(model.SensitivityInternal)
			doc.Uploaded = append(doc.Uploaded, exportOf(r, named))
		}
		if len(batch) < exportBatch {
			break
		}
		afterAt, afterID = batch[len(batch)-1].CreatedAt, batch[len(batch)-1].ID
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, errs.E(op, errs.Internal, fmt.Errorf("encode the export: %w", err))
	}
	return out, nil
}

// readAll reads a file's object whole.
func (s *Service) readAll(ctx context.Context, op string, r rows.File) (data []byte, err error) {
	if want := s.objects.Kind().String(); r.Store != want {
		return nil, errs.E(op, errs.Unavailable, fmt.Errorf("a file is stored in %s and this process is configured for %s", r.Store, want))
	}
	obj, err := s.objects.Get(ctx, objects.Bucket(r.Bucket), objects.Key(r.ObjectKey))
	if err != nil {
		return nil, fmt.Errorf("%s: read an object: %w", op, err)
	}
	defer func() { err = errors.Join(err, obj.Body.Close()) }()
	data, err = io.ReadAll(io.LimitReader(obj.Body, r.Size+1))
	if err != nil {
		if k, ok := ctxKind(err); ok {
			return nil, errs.E(op, k, err)
		}
		return nil, errs.E(op, errs.Unavailable, fmt.Errorf("read an object: %w", err))
	}
	return data, nil
}

// DeletePerson runs inside people's Write transaction. It clears the uploader of every file
// the person uploaded (counted as Anonymized) and refuses with errs.Conflict (code in_use,
// owners and counts in the message) when a file still has the person as its subject: the
// owner forgot to delete it with DeleteByRecord, and people rolls the whole deletion back.
// Deleted is always 0. It fails errs.Invalid for a zero person, errs.Internal outside a Write
// transaction and errs.Unavailable wrapping db.ErrReadOnly on a read-only copy.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (Deletion, error) {
	const op = "files.Service.DeletePerson"
	if person.IsZero() {
		return Deletion{}, fieldErr(op, errs.Invalid, "person", codeRequired, nil, "must be set")
	}
	if err := needWriteTx(ctx, op); err != nil {
		return Deletion{}, err
	}
	n, err := s.store.AnonymiseUploads(ctx, person)
	if err != nil {
		return Deletion{}, fmt.Errorf("%s: anonymise uploads: %w", op, err)
	}
	left, err := s.store.FilesAboutPerson(ctx, person)
	if err != nil {
		return Deletion{}, fmt.Errorf("%s: count files: %w", op, err)
	}
	if len(left) > 0 {
		sort.Slice(left, func(i, j int) bool { return left[i].Owner < left[j].Owner })
		parts := make([]string, len(left))
		for i, c := range left {
			parts[i] = c.Owner + ": " + strconv.FormatInt(c.Files, 10)
		}
		return Deletion{}, fieldErr(op, errs.Conflict, "files", codeInUse, nil,
			"files about the person remain; their owners must delete them first (%s)", strings.Join(parts, ", "))
	}
	return Deletion{Anonymized: n}, nil
}
