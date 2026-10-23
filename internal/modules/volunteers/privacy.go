package volunteers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// exportDoc is the shape of ExportPerson's data (module-volunteers.md, "Data").
type exportDoc struct {
	Memberships []exportMembership `json:"memberships"`
	Listed      *bool              `json:"listed,omitempty"` // present when the module holds anything about the person
	Signups     []exportSignup     `json:"signups"`
	Records     []exportRecord     `json:"qualification_records"`
	Verified    int                `json:"verified_for_others"`
	Created     int                `json:"shifts_created"`
}

type exportMembership struct {
	Department     string    `json:"department"`
	DepartmentName string    `json:"department_name"`
	Since          time.Time `json:"since"`
}

type exportSignup struct {
	Label        string     `json:"label"`
	Department   string     `json:"department"`
	Position     string     `json:"position"`
	Starts       time.Time  `json:"starts"`
	Ends         time.Time  `json:"ends"`
	Zone         string     `json:"zone"`
	Status       string     `json:"status"`
	BookedBy     string     `json:"booked_by"`
	Waived       bool       `json:"requirement_waived"`
	CanceledAt   *time.Time `json:"canceled_at,omitempty"`
	CanceledBy   string     `json:"canceled_by,omitempty"`
	CancelReason string     `json:"cancel_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type exportRecord struct {
	ID            string     `json:"id"`
	Qualification string     `json:"qualification"`
	AcquiredOn    string     `json:"acquired_on"`
	ExpiresOn     string     `json:"expires_on,omitempty"`
	Verified      bool       `json:"verified"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
	Certificate   string     `json:"certificate_file,omitempty"`
}

