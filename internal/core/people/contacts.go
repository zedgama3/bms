package people

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/model"
)

var contactFields = []string{"name", "phone", "relationship"}

// contactSetup is the part every contact use case shares: the actor is the person or a
// profile manager, and the regional keeps emergency contacts (F21).
func (s *Service) contactSetup(ctx context.Context, op string, person model.PersonID) error {
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return err
	}
	if person.IsZero() {
		return fieldInvalid(op, "person", codeRequired, "must be set")
	}
	if _, err := s.selfOrManage(ctx, op, a, person); err != nil {
		return err
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return wrap(op, err)
	}
	if pol.EmergencyContacts == FieldHidden {
		return fieldInvalid(op, "contacts", codeHidden, "emergency contacts are turned off")
	}
	return nil
}

// AddContact appends an emergency contact to the person's list. The actor is the person or
// holds people.profiles.manage. Name and phone are required; at most MaxEmergencyContacts.
// Records people.contact.created in the transaction. Fails errs.Permission, errs.Invalid
// (F19, F20, F21), errs.NotFound (unknown person) and store kinds.
func (s *Service) AddContact(ctx context.Context, person model.PersonID, in ContactInput) (_ EmergencyContact, err error) {
	const op = "people.Service.AddContact"
	ctx, span := s.start(ctx, "add-contact")
	defer endSpan(span, &err)
	if err := s.contactSetup(ctx, op, person); err != nil {
		return EmergencyContact{}, err
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	c, err := in.normalize(op)
	if err != nil {
		return EmergencyContact{}, err
	}
	var out rows.Contact
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockPerson(ctx, person); err != nil {
			return wrap(op, err)
		}
		row, added, err := s.store.AddContact(ctx, rows.Contact{
			ID: model.NewID[ContactID](), Person: person, Name: c.Name, Phone: c.Phone,
			Relationship: c.Relationship, CreatedAt: s.now(),
		}, MaxEmergencyContacts)
		if err != nil {
			return wrap(op, err)
		}
		if !added {
			return paramInvalid(op, "contacts", codeTooMany, map[string]string{"max": fmt.Sprint(MaxEmergencyContacts)},
				fmt.Sprintf("at most %d emergency contacts", MaxEmergencyContacts))
		}
		out = row
		return s.record(ctx, audit.Entry{
			Action: actionContactCreated, Outcome: audit.Succeeded, Subject: person,
			Record: audit.RefOf(kindContact, row.ID), Fields: contactFields, Level: model.SensitivityPersonal,
		})
	})
	if err != nil {
		return EmergencyContact{}, wrap(op, err)
	}
	return contactOf(out), nil
}

// UpdateContact replaces the text of one of the person's contacts. Fails as AddContact
// does, and errs.NotFound when the contact is not the person's (F22). Records
// people.contact.changed with the fields that changed.
func (s *Service) UpdateContact(ctx context.Context, person model.PersonID, id ContactID, in ContactInput) (_ EmergencyContact, err error) {
	const op = "people.Service.UpdateContact"
	ctx, span := s.start(ctx, "update-contact")
	defer endSpan(span, &err)
	if err := s.contactSetup(ctx, op, person); err != nil {
		return EmergencyContact{}, err
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	c, err := in.normalize(op)
	if err != nil {
		return EmergencyContact{}, err
	}
	var out rows.Contact
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockPerson(ctx, person); err != nil {
			return wrap(op, err)
		}
		cur, err := s.store.GetContact(ctx, person, id)
		if err != nil {
			return wrap(op, err)
		}
		var changed []string
		if c.Name != cur.Name {
			changed = append(changed, "name")
		}
		if c.Phone != cur.Phone {
			changed = append(changed, "phone")
		}
		if c.Relationship != cur.Relationship {
			changed = append(changed, "relationship")
		}
		if len(changed) == 0 {
			out = cur
			return nil
		}
		out, err = s.store.UpdateContact(ctx, rows.Contact{
			ID: id, Person: person, Name: c.Name, Phone: c.Phone, Relationship: c.Relationship, UpdatedAt: s.now(),
		})
		if err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionContactChanged, Outcome: audit.Succeeded, Subject: person,
			Record: audit.RefOf(kindContact, id), Fields: changed, Level: model.SensitivityPersonal,
		})
	})
	if err != nil {
		return EmergencyContact{}, wrap(op, err)
	}
	return contactOf(out), nil
}

// RemoveContact deletes one of the person's contacts and closes the gap in the order.
// Fails as UpdateContact does. Records people.contact.deleted.
func (s *Service) RemoveContact(ctx context.Context, person model.PersonID, id ContactID) (err error) {
	const op = "people.Service.RemoveContact"
	ctx, span := s.start(ctx, "remove-contact")
	defer endSpan(span, &err)
	if err := s.contactSetup(ctx, op, person); err != nil {
		return err
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		if err := s.store.LockPerson(ctx, person); err != nil {
			return wrap(op, err)
		}
		if err := s.store.DeleteContact(ctx, person, id); err != nil {
			return wrap(op, err)
		}
		if err := s.store.Renumber(ctx, person); err != nil {
			return wrap(op, err)
		}
		return s.record(ctx, audit.Entry{
			Action: actionContactDeleted, Outcome: audit.Succeeded, Subject: person, Record: audit.RefOf(kindContact, id),
		})
	})
	if err != nil {
		return wrap(op, err)
	}
	return nil
}
