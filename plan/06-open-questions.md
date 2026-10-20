# Open Questions

Items marked **Blocking** need answers before the phase they belong to. Record each answer
here, copy it into the document it affects, and tick the box.

## Open

### Product

- [ ] **Q-TRUST:** How do regionals trust each other (D18)? For example: each publishes a
      document at a `/.well-known/` URL with its public key and the regionals it trusts;
      shared records such as volunteer credit are signed by the issuing regional. What's
      shared, with whose consent, and how a person proves both accounts are theirs.
      Not needed for the MVP.
- [ ] **Q-OFFLINE (Phase 0, no longer Blocking):** The workflows and the direction are
      answered (2026-10-16, D27): offline can come later, not MVP. The workflows most needed
      offline are incident management, volunteer management, and looking up a participant's
      personal and emergency information. The direction is an on-site mirror as the single
      primary during the event, the other copy serving reads. Still open: the handover
      mechanism (planned switchover, by whom, how fast) and which workflows keep working on
      site. The emergency-information lookup offline has privacy implications (sensitive data
      on an on-site copy) to address when it's designed.
### Build (M4)

- None open. The M4 review questions were answered 2026-10-20 (D40).

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
      Refined 2026-10-16: the username is the primary e-mail address. → D21
- [x] **Q-CORE1–13:** sign-up, password reset, login name, minors, sub-teams, showing people
      their audit trail, emergency contacts, "edition", departments, work outside the event,
      leadership terms, superuser and separation of duties, what counts as an event.
      Answered 2026-10-16. → [09-core-features.md](09-core-features.md#owners-answers-2026-10-16),
      D21, D22, D23, D24, Q-QUAL
- [x] **Q-MODEL1:** Event dates: instants or calendar dates? Answered by the owner 2026-10-16:
      an event has a start and an end, each a date and time (instants, with the IANA zone
      name); no civil `Date` type in `model`. Further dates and times inside an event (gate
      hours, possibly several; the temple burn, a single moment) are for `departments-events`
      to settle. → [design/model.md](design/model.md) (Open question 1, Proposed change 8).
- [x] **Q-MODEL2:** Canonical e-mail beyond case? Answered by the owner 2026-10-16: only
      capitalization is ignored; no dot or `+tag` stripping. → [design/model.md](design/model.md)
      (decision 11).
- [x] **Q-MVP:** Which module comes first, and which regional pilots it? The owner delegated
      the choice of order and dropped the pilot regional and date. Order after the core
      (M0–M4): `volunteers`, `incidents`, `registrations`, `after-reports`. → D26, and
      [02-scope.md](02-scope.md), [05-roadmap.md](05-roadmap.md)
- [x] **Q-TICKET:** Is ticketing in scope, or do we integrate with existing ticketing vendors?
      Not in scope at this time. → D33, [02-scope.md](02-scope.md)
- [x] **Q-PAY:** Are payments in scope (camp dues, fees, grants)? Not in scope at this time.
      → D33, [02-scope.md](02-scope.md)
- [x] **Q-I18N:** Which languages are needed at launch? English today, Spanish soon, then
      maybe others. The default language is a per-regional setting; every string is
      translation-ready from day one. → D28, [09-core-features.md](09-core-features.md)
- [x] **Q-IMPORT:** Do regionals need to import data from their current systems? Useful, but
      low priority; a later feature, not in the MVP. → D29
- [x] **Q-RETAIN:** What are the default retention periods for sensitive data? Retention is
      configurable by each regional; the default is forever (keep data until a regional sets
      a period). → D30, [09-core-features.md](09-core-features.md) (§8, §11)
- [x] **Q-WEBMOD:** How do modules contribute UI screens? Yes: each module gets its own folder
      in `web/`, with a build-time registry mirroring the Go side. `web-shell.md` (row 17)
      owns the registry. → D31, [design/README.md](design/README.md)
- [x] **Q-FORMS:** Do registrations need forms that a regional configures, or are fields fixed
      per module? Forms are built in code for now, with fields fixed per module; configurable
      forms may come later. → D32
- [x] **Q-NAME (GitHub org):** Does the project move to a GitHub org? No. The repository will
      eventually be published at `github.com/zedgama3/bms`, the current module path; no org
      move is planned. → D40
- [x] **Q-QUAL:** How are qualifications held and checked? Department leads check them
      manually. A lead applies a training record (date acquired, expiry or indefinite, who
      approved it), some department-specific and some org-wide; a user uploads an ICS100
      certificate and any lead certifies it. The shape (a `qualifications` capability, D19) is
      for the architect in the `volunteers` spec; the certificate upload depends on `files`.
      → D40
      Refined 2026-10-20: only department leads verify qualifications; department-specific
      ones by that department's leads, org-wide ones (e.g. ICS100) by any department lead. → D41
- [x] **Q-GOV1:** Governance: the code of conduct is the Contributor Covenant. Who can merge and
      how maintainers are added: the owner merges for now; nothing more decided. → D40
- [x] **Q-GOV2:** Contribution sign-off: DCO (`Signed-off-by`), no CLA. → D40
- [x] **Q-UPGRADE:** Upgrade promise for forks: semantic versioning. The module contract
      (Module interface, Manifest, Deps, the core contracts modules import) is the public API;
      breaking changes only in a major version, additions in minor, fixes in patch. → D40
- [x] **Q-AMEND1:** Pending spec amendments (1)–(5) approved with recommendations. → D40
- [x] **Q-RESET-RATE:** Reset requests: own limit buckets plus a per-account cooldown. → D40
      Refined 2026-10-20: the limits become `accounts.*` regional settings with the current
      defaults (the keys and ranges are in the identity amendment, being drafted). → D41
- [x] **Q-RESET-LIFE:** Templates get the reset lifetime as a number; each template words the
      unit. → D40
- [x] **Q-SETTINGS-REVIEW:** Settings review points (1)–(4) approved as recommended. → D40
- [x] **Q-MAILPIT-AUTH:** Enable `MP_SMTP_AUTH_ACCEPT_ANY` and `MP_SMTP_AUTH_ALLOW_INSECURE`
      in `deploy/compose`. → D40
- [x] **Q-WEB-REVIEW:** Web review points (a) `Referrer-Policy: no-referrer` on the reset page
      path; (b) translate outbox error tokens and template names, raw token as fallback; (c)
      the outbox recipient reads "An e-mail address (no account)". → D40

### Design (for the architect)

- None open. Q-WEBMOD and Q-FORMS are resolved above. The shape of qualifications is for the
  architect in the `volunteers` spec (Q-QUAL, D40).
