package people

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

// The JSON forms of the people API (openapi.json, schemas People*). IDs are canonical UUID
// strings and times RFC 3339 UTC. Personal strings the viewer may not see are left out.

type wirePerson struct {
	ID           model.PersonID `json:"id"`
	PlayaName    string         `json:"playa_name"`
	Pronouns     string         `json:"pronouns"`
	Language     string         `json:"language"`
	LegalName    string         `json:"legal_name,omitempty"`
	Phone        string         `json:"phone,omitempty"`
	ContactEmail string         `json:"contact_email,omitempty"`
	Email        string         `json:"email,omitempty"`
	HasAccount   bool           `json:"has_account"`
	Complete     bool           `json:"complete"`
	CreatedAt    time.Time      `json:"created_at,omitzero"`
	UpdatedAt    time.Time      `json:"updated_at,omitzero"`
}

type wireContact struct {
	ID           ContactID      `json:"id"`
	PersonID     model.PersonID `json:"person_id"`
	Name         string         `json:"name"`
	Phone        string         `json:"phone"`
	Relationship string         `json:"relationship"`
	Position     int            `json:"position"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type wireConsent struct {
	PersonID   model.PersonID `json:"person_id"`
	Purpose    Purpose        `json:"purpose"`
	Granted    bool           `json:"granted"`
	RecordedBy model.PersonID `json:"recorded_by,omitzero"`
	ChangedAt  time.Time      `json:"changed_at"`
}

type wireItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Href  string `json:"href,omitempty"`
}

type wireSection struct {
	Key    string     `json:"key"`
	Items  []wireItem `json:"items"`
	Failed bool       `json:"failed"`
}

type wireCan struct {
	Edit         bool `json:"edit"`
	ReadContacts bool `json:"read_contacts"`
	Export       bool `json:"export"`
	Delete       bool `json:"delete"`
	Link         bool `json:"link"`
	ViewRoles    bool `json:"view_roles"`
	ViewAudit    bool `json:"view_audit"`
}

type wirePolicy struct {
	LegalName         string `json:"legal_name"`
	Phone             string `json:"phone"`
	EmergencyContacts string `json:"emergency_contacts"`
}

type wireProfile struct {
	Person   wirePerson    `json:"person"`
	Fields   []string      `json:"fields"`
	Contacts []wireContact `json:"contacts"`
	Consents []wireConsent `json:"consents"`
	Sections []wireSection `json:"sections"`
	Can      wireCan       `json:"can"`
	Policy   wirePolicy    `json:"policy"`
	// FirstPrompt is internal and only ever the viewer's own: true on the one GET /me that
	// recorded the first-sign-in prompt of this session, absent otherwise (decision 35).
	FirstPrompt bool `json:"first_prompt,omitempty"`
}

type wireProfileInput struct {
	PlayaName    *string `json:"playa_name"`
	Pronouns     *string `json:"pronouns"`
	Language     *string `json:"language"`
	LegalName    *string `json:"legal_name"`
	Phone        *string `json:"phone"`
	ContactEmail *string `json:"contact_email"`
}

type wireContactInput struct {
	Name         string `json:"name"`
	Phone        string `json:"phone"`
	Relationship string `json:"relationship"`
}

type wireConsentInput struct {
	Granted bool `json:"granted"`
}

type wireMatch struct {
	ID        model.PersonID `json:"id"`
	PlayaName string         `json:"playa_name"`
	Pronouns  string         `json:"pronouns"`
}

type wireRequest struct {
	ID           RequestID      `json:"id"`
	PersonID     model.PersonID `json:"person_id"`
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	Note         string         `json:"note"`
	CreatedAt    time.Time      `json:"created_at"`
	DecidedAt    time.Time      `json:"decided_at,omitzero"`
	DecidedBy    model.PersonID `json:"decided_by,omitzero"`
	DecisionNote string         `json:"decision_note"`
}

type wireRequestInput struct {
	Kind string `json:"kind"`
	Note string `json:"note"`
}

type wireDecisionInput struct {
	Note string `json:"note"`
}

type wireLinkInput struct {
	Email string `json:"email"`
}

type wireLinkResult struct {
	AccountID     model.AccountID `json:"account_id"`
	OrphanID      model.PersonID  `json:"orphan_id,omitzero"`
	OrphanDeleted bool            `json:"orphan_deleted"`
}

type wireKept struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type wireModuleDeletion struct {
	Deleted    int        `json:"deleted"`
	Anonymized int        `json:"anonymized"`
	Kept       []wireKept `json:"kept"`
}

type wireReport struct {
	Person   model.PersonID                `json:"person"`
	People   int                           `json:"people"`
	Identity int                           `json:"identity"`
	Access   int                           `json:"access"`
	Modules  map[string]wireModuleDeletion `json:"modules"`
	Kept     []wireKept                    `json:"kept"`
}

func wireProfileOf(p Profile) wireProfile {
	out := wireProfile{
		Person: wirePerson{
			ID: p.Person.ID, PlayaName: p.Person.PlayaName, Pronouns: p.Person.Pronouns, Language: p.Person.Language,
			LegalName: p.Person.LegalName, Phone: p.Person.Phone, ContactEmail: string(p.Person.ContactEmail),
			Email: string(p.Person.Email), HasAccount: p.Person.HasAccount, Complete: p.Person.Complete,
			CreatedAt: p.Person.CreatedAt, UpdatedAt: p.Person.UpdatedAt,
		},
		FirstPrompt: p.FirstPrompt,
		Fields:      append([]string{}, p.Fields...),
		Contacts:    make([]wireContact, len(p.Contacts)),
		Consents:    make([]wireConsent, len(p.Consents)),
		Sections:    make([]wireSection, len(p.Sections)),
		Can:         wireCan(p.Can),
		Policy: wirePolicy{
			LegalName: p.Policy.LegalName.String(), Phone: p.Policy.Phone.String(), EmergencyContacts: p.Policy.EmergencyContacts.String(),
		},
	}
	for i, c := range p.Contacts {
		out.Contacts[i] = wireContactOf(c)
	}
	for i, c := range p.Consents {
		out.Consents[i] = wireConsentOf(c)
	}
	for i, sec := range p.Sections {
		w := wireSection{Key: sec.Key, Failed: sec.Failed, Items: make([]wireItem, len(sec.Items))}
		for j, it := range sec.Items {
			w.Items[j] = wireItem(it)
		}
		out.Sections[i] = w
	}
	return out
}

func wireContactOf(c EmergencyContact) wireContact {
	return wireContact{
		ID: c.ID, PersonID: c.Person, Name: c.Name, Phone: c.Phone, Relationship: c.Relationship,
		Position: c.Position, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func wireConsentOf(c Consent) wireConsent {
	return wireConsent{PersonID: c.Person, Purpose: c.Purpose, Granted: c.Granted, RecordedBy: c.RecordedBy, ChangedAt: c.ChangedAt}
}

func wireRequestOf(r Request) wireRequest {
	return wireRequest{
		ID: r.ID, PersonID: r.Person, Kind: r.Kind, Status: r.Status.String(), Note: r.Note, CreatedAt: r.CreatedAt,
		DecidedAt: r.DecidedAt, DecidedBy: r.DecidedBy, DecisionNote: r.DecisionNote,
	}
}

func wireKeptOf(ks []Kept) []wireKept {
	out := make([]wireKept, len(ks))
	for i, k := range ks {
		out[i] = wireKept(k)
	}
	return out
}

func wireReportOf(r DeletionReport) wireReport {
	out := wireReport{
		Person: r.Person, People: r.People, Identity: r.Identity, Access: r.Access,
		Modules: make(map[string]wireModuleDeletion, len(r.Modules)), Kept: wireKeptOf(r.Kept),
	}
	for name, m := range r.Modules {
		out.Modules[name] = wireModuleDeletion{Deleted: m.Deleted, Anonymized: m.Anonymized, Kept: wireKeptOf(m.Kept)}
	}
	return out
}

// requestKind reads the kind of a request body: only "delete" exists in M2.
func requestKind(op, kind string) error {
	if kind == "delete" {
		return nil
	}
	return fieldInvalid(op, "kind", codeOneOf, "must be delete")
}
