# Open Questions

Items marked **Blocking** need answers before the phase they belong to. Record each answer
here, copy it into the document it affects, and tick the box.

## Open

### Product

- [ ] **Q-MVP (Blocking, Phase 0):** Which module comes first, which regional pilots it, and
      what's the date of the event it must be ready for? **Direction (owner, 2026-10-16):**
      users come first (for management); volunteering needs several core features first
      (editions, departments and more). Agree the core feature list
      ([09-core-features.md](09-core-features.md)) before building. `volunteers` remains the
      likely first module. Regional-specific details go in `private/`.
- [ ] **Q-OFFLINE (Blocking, Phase 0):** Which workflows must work with no internet at the
      event (incident logging, shift check-in, gate)? Options: (a) online only; (b) an
      on-site instance on the local network, synced with the main one before and after;
      (c) an offline-capable web app that queues changes. This shapes the module contract
      and the `incidents` design.
      **Direction (owner, 2026-10-16):** it must work offline. Likely (b): an on-site mirror
      becomes the **single primary** during the event; the other copy keeps serving reads.
      One writer at a time, never two. Still open: how the handover works (planned
      switchover, by whom, how fast), and which workflows must keep working on site.
- [ ] **Q-TRUST:** How do regionals trust each other (D18)? For example: each publishes a
      document at a `/.well-known/` URL with its public key and the regionals it trusts;
      shared records such as volunteer credit are signed by the issuing regional. What's
      shared, with whose consent, and how a person proves both accounts are theirs.
      Not needed for the MVP.
- [ ] **Q-TICKET:** Is ticketing in scope, or do we integrate with existing ticketing vendors
      (for example, import ticket holders to link to profiles)?
- [ ] **Q-PAY:** Are payments in scope (camp dues, fees, grants)? If so, behind a payments
      interface with a self-hostable option.
- [ ] **Q-I18N:** Which languages are needed at launch? Proposed: English, with every string
      ready for translation.
- [ ] **Q-IMPORT:** Do regionals need to import data from their current systems (CSV of
      volunteers, camps, past editions)?
- [ ] **Q-RETAIN:** What are the default retention periods for sensitive data (incidents,
      medical notes)? These depend on law and insurance, so they must be configurable.
      Which defaults do we ship?

### Project

- [ ] **Q-NAME:** What's the final name, and does it move to a GitHub org? It lives at
      `github.com/zedgama3/bms` for now. `bms` is the working name
      (D6). Alternatives to consider: something that reads well to burners and isn't already
      taken by another burn tool.
- [ ] **Q-GOV1:** Governance: who can merge, how are maintainers added, which code of
      conduct? Proposed: Contributor Covenant.
- [ ] **Q-GOV2:** Contribution sign-off: DCO (`Signed-off-by`) or a CLA? Proposed: DCO, which
      keeps the AGPL promise symmetric for everyone.
- [ ] **Q-UPGRADE:** What's the upgrade promise between versions for forks that add their own
      modules (a stable module contract, semver)?

### Design (for the architect)

- [ ] **Q-WEBMOD:** How do modules contribute UI screens: a build-time registry in `web/`,
      so a fork adds a module folder and one import? Proposed: yes, mirroring the Go side.
- [ ] **Q-FORMS:** Do registrations need forms that a regional configures (custom
      questions per application type), or are fields fixed per module? Configurable forms are
      powerful but add a lot of complexity.

## Resolved

- [x] **Q-TYPE:** What kind of project is it? A web application, fully extensible, so each
      regional can fork what it wants. → D1, D5
- [x] **Q-STACK:** Which stack? The same as Sojourn: Go, Vite + React + TypeScript,
      PostgreSQL. Simple and boring, so it's easy to contribute. → D3
- [x] **Q-LICENSE:** Which license? AGPL-3.0. → D2
- [x] **Q-TENANCY:** One deployment per regional, or multi-tenant? One per regional,
      self-hosted. → D4
- [x] **Q-EXTEND:** How do regionals extend it? Compile-time modules. → D5
- [x] **Q-VIS:** Public or private? Public and open source from the start. → D12
- [x] **Q-IDP:** Is there an existing burner identity to use? No: there's no open, shared
      burner login; each tool has its own. Local accounts and OIDC (D15); trust between
      regionals later (Q-TRUST). → D18
- [x] **Q-LOGIN:** How do local accounts log in? For now, username and password only. E-mail
      reset, magic links, passkeys and OIDC come later, behind the same provider model (D15).
