package datarights

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

// The JSON forms of the datarights API (openapi.json, schemas Datarights*). IDs are
// canonical UUID strings and times RFC 3339 UTC. Personal strings the viewer may not see
// are left out.

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