func checkPerson(op string, person model.PersonID) error {
	if person.IsZero() {
		acc := validate.Errors{Op: op}
		acc.Addf("person", validate.CodeRequired, "must be set")
		return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return nil
}

// ExportPerson returns everything the module holds about one person as one JSON object:
// memberships, whether they chose not to be listed (unlisted), sign-ups (who booked or canceled as role words, never
// names), qualification records (the certificate's file ID; files exports the bytes), and
// counts of records verified and shifts created for others. It runs as the system for
// people, which checks and audits the export. Fails errs.Invalid for a zero person and with
// store and Departments kinds.
func (s *Service) ExportPerson(ctx context.Context, person model.PersonID) (_ module.Export, err error) {
	const op = "volunteers.ExportPerson"
	if err := checkPerson(op, person); err != nil {
		return module.Export{}, err
	}
	ctx, span := s.begin(ctx, "volunteers.export-person")
	defer endSpan(span, &err)
	mem, err := s.store.MembershipsOf(ctx, person)
	if err != nil {
		return module.Export{}, wrap(op, err)
	}
	signups, err := s.store.ExportSignups(ctx, person)
	if err != nil {
		return module.Export{}, wrap(op, err)
	}
	records, err := s.store.ExportRecords(ctx, person)
	if err != nil {
		return module.Export{}, wrap(op, err)
	}
	listed, err := s.store.Listed(ctx, person)
	if err != nil {
		return module.Export{}, wrap(op, err)
	}
	verified, err := s.store.CountVerifiedBy(ctx, person)
	if err != nil {
		return module.Export{}, wrap(op, err)
	}
	created, err := s.store.CountShiftsCreatedBy(ctx, person)
	if err != nil {
		return module.Export{}, wrap(op, err)
	}
	ids := make([]model.DepartmentID, 0, len(mem)+len(signups))
	for _, m := range mem {
		ids = append(ids, m.Department)
	}
	for _, su := range signups {
		ids = append(ids, su.Department)
	}
	names := map[model.DepartmentID]string{}
	if len(ids) > 0 {
		for chunk := range chunked(ids, vol.MaxIDs) {
			m, nerr := s.depts.NamesOf(ctx, chunk)
			if nerr != nil {
				return module.Export{}, wrap(op, nerr)
			}
			for k, v := range m {
				names[k] = v
			}
		}
	}
	doc := exportDoc{
		Memberships: []exportMembership{}, Signups: []exportSignup{}, Records: []exportRecord{},
		Verified: verified, Created: created,
	}
	for _, m := range mem {
		doc.Memberships = append(doc.Memberships, exportMembership{
			Department: m.Department.String(), DepartmentName: names[m.Department], Since: m.CreatedAt,
		})
	}
	for _, su := range signups {
		e := exportSignup{
			Label: su.Label, Department: names[su.Department], Position: su.PositionName, Starts: su.Starts, Ends: su.Ends,
			Zone: su.Zone, Status: string(su.Status), BookedBy: su.BookedByRole, Waived: su.Waived,
			CanceledBy: su.CanceledByRole, CancelReason: string(su.CancelReason), CreatedAt: su.CreatedAt,
		}
		if !su.CanceledAt.IsZero() {
			t := su.CanceledAt
			e.CanceledAt = &t
		}
		doc.Signups = append(doc.Signups, e)
	}
	for _, r := range records {
		e := exportRecord{
			ID: r.ID.String(), Qualification: r.QualificationName, AcquiredOn: r.AcquiredOn.String(),
			ExpiresOn: r.ExpiresOn.String(), Verified: r.Verified,
		}
		if !r.VerifiedAt.IsZero() {
			t := r.VerifiedAt
			e.VerifiedAt = &t
		}
		if !r.Certificate.IsZero() {
			e.Certificate = r.Certificate.String()
		}
		doc.Records = append(doc.Records, e)
	}
	if listed && len(doc.Memberships)+len(doc.Signups)+len(doc.Records)+verified+created == 0 {
		// A person the module holds nothing about exports {} (F155): no listed key, no empty lists.
		return module.Export{Data: json.RawMessage(`{}`)}, nil
	}
	doc.Listed = &listed
	data, err := json.Marshal(doc)
	if err != nil {
		return module.Export{}, errs.E(op, errs.Internal, fmt.Errorf("encode export: %w", err))
	}
	return module.Export{Data: data}, nil
}

// DeletePerson deletes the person's sign-ups, memberships, listing row and records (with
// their certificate files), then nulls every who-did-it column that names them (booked_by,
// waived_by, canceled_by, verified_by, added_by, created_by), which it reports as Anonymized.
// Nothing is kept. It runs in the transaction people gave it and is idempotent. Fails
// errs.Invalid for a zero person, errs.Unavailable wrapping db.ErrReadOnly on a read-only
// copy, and files' and store kinds.
func (s *Service) DeletePerson(ctx context.Context, person model.PersonID) (_ module.Deletion, err error) {
	const op = "volunteers.DeletePerson"
	if err := checkPerson(op, person); err != nil {
		return module.Deletion{}, err
	}
	ctx, span := s.begin(ctx, "volunteers.delete-person")
	defer endSpan(span, &err)
	var out module.Deletion
	err = s.write(ctx, func(ctx context.Context) error {
		recs, rerr := s.store.RecordIDsOf(ctx, person)
		if rerr != nil {
			return fmt.Errorf("records: %w", rerr)
		}
		for _, r := range recs {
			if _, derr := s.files.DeleteByRecord(ctx, ref(kindRecord, r.ID)); derr != nil {
				return fmt.Errorf("delete certificate: %w", derr)
			}
		}
		for _, del := range []func(context.Context, model.PersonID) (int, error){
			s.store.DeleteSignupsByPerson, s.store.DeleteMembersByPerson,
			s.store.DeleteUnlistedByPerson, s.store.DeleteRecordsByPerson,
		} {
			n, derr := del(ctx, person)
			if derr != nil {
				return fmt.Errorf("delete: %w", derr)
			}
			out.Deleted += n
		}
		an, aerr := s.store.AnonymisePerson(ctx, person)
		if aerr != nil {
			return fmt.Errorf("anonymise: %w", aerr)
		}
		out.Anonymized = an.Records + an.Members + an.Signups + an.Shifts
		return nil
	})
	if err != nil {
		return module.Deletion{}, wrap(op, err)
	}
	return out, nil
}
