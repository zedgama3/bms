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
- [ ] **Q-QUAL:** How are qualifications held and checked? Volunteer positions and shifts
      have requirements: training completed, certificates such as ICS-100 or ICS-200 (often
      with an expiry date), being over 18. Other modules may need the same facts (for
      example, early-arrival passes). Proposed: a `qualifications` capability (D19): a
      person's qualifications, who recorded or verified them, and their expiry, provided by
      one module and required by `volunteers`. Decide before the `volunteers` spec; not
      needed for the core MVP.
      **Partly answered (owner, 2026-10-16):** department leads check off (verify) the
      qualifications related to their department. Some qualifications apply to several
      departments (for example, ICS-100), but most are specific to one department. Recorded in
      [09-core-features.md](09-core-features.md) (§5 and the notes for `volunteers`). The shape
      (the `qualifications` capability) is still open for the architect when the `volunteers`
      spec is written.
- [ ] **Q-NAME (GitHub org):** Does the project move to a GitHub org? It lives at
      `github.com/zedgama3/bms` for now. The name is answered: BMS, for "Burn Management
      System" → D25. The org question was not answered and stays open; the repository stays
      at `github.com/zedgama3/bms` for now.

### Project

- [ ] **Q-GOV1:** Governance: who can merge, how are maintainers added, which code of
      conduct? Proposed: Contributor Covenant.
- [ ] **Q-GOV2:** Contribution sign-off: DCO (`Signed-off-by`) or a CLA? Proposed: DCO, which
      keeps the AGPL promise symmetric for everyone.
- [ ] **Q-UPGRADE:** What's the upgrade promise between versions for forks that add their own
      modules (a stable module contract, semver)?

### Build (M4, awaiting the owner)

- [ ] **Q-AMEND1:** Approve six pending spec amendments, already built on feat/m1: (1)
      `Notifier.Configured`, so a reset returns a uniform 501 when e-mail is off (identity.md
      and notify.md); (2) settings' `lazyAuthorizer`, no-store and ETag caching, and
      `PublicOrigin` deferred; (3) `HoldersOf` counts explicit grants only; (4) the
      `hello.greeted` template; (5) notify expiry: `Message.TTL`, `MaxAge` of 7 days, failed
      rows retryable until expiry, `ExpireStale`, and `SendTo` dedupe (F67–F72).
- [ ] **Q-RESET-RATE:** Reset requests share the sign-in failure buckets. That can lock out
      sign-in, and about 960 reset e-mails a day to one victim are possible. Recommended: own
      buckets for resets plus a per-account cooldown. Relates to D36(i).
- [ ] **Q-RESET-LIFE:** The reset lifetime is passed to templates as English words ("60
      minutes"). Recommended: pass a number and let each template word the unit.
- [ ] **Q-SETTINGS-REVIEW:** Settings review points: (1) drop the early area check, which can
      wrongly refuse a valid change for up to 30 s; (2) canonicalise and refuse a bad
      `public_url`; (3) move the crypto and contrast tests to unit tests; (4) document that a
      Check cannot see whether a secret is set, and that re-saving a secret counts as a change.
- [ ] **Q-MAILPIT-AUTH:** The dev Mailpit lacks the SMTP AUTH settings
      (`MP_SMTP_AUTH_ACCEPT_ANY`, `MP_SMTP_AUTH_ALLOW_INSECURE`), so SMTP login is not tested.
      Recommended: enable them in `deploy/compose`.
- [ ] **Q-WEB-REVIEW:** Web review points: (1) `Referrer-Policy: no-referrer` on the reset
      path, or `strict-origin` site-wide; (2) translate outbox error tokens and template
      names; (3) the outbox "address" wording.

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

### Design (for the architect)

- None open. Q-WEBMOD and Q-FORMS are resolved above. The shape of qualifications (Q-QUAL) is
  still open under Product.
