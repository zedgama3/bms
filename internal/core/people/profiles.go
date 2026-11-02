package people

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// personalFields are the Profile.Fields names at the personal level, in display order.
var personalFields = []string{FieldLegalName, FieldPhone, FieldContactEmail, FieldEmail, FieldEmergencyContacts}

// fieldLevel is the sensitivity of each profile field a change can name.
func fieldLevel(field string) model.Sensitivity {
	switch field {
	case FieldPlayaName, FieldPronouns, FieldLanguage:
		return model.SensitivityInternal
	default:
		return model.SensitivityPersonal
	}
}

// highest returns the highest sensitivity among fields (internal for none).
func highest(fields []string) model.Sensitivity {
	level := model.SensitivityInternal
	for _, f := range fields {
		level = max(level, fieldLevel(f))
	}
	return level
}

// actorPerson is actor with the requirement that a person is signed in (a session always
// carries one).
func (s *Service) actorPerson(ctx context.Context, op string) (authz.Actor, error) {
	a, err := s.actor(ctx, op)
	if err != nil {
		return authz.Actor{}, err
	}
	if a.Person.IsZero() {
		return authz.Actor{}, errs.E(op, errs.Unauthenticated, errors.New("no signed-in person"))
	}
	return a, nil
}

// ---- LegalNamesOf -----------------------------------------------------------------------

