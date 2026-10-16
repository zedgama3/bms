# Open Questions

Items marked **Blocking** need answers before the phase they belong to. Record each answer
here, copy it into the document it affects, and tick the box.

## Open

### Product

- [ ] **Q-MVP (Blocking, Phase 0):** Which module comes first, which regional pilots it, and
      what's the date of the event it must be ready for? Proposed: `volunteers` first.
      Regional-specific details go in `private/`.
- [ ] **Q-OFFLINE (Blocking, Phase 0):** Which workflows must work with no internet at the
      event (incident logging, shift check-in, gate)? Options: (a) online only; (b) an
      on-site instance on the local network, synced with the main one before and after;
      (c) an offline-capable web app that queues changes. This shapes the module contract
      and the `incidents` design.
      **Direction (owner, 2026-10-16):** it must work offline. Likely (b): an on-site mirror
      becomes the **single primary** during the event; the other copy keeps serving reads.
      One writer at a time, never two. Still open: how the handover works (planned
      switchover, by whom, how fast), and which workflows must keep working on site.
- [ ] **Q-IDP (Blocking, Phase 0):** How do people log in? Proposed: local accounts
      (e-mail plus password or magic link) and any OIDC provider. Is there an existing
      burner identity that regionals would want to use? D15 settles the provider model
      (local, OIDC, optional trusted proxy header). Still open: an existing burner identity,
      and the local-account login method (password or magic link).
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