// LegalNamesOf returns the legal name of each person among ids who has one; unknown IDs
// and people with none are omitted and not audited (F69). It needs people.contacts.read in
// any extent (F67) and answers only for the people that extent reaches (decision 9, D45):
// everyone for a site extent; for a department-scoped one nobody while no people.membership
// provider is registered (an empty map for others, no audit, not an error: F73) and
// otherwise the members the providers report; the module that asks decides whom it shows.
// Reach never excludes the actor (D46): their own name is answered whenever they have one.
// One people.person.read (Fields [legal_name], Level personal) is recorded per returned
// person other than the actor, before the map is returned (decision 15: a self read is not
// audited). When the regional's legal-name mode is hidden it returns an empty
// map without reading or auditing. It is for modules and is never on a route. Fails
// errs.Unauthenticated without a signed-in person, errs.Permission (F67), errs.Invalid
// (ids, too_many {max}) for more than MaxNames IDs (F68), the settings source's kinds, audit's
// kinds and the store's.
func (s *Service) LegalNamesOf(ctx context.Context, ids []model.PersonID) (_ map[model.PersonID]string, err error) {
	const op = "people.Service.LegalNamesOf"
	ctx, span := s.start(ctx, "legal-names-of")
	defer endSpan(span, &err)
	actor, err := s.actorPerson(ctx, op)
	if err != nil {
		return nil, err
	}
	if err := s.requireAnywhere(ctx, op, PermissionContactsRead); err != nil {
		return nil, err
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	if pol.LegalName == FieldHidden {
		// Hidden mode hides the field and stops accepting it; stored values stay but are
		// not shown to anyone, so nothing is read and nothing is audited.
		return map[model.PersonID]string{}, nil
	}
	if len(ids) > MaxNames {
		return nil, paramInvalid(op, "ids", codeTooMany, map[string]string{"max": fmt.Sprint(MaxNames)},
			fmt.Sprintf("at most %d IDs", MaxNames))
	}
	unique := make([]model.PersonID, 0, len(ids))
	seen := make(map[model.PersonID]struct{}, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup || id.IsZero() {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	span.SetAttrs(telemetry.Int("ids", len(unique)))
	if len(unique) == 0 {
		return map[model.PersonID]string{}, nil
	}
	// Answer only for the people the actor's extent reaches (D45), and always for the actor
	// themselves (D46): a department-scoped holder reaches nobody while no people.membership
	// provider is registered, which is an empty answer for others, not a Permission error
	// (the permission is held).
	r, err := s.reachOf(ctx, PermissionContactsRead, unique)
	if err != nil {
		return nil, wrap(op, err)
	}
	if !r.all {
		unique = slices.DeleteFunc(unique, func(id model.PersonID) bool { return id != actor.Person && !r.has(id) })
		if len(unique) == 0 {
			return map[model.PersonID]string{}, nil
		}
	}
	names, err := s.store.LegalNamesOf(ctx, unique)
	if err != nil {
		return nil, wrap(op, err)
	}
	read := make([]model.PersonID, 0, len(names))
	out := make(map[model.PersonID]string, len(names))
	for id, name := range names {
		if name == "" {
			continue
		}
		out[id] = name
		if id != actor.Person { // a self read is not audited (decision 15)
			read = append(read, id)
		}
	}
	slices.SortFunc(read, func(a, b model.PersonID) int { return bytes.Compare(a[:], b[:]) })
	for chunk := range slices.Chunk(read, audit.MaxBatch) {
		entries := make([]audit.Entry, len(chunk))
		for i, id := range chunk {
			entries[i] = audit.Entry{
				Action: actionRead, Outcome: audit.Succeeded, Subject: id, Record: audit.RefOf(kindPerson, id),
				Fields: []string{FieldLegalName}, Level: model.SensitivityPersonal,
			}
		}
		if err := s.record(ctx, entries...); err != nil {
			return nil, err
		}
	}
	span.SetAttrs(telemetry.Int("returned", len(out)))
	return out, nil
}

// ---- building a profile -----------------------------------------------------------------

// perms are the permissions of the viewer that shape a profile.
type perms struct {
	contacts, manage, export, del, grants, auditSearch bool
}

// viewerPerms asks the authorizer for what a profile page shows. For the person's own
// profile only the permissions that are not implied by being the person are asked. For
// someone else's, the contacts permission counts only when its extent reaches person (D44:
// a department-scoped extent reaches that department's members once a module contributes
// people.membership).
func (s *Service) viewerPerms(ctx context.Context, self bool, person model.PersonID) (p perms, err error) {
	if p.manage, err = s.holds(ctx, PermissionProfilesManage); err != nil {
		return perms{}, err
	}
	if p.auditSearch, err = s.holds(ctx, audit.PermissionSearch); err != nil {
		return perms{}, err
	}
	if self {
		return p, nil
	}
	r, err := s.reachOf(ctx, PermissionContactsRead, []model.PersonID{person})
	if err != nil {
		return perms{}, err
	}
	p.contacts = r.has(person)
	if p.export, err = s.holds(ctx, PermissionDataExport); err != nil {
		return perms{}, err
	}
	if p.del, err = s.holds(ctx, PermissionDataDelete); err != nil {
		return perms{}, err
	}
	if p.grants, err = s.anywhere(ctx, authz.PermissionGrantsRead); err != nil {
		return perms{}, err
	}
	return p, nil
}

// complete says whether a row has the playa name and every field the policy requires.
func complete(r rows.Person, pol Policy) bool {
	return r.PlayaName != "" &&
		(pol.LegalName != FieldRequired || r.LegalName != "") &&
		(pol.Phone != FieldRequired || r.Phone != "")
}

// profileView is who is looking at which profile.
type profileView struct {
	person model.PersonID
	self   bool
	perms  perms
	pol    Policy
}

// buildProfile assembles the profile of row for the viewer: the fields it may see, the
// contacts, consents and module sections, and what it may do. exists is false for a person
// with no row yet (Me). A read of personal fields about someone else is recorded before
// the profile is returned.
func (s *Service) buildProfile(ctx context.Context, op string, v profileView, row rows.Person, exists bool) (Profile, error) {
	if !exists {
		row = rows.Person{ID: v.person}
	}
	email, err := s.accounts.EmailOf(ctx, v.person)
	hasAccount := err == nil
	if err != nil && !errs.Is(err, errs.NotFound) {
		return Profile{}, wrap(op, err)
	}
	personal := v.self || v.perms.contacts
	p := Person{
		ID: row.ID, PlayaName: row.PlayaName, Pronouns: row.Pronouns, Language: row.Language,
		HasAccount: hasAccount, Complete: complete(row, v.pol), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	fields := []string{FieldPlayaName, FieldPronouns, FieldLanguage}
	if personal {
		if v.pol.LegalName != FieldHidden {
			p.LegalName = row.LegalName
			fields = append(fields, FieldLegalName)
		}
		if v.pol.Phone != FieldHidden {
			p.Phone = row.Phone
			fields = append(fields, FieldPhone)
		}
		if hasAccount {
			p.Email = email
			fields = append(fields, FieldEmail)
		} else {
			p.ContactEmail = model.Email(row.ContactEmail)
			fields = append(fields, FieldContactEmail)
		}
	}
	prof := Profile{Person: p, Policy: v.pol, Can: s.capabilities(v, hasAccount)}
	if personal && v.pol.EmergencyContacts != FieldHidden {
		fields = append(fields, FieldEmergencyContacts)
		if exists {
			cs, err := s.store.Contacts(ctx, v.person)
			if err != nil {
				return Profile{}, wrap(op, err)
			}
			prof.Contacts = contactsOf(cs)
		}
	}
	if (v.self || v.perms.manage) && exists {
		fields = append(fields, FieldConsents)
		cs, err := s.store.Consents(ctx, v.person)
		if err != nil {
			return Profile{}, wrap(op, err)
		}
		prof.Consents = consentsOf(cs)
	} else if v.self || v.perms.manage {
		fields = append(fields, FieldConsents)
	}
	prof.Fields = fields
	if !v.self {
		if err := s.auditPersonalRead(ctx, v.person, fields); err != nil {
			return Profile{}, err
		}
	}
	if exists {
		if prof.Sections, err = s.sections(ctx, v.person); err != nil {
			return Profile{}, wrap(op, err)
		}
	}
	return prof, nil
}

// capabilities says what the viewer may do on this profile.
func (s *Service) capabilities(v profileView, hasAccount bool) Capabilities {
	return Capabilities{
		Edit:         v.self || v.perms.manage,
		ReadContacts: v.self || v.perms.contacts,
		Export:       v.self || v.perms.export,
		Delete:       !v.self && v.perms.del,
		Link:         v.perms.manage && !hasAccount,
		ViewRoles:    v.self || v.perms.grants,
		ViewAudit:    v.perms.auditSearch,
	}
}

// auditPersonalRead records people.person.read for the personal groups among fields, if
// there are any: the read of someone else's personal fields.
func (s *Service) auditPersonalRead(ctx context.Context, person model.PersonID, fields []string) error {
	var read []string
	for _, f := range fields {
		if slices.Contains(personalFields, f) {
			read = append(read, f)
		}
	}
	if len(read) == 0 {
		return nil
	}
	return s.record(ctx, audit.Entry{
		Action: actionRead, Outcome: audit.Succeeded, Subject: person, Record: audit.RefOf(kindPerson, person),
		Fields: read, Level: model.SensitivityPersonal,
	})
}

func contactsOf(cs []rows.Contact) []EmergencyContact {
	out := make([]EmergencyContact, len(cs))
	for i, c := range cs {
		out[i] = contactOf(c)
	}
	return out
}

func contactOf(c rows.Contact) EmergencyContact {
	return EmergencyContact{
		ID: c.ID, Person: c.Person, Name: c.Name, Phone: c.Phone, Relationship: c.Relationship,
		Position: c.Position, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func consentsOf(cs []rows.Consent) []Consent {
	out := make([]Consent, len(cs))
	for i, c := range cs {
		out[i] = consentOf(c)
	}
	return out
}

func consentOf(c rows.Consent) Consent {
	return Consent{Person: c.Person, Purpose: Purpose(c.Purpose), Granted: c.Granted, RecordedBy: c.RecordedBy, ChangedAt: c.ChangedAt}
}

// ---- sections ---------------------------------------------------------------------------

// sections asks every provider for the person's sections, in build order, one at a time,
// each under SectionTimeout. A provider that fails or times out yields one Failed section
// and never fails the page (F48); a done ctx does.
func (s *Service) sections(ctx context.Context, person model.PersonID) ([]personpage.Section, error) {
	const op = "people.Service.sections"
	st, _ := s.links()
	var out []personpage.Section
	for _, sp := range st.sections {
		secs, err := s.callProvider(ctx, sp, person)
		if cerr := ctx.Err(); cerr != nil {
			return nil, errs.E(op, errs.KindOf(cerr), cerr)
		}
		if err != nil {
			s.metrics.section(ctx, sp.Module, outcomeFailed)
			s.logger.WarnContext(ctx, "person-page section failed",
				slog.String("module", sp.Module), slog.String("kind", errs.KindOf(err).String()),
				slog.Any("person", Person{ID: person}))
			out = append(out, personpage.Section{Key: sp.Module + ".unavailable", Failed: true})
			continue
		}
		s.metrics.section(ctx, sp.Module, outcomeOK)
		out = append(out, secs...)
	}
	return out, nil
}

// callProvider runs one provider under SectionTimeout (a context deadline, so a provider
// that honours its ctx stops when the page gives up on it).
func (s *Service) callProvider(ctx context.Context, sp SectionProvider, person model.PersonID) (_ []personpage.Section, err error) {
	const op = "people.Service.callProvider"
	ctx, span := s.start(ctx, "section")
	defer endSpan(span, &err)
	span.SetAttrs(telemetry.Module(sp.Module), telemetry.ID("person_id", person))

	pctx, cancel := context.WithTimeout(ctx, SectionTimeout)
	defer cancel()
	m, err := sp.Provider.Sections(pctx, []model.PersonID{person})
	if err == nil && errors.Is(pctx.Err(), context.DeadlineExceeded) {
		err = errs.E(op, errs.Timeout, errors.New("the provider took too long"))
	}
	if err != nil {
		return nil, wrap(op, err)
	}
	return m[person], nil
}

// ---- Me, Get ----------------------------------------------------------------------------

// Me returns the actor's own profile with every field group, their contacts, consents and
// the modules' sections. A person with no row yet gets an empty, incomplete Person{ID}
// and nothing is written (F4). On a writable copy, when the actor is not the superuser and
// the row holds no playa name, Me records the first-sign-in prompt once per session in a
// short Write transaction of its own and reports it in FirstPrompt (F91 to F100); on a
// read-only copy it never prompts and does not fail (F96). Fails errs.Unauthenticated (F3)
// and the store's kinds (F97). The mark is written after the profile is built, so a failed
// build leaves the prompt for the next call.
func (s *Service) Me(ctx context.Context) (_ Profile, err error) {
	const op = "people.Service.Me"
	ctx, span := s.start(ctx, "me")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Profile{}, err
	}
	span.SetAttrs(telemetry.ID("person_id", a.Person))
	prof, err := s.profileOf(ctx, op, a, a.Person, true)
	if err != nil {
		return Profile{}, err
	}
	span.SetAttrs(telemetry.Bool("first_prompt", prof.FirstPrompt))
	return prof, nil
}

// profileOf builds the profile of a self view: the row is read, a missing one is an empty
// profile. With prompt set (Me only) it then decides and records the first-sign-in prompt.
func (s *Service) profileOf(ctx context.Context, op string, a authz.Actor, person model.PersonID, prompt bool) (Profile, error) {
	pol, err := s.policy(ctx)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	p, err := s.viewerPerms(ctx, true, a.Person)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	row, err := s.store.GetPerson(ctx, person)
	exists := err == nil
	if err != nil && !errs.Is(err, errs.NotFound) {
		return Profile{}, wrap(op, err)
	}
	var (
		due    bool
		issued time.Time
	)
	if prompt {
		if due, issued, err = s.promptDue(ctx, op, a, row, exists); err != nil {
			return Profile{}, err
		}
	}
	prof, err := s.buildProfile(ctx, op, profileView{person: a.Person, self: true, perms: p, pol: pol}, row, exists)
	if err != nil {
		return Profile{}, err
	}
	// The mark is written only after the profile was built: a failed build leaves no mark,
	// so the session's next /me still prompts.
	if due {
		if prof.FirstPrompt, err = s.markPrompted(ctx, op, a.Person, issued, exists); err != nil {
			return Profile{}, err
		}
	}
	return prof, nil
}

// promptDue is the cheap, write-free part of the first-sign-in decision (decisions 34 to
// 36): due is false for the superuser, for a row that holds a playa name and on a read-only
// copy; otherwise it is true and issued is the session's IssuedAt. A signed-in actor
// without a session in ctx is Internal: the wiring is broken.
func (s *Service) promptDue(ctx context.Context, op string, a authz.Actor, row rows.Person, exists bool) (due bool, issued time.Time, _ error) {
	if a.Superuser || (exists && row.PlayaName != "") || s.db.ReadOnly() {
		return false, time.Time{}, nil
	}
	sess, ok := s.sessionFn(ctx)
	if !ok {
		return false, time.Time{}, errs.E(op, errs.Internal, errors.New("the actor has no session to date the prompt by"))
	}
	return true, sess.IssuedAt.UTC(), nil
}

// markPrompted records the prompt once the profile is built. A person with no row yet is
// prompted without writing (decision 2: their first save creates the row). Otherwise
// MarkNamePrompted runs in a Write transaction of its own (never inside a read) and the
// statement is the one decider: true exactly when it updated the row. db.ErrReadOnly means
// "cannot record" and is false and not an error; any other failure fails the call with its
// kind.
func (s *Service) markPrompted(ctx context.Context, op string, person model.PersonID, issued time.Time, exists bool) (bool, error) {
	if !exists {
		return true, nil
	}
	var marked bool
	err := s.inTx(ctx, db.Write, func(ctx context.Context) error {
		var err error
		marked, err = s.store.MarkNamePrompted(ctx, person, issued, s.now())
		return err //nolint:wrapcheck // wrapped below, with the transaction's own errors
	})
	if err != nil {
		if errors.Is(err, db.ErrReadOnly) {
			return false, nil
		}
		return false, wrap(op, err)
	}
	return marked, nil
}

// Get returns another person's profile: internal fields for a holder of
// people.directory.read in any extent, plus personal fields and contacts for a holder of
// people.contacts.read, plus consents for a holder of people.profiles.manage. Your own ID
// behaves as Me. A read that included personal fields is recorded before it returns (F13).
// Fails errs.Unauthenticated, errs.Permission (F10), errs.NotFound (F11) and store kinds.
func (s *Service) Get(ctx context.Context, person model.PersonID) (_ Profile, err error) {
	const op = "people.Service.Get"
	ctx, span := s.start(ctx, "get")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Profile{}, err
	}
	if person.IsZero() {
		return Profile{}, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	if a.Person == person {
		return s.profileOf(ctx, op, a, person, false)
	}
	if err := s.requireAnywhere(ctx, op, PermissionDirectoryRead); err != nil {
		return Profile{}, err
	}
	// A department-scoped extent reaches only that department's members once a module
	// contributes people.membership (F71). A person outside it is answered NotFound, never
	// Permission, so the answer does not confirm the ID exists.
	r, err := s.reachOf(ctx, PermissionDirectoryRead, []model.PersonID{person})
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	if !r.has(person) {
		return Profile{}, errs.E(op, errs.NotFound, errors.New("no such person"))
	}
	return s.otherProfile(ctx, op, person, func(ctx context.Context) (rows.Person, error) {
		return s.store.GetPerson(ctx, person)
	})
}

// otherProfile builds the profile of someone else for the actor.
func (s *Service) otherProfile(ctx context.Context, op string, person model.PersonID, load func(context.Context) (rows.Person, error)) (Profile, error) {
	pol, err := s.policy(ctx)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	p, err := s.viewerPerms(ctx, false, person)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	row, err := load(ctx)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	return s.buildProfile(ctx, op, profileView{person: person, self: false, perms: p, pol: pol}, row, true)
}

// ---- changing a profile -----------------------------------------------------------------

// checkPolicy applies the regional's modes to the input: a hidden field in the body is
// Invalid "hidden"; with bound set (the person's own save) an explicit empty value for a
// required field is Invalid "required".
func checkPolicy(op string, pol Policy, f profileFields, bound bool) error {
	v := validate.Errors{Op: op}
	hide := func(field string, p *string, mode FieldMode) {
		if p != nil && mode == FieldHidden {
			v.Addf(field, codeHidden, "this field is turned off")
		}
	}
	hide(FieldLegalName, f.legalName, pol.LegalName)
	hide(FieldPhone, f.phone, pol.Phone)
	if bound {
		req := func(field string, p *string, required bool) {
			if p != nil && required && *p == "" {
				v.Addf(field, codeRequired, "must not be empty")
			}
		}
		req(FieldPlayaName, f.playaName, true)
		req(FieldLegalName, f.legalName, pol.LegalName == FieldRequired)
		req(FieldPhone, f.phone, pol.Phone == FieldRequired)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// patchOf turns the provided fields into a patch against cur and the names that change.
func patchOf(id model.PersonID, cur rows.Person, f profileFields) (rows.PersonPatch, []string) {
	patch := rows.PersonPatch{ID: id}
	var changed []string
	pick := func(field string, in *string, was string, dst **string) {
		if in != nil && *in != was {
			*dst = in
			changed = append(changed, field)
		}
	}
	pick(FieldPlayaName, f.playaName, cur.PlayaName, &patch.PlayaName)
	if patch.PlayaName != nil {
		key := NameKey(*patch.PlayaName)
		patch.PlayaNameKey = &key
	}
	pick(FieldPronouns, f.pronouns, cur.Pronouns, &patch.Pronouns)
	pick(FieldLanguage, f.language, cur.Language, &patch.Language)
	pick(FieldLegalName, f.legalName, cur.LegalName, &patch.LegalName)
	pick(FieldPhone, f.phone, cur.Phone, &patch.Phone)
	pick(FieldContactEmail, f.contactEmail, cur.ContactEmail, &patch.ContactEmail)
	return patch, changed
}

// merged is cur with the patch applied: the row after the write.
func merged(cur rows.Person, patch rows.PersonPatch) rows.Person {
	set := func(dst *string, p *string) {
		if p != nil {
			*dst = *p
		}
	}
	set(&cur.PlayaName, patch.PlayaName)
	set(&cur.PlayaNameKey, patch.PlayaNameKey)
	set(&cur.Pronouns, patch.Pronouns)
	set(&cur.Language, patch.Language)
	set(&cur.LegalName, patch.LegalName)
	set(&cur.Phone, patch.Phone)
	set(&cur.ContactEmail, patch.ContactEmail)
	return cur
}

// reasonRename is the audit Reason of a change of a set playa name by a profile manager
// (decision 30).
const reasonRename = "rename"

// renames reports whether patch changes the set playa name of cur to one with another key
// (a rename), as against a first name or a respelling with the same key. cur.PlayaNameKey
// is the stored key, so a backfilled row compares against what the index holds.
func renames(cur rows.Person, patch rows.PersonPatch) bool {
	return patch.PlayaName != nil && patch.PlayaNameKey != nil &&
		cur.PlayaName != "" && *patch.PlayaNameKey != cur.PlayaNameKey
}

// requiredAfter fails errs.Invalid (field "required") for each required field that is
// empty in the row a save would leave (F7).
func requiredAfter(op string, pol Policy, r rows.Person) error {
	v := validate.Errors{Op: op}
	if r.PlayaName == "" {
		v.Addf(FieldPlayaName, codeRequired, "must not be empty")
	}
	if pol.LegalName == FieldRequired && r.LegalName == "" {
		v.Addf(FieldLegalName, codeRequired, "must not be empty")
	}
	if pol.Phone == FieldRequired && r.Phone == "" {
		v.Addf(FieldPhone, codeRequired, "must not be empty")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// checkContactEmail refuses a contact address for a person who has an account (F9).
func (s *Service) checkContactEmail(ctx context.Context, op string, person model.PersonID, f profileFields) error {
	if f.contactEmail == nil || *f.contactEmail == "" {
		return nil
	}
	_, err := s.accounts.EmailOf(ctx, person)
	switch {
	case err == nil:
		return fieldInvalid(op, FieldContactEmail, codeHasAccount, "a person with an account is reached at the login address")
	case errs.Is(err, errs.NotFound):
		return nil
	default:
		return wrap(op, err)
	}
}

// UpdateMe changes the actor's own profile. Nil fields are left alone. The playa name and
// every field the policy requires must be non-empty after the update (not checked when the
// body sets only the language); a hidden field in the input is refused. A missing row is created (the repair of decision 2). Records
// people.person.changed (people.person.created and the people.person-created signal when
// the row was missing) in the transaction. The repair takes the person's deletion lock and
// fails errs.Unauthenticated when the account no longer exists. Once the row holds a playa
// name, the same name again writes nothing, a respelling with the same NameKey is saved,
// and any other name fails errs.Invalid (field playa_name, code permanent); a name whose
// NameKey another person holds fails errs.Conflict (field playa_name, code taken) with
// nothing audited or published. Fails errs.Unauthenticated, errs.Invalid (F5 to F9, F75),
// errs.Conflict (F74), errs.Unavailable wrapping db.ErrReadOnly and store kinds.
func (s *Service) UpdateMe(ctx context.Context, in ProfileInput) (_ Profile, err error) {
	return s.updateMe(ctx, "people.Service.UpdateMe", "update-me", in, false)
}

// updateMe is UpdateMe; manager says the actor holds people.profiles.manage and may rename
// their own set playa name (Update on your own ID, decision 30).
func (s *Service) updateMe(ctx context.Context, op, spanName string, in ProfileInput, manager bool) (_ Profile, err error) {
	ctx, span := s.start(ctx, spanName)
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Profile{}, err
	}
	span.SetAttrs(telemetry.ID("person_id", a.Person))
	pol, err := s.policy(ctx)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	f, err := in.normalize(op)
	if err != nil {
		return Profile{}, err
	}
	if err := checkPolicy(op, pol, f, true); err != nil {
		return Profile{}, wrap(op, err)
	}
	if err := s.checkContactEmail(ctx, op, a.Person, f); err != nil {
		return Profile{}, err
	}
	var row rows.Person
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		cur, gerr := s.store.GetForUpdate(ctx, a.Person)
		missing := errs.Is(gerr, errs.NotFound)
		if gerr != nil && !missing {
			return wrap(op, gerr)
		}
		if missing {
			cur = rows.Person{ID: a.Person}
		}
		patch, changed := patchOf(a.Person, cur, f)
		if missing {
			// The repair creates the row, so it must not race a deletion of this person: take
			// the deletion lock and check under it that the account is still there, or a
			// save that overlaps a fulfilled deletion would re-insert the person.
			if err := s.store.LockDeletion(ctx, a.Person); err != nil {
				return wrap(op, err)
			}
			if _, err := s.accounts.EmailOf(ctx, a.Person); err != nil {
				if errs.Is(err, errs.NotFound) {
					return errs.E(op, errs.Unauthenticated, errors.New("the account of this session is gone"))
				}
				return wrap(op, err)
			}
		}
		// A save that sets only the language is exempt: the shell stores the language before
		// the person has filled in the rest (D37, F54).
		if !f.languageOnly() {
			if err := requiredAfter(op, pol, merged(cur, patch)); err != nil {
				return wrap(op, err)
			}
		}
		renamed := renames(cur, patch)
		if renamed && !manager {
			return fieldInvalid(op, FieldPlayaName, codePermanent, "a playa name cannot be changed once it is set")
		}
		if !missing && len(changed) == 0 {
			row = cur
			return nil
		}
		saved, inserted, err := s.store.Upsert(ctx, patch, s.now())
		if err != nil {
			return wrap(op, err)
		}
		row = saved
		return s.recordSave(ctx, a.Person, inserted, changed, renamed)
	})
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	p, err := s.viewerPerms(ctx, true, a.Person)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	return s.buildProfile(ctx, op, profileView{person: a.Person, self: true, perms: p, pol: pol}, row, true)
}

// recordSave records people.person.created (and publishes people.person-created) or
// people.person.changed for a profile write, inside the caller's transaction.
func (s *Service) recordSave(ctx context.Context, person model.PersonID, created bool, changed []string, renamed bool) error {
	e := audit.Entry{Action: actionChanged, Outcome: audit.Succeeded, Subject: person, Record: audit.RefOf(kindPerson, person)}
	if renamed {
		e.Reason = reasonRename
	}
	if created {
		e.Action = actionCreated
	}
	if len(changed) > 0 {
		e.Fields, e.Level = changed, highest(changed)
	}
	if err := s.record(ctx, e); err != nil {
		return err
	}
	if created {
		return s.publishPerson(ctx, SignalPersonCreated, person)
	}
	return nil
}

func (s *Service) publishPerson(ctx context.Context, name signals.Name, person model.PersonID) error {
	return s.publish(ctx, name, signals.Ref(PayloadPerson, person))
}

// Create makes a person without an account (a walk-on): playa name required, the other
// fields optional, the regional's required modes not enforced (decision 6). Needs
// people.profiles.manage. Records people.person.created and publishes
// people.person-created in the transaction. Fails errs.Permission (F14), errs.Invalid
// (F5, F6, F8) and store kinds.
func (s *Service) Create(ctx context.Context, in ProfileInput) (_ Profile, err error) {
	const op = "people.Service.Create"
	ctx, span := s.start(ctx, "create")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Profile{}, err
	}
	if err := s.require(ctx, op, PermissionProfilesManage); err != nil {
		return Profile{}, err
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	f, err := in.normalize(op)
	if err != nil {
		return Profile{}, err
	}
	if err := checkPolicy(op, pol, f, false); err != nil {
		return Profile{}, wrap(op, err)
	}
	if f.playaName == nil || *f.playaName == "" {
		return Profile{}, fieldInvalid(op, FieldPlayaName, codeRequired, "must not be empty")
	}
	id := model.NewID[model.PersonID]()
	span.SetAttrs(telemetry.ID("person_id", id))
	patch, changed := patchOf(id, rows.Person{ID: id}, f)
	var row rows.Person
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		in := rows.Person{ID: id, CreatedBy: a.Person}
		in = merged(in, patch)
		saved, inserted, err := s.store.InsertPerson(ctx, in, s.now())
		if err != nil {
			return wrap(op, err)
		}
		if !inserted {
			return errs.E(op, errs.Internal, errors.New("a freshly generated person ID already has a row"))
		}
		row = saved
		return s.recordSave(ctx, id, true, changed, false)
	})
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	return s.otherProfile(ctx, op, id, func(context.Context) (rows.Person, error) { return row, nil })
}

// Update changes another person's profile (a profile manager's edit): nil fields are left
// alone, required modes are not enforced (decision 6), a hidden field is refused. Your own
// ID behaves as UpdateMe, except that a holder of people.profiles.manage may rename their
// own set playa name. Needs people.profiles.manage. Records people.person.changed with the
// names that changed; a change of a set playa name to another NameKey carries Reason
// "rename". A set name cannot be cleared. Fails errs.Permission (F14), errs.NotFound (F11),
// errs.Invalid (F6, F8, F9, F79), errs.Conflict (field playa_name, code taken, F74) and
// store kinds.
func (s *Service) Update(ctx context.Context, person model.PersonID, in ProfileInput) (_ Profile, err error) {
	const op = "people.Service.Update"
	ctx, span := s.start(ctx, "update")
	defer endSpan(span, &err)
	a, err := s.actorPerson(ctx, op)
	if err != nil {
		return Profile{}, err
	}
	if person.IsZero() {
		return Profile{}, fieldInvalid(op, "person", codeRequired, "must be set")
	}
	span.SetAttrs(telemetry.ID("person_id", person))
	if a.Person == person {
		// Your own ID is UpdateMe, except that a profile manager may rename their own set
		// playa name (decision 30, F78).
		manager := false
		if in.PlayaName != nil {
			if manager, err = s.holds(ctx, PermissionProfilesManage); err != nil {
				return Profile{}, wrap(op, err)
			}
		}
		return s.updateMe(ctx, op, "update-me", in, manager)
	}
	if err := s.require(ctx, op, PermissionProfilesManage); err != nil {
		return Profile{}, err
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	f, err := in.normalize(op)
	if err != nil {
		return Profile{}, err
	}
	if err := checkPolicy(op, pol, f, false); err != nil {
		return Profile{}, wrap(op, err)
	}
	if err := s.checkContactEmail(ctx, op, person, f); err != nil {
		return Profile{}, err
	}
	var row rows.Person
	err = s.inTx(ctx, db.Write, func(ctx context.Context) error {
		cur, err := s.store.GetForUpdate(ctx, person)
		if err != nil {
			return wrap(op, err)
		}
		if f.playaName != nil && *f.playaName == "" && cur.PlayaName != "" {
			return fieldInvalid(op, FieldPlayaName, codeRequired, "a playa name cannot be cleared")
		}
		patch, changed := patchOf(person, cur, f)
		if len(changed) == 0 {
			row = cur
			return nil
		}
		if row, err = s.store.UpdatePerson(ctx, patch, s.now()); err != nil {
			return wrap(op, err)
		}
		return s.recordSave(ctx, person, false, changed, renames(cur, patch))
	})
	if err != nil {
		return Profile{}, wrap(op, err)
	}
	return s.otherProfile(ctx, op, person, func(context.Context) (rows.Person, error) { return row, nil })
}

// ---- search -----------------------------------------------------------------------------

// Search returns up to q.Limit people whose playa name contains q (case-insensitive),
// ordered by playa name. A holder of people.contacts.read in any extent also matches legal
// name, phone and contact address (each unless the regional hid the field), and every
// result that matched a personal field is recorded as people.search.read. Needs
// people.directory.read in any extent. Fails errs.Permission (F10), errs.Invalid (F15) and
// store kinds.
func (s *Service) Search(ctx context.Context, q SearchQuery) (_ []Match, err error) {
	const op = "people.Service.Search"
	ctx, span := s.start(ctx, "search")
	defer endSpan(span, &err)
	if _, err := s.actorPerson(ctx, op); err != nil {
		return nil, err
	}
	if err := s.requireAnywhere(ctx, op, PermissionDirectoryRead); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(q.Q)
	limit := q.Limit
	bad := validate.Errors{Op: op}
	switch {
	case len(text) < MinSearchBytes:
		bad.Add(paramInvalid(op, "q", codeTooShort, map[string]string{"min": fmt.Sprint(MinSearchBytes)},
			fmt.Sprintf("must be at least %d bytes", MinSearchBytes)))
	case len(text) > MaxSearchBytes:
		bad.Add(paramInvalid(op, "q", codeTooLong, map[string]string{"max": fmt.Sprint(MaxSearchBytes)},
			fmt.Sprintf("must be at most %d bytes", MaxSearchBytes)))
	}
	if limit < 0 || limit > MaxSearchResults {
		bad.Add(paramInvalid(op, "limit", codeRange,
			map[string]string{"min": "0", "max": fmt.Sprint(MaxSearchResults)}, "out of range"))
	}
	if err := bad.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	if limit == 0 {
		limit = DefaultSearchResults
	}
	contacts, err := s.anywhere(ctx, PermissionContactsRead)
	if err != nil {
		return nil, wrap(op, err)
	}
	pol, err := s.policy(ctx)
	if err != nil {
		return nil, wrap(op, err)
	}
	query := rows.SearchQuery{
		Q: text, Limit: limit,
		LegalName:    contacts && pol.LegalName != FieldHidden,
		Phone:        contacts && pol.Phone != FieldHidden,
		ContactEmail: contacts,
	}
	found, err := s.searchWithin(ctx, op, query)
	if err != nil {
		return nil, err
	}
	out := make([]Match, len(found))
	var entries []audit.Entry
	for i, m := range found {
		out[i] = Match{ID: m.ID, PlayaName: m.PlayaName, Pronouns: m.Pronouns}
		var matched []string
		if m.Legal {
			matched = append(matched, FieldLegalName)
		}
		if m.Phone {
			matched = append(matched, FieldPhone)
		}
		if m.Email {
			matched = append(matched, FieldContactEmail)
		}
		if len(matched) > 0 {
			entries = append(entries, audit.Entry{
				Action: actionSearchRead, Outcome: audit.Succeeded, Subject: m.ID, Record: audit.RefOf(kindPerson, m.ID),
				Fields: matched, Level: model.SensitivityPersonal,
			})
		}
	}
	span.SetAttrs(telemetry.Int("results", len(out)), telemetry.Int("personal_matches", len(entries)))
	if len(entries) > 0 {
		if err := s.record(ctx, entries...); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// searchWithin runs the store search and narrows it to what the actor's extents reach
// (D44). A site extent, or a department extent of people.directory.read while no module
// contributes people.membership (decision 9), changes nothing; a department extent of
// people.contacts.read then loses the personal columns (D45). Otherwise the store is asked for up to MaxSearchResults
// rows and the result keeps only the people the directory extent reaches; a match on a
// personal field counts only for a person the contacts extent reaches, so such a person
// appears only when their playa name matches too (found by a second, name-only query) and
// the personal flags are cleared. A directory that is larger than MaxSearchResults matches
// may therefore show fewer than Limit results, never more than the extent allows.
func (s *Service) searchWithin(ctx context.Context, op string, q rows.SearchQuery) ([]rows.Match, error) {
	st, _ := s.links()
	if len(st.membership) == 0 {
		// No provider can say who belongs to a department, so a department-scoped
		// people.contacts.read reaches nobody (D45) and the personal columns are not
		// searched for it; people.directory.read reaches everyone (decision 9).
		if q.LegalName || q.Phone || q.ContactEmail {
			conExt, err := s.authz.ExtentOf(ctx, PermissionContactsRead)
			if err != nil {
				return nil, wrap(op, err)
			}
			if !conExt.Site {
				q.LegalName, q.Phone, q.ContactEmail = false, false, false
			}
		}
		found, err := s.store.Search(ctx, q)
		return found, wrap(op, err)
	}
	dirExt, err := s.authz.ExtentOf(ctx, PermissionDirectoryRead)
	if err != nil {
		return nil, wrap(op, err)
	}
	conExt := authz.Extent{}
	personal := q.LegalName || q.Phone || q.ContactEmail
	if personal {
		if conExt, err = s.authz.ExtentOf(ctx, PermissionContactsRead); err != nil {
			return nil, wrap(op, err)
		}
	}
	if dirExt.Site && (!personal || conExt.Site) {
		found, err := s.store.Search(ctx, q)
		return found, wrap(op, err)
	}
	wide := q
	wide.Limit = MaxSearchResults
	found, err := s.store.Search(ctx, wide)
	if err != nil {
		return nil, wrap(op, err)
	}
	var byName map[model.PersonID]struct{}
	if personal && !conExt.Site {
		nameOnly := wide
		nameOnly.LegalName, nameOnly.Phone, nameOnly.ContactEmail = false, false, false
		named, err := s.store.Search(ctx, nameOnly)
		if err != nil {
			return nil, wrap(op, err)
		}
		byName = make(map[model.PersonID]struct{}, len(named))
		for _, m := range named {
			byName[m.ID] = struct{}{}
		}
	}
	ids := make([]model.PersonID, len(found))
	for i, m := range found {
		ids[i] = m.ID
	}
	dir, err := s.reachOf(ctx, PermissionDirectoryRead, ids)
	if err != nil {
		return nil, wrap(op, err)
	}
	var con reach
	if byName != nil {
		if con, err = s.reachOf(ctx, PermissionContactsRead, ids); err != nil {
			return nil, wrap(op, err)
		}
	}
	out := make([]rows.Match, 0, min(len(found), q.Limit))
	for _, m := range found {
		if !dir.has(m.ID) {
			continue
		}
		if byName != nil && !con.has(m.ID) {
			if _, named := byName[m.ID]; !named {
				continue
			}
			m.Legal, m.Phone, m.Email = false, false, false
		}
		out = append(out, m)
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}
