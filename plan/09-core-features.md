# Core Features

> **Agreed by the owner (2026-10-16):** milestones M0–M4, what "done" means and the order of
> the specs. The owner's answers (Q-CORE1–13, at the end) are applied below.

This is what the core lets people *do*, in the order it has to be built, and how each feature
breaks down into specs. [07-abstractions.md](07-abstractions.md) describes the code that
provides it, and specs in [design/](design/) describe each piece in detail. How burns work,
and the words they use, is in the `burn-domain` skill.

**MVP** means it's needed before the first module (`volunteers`, D26) can be used at a
real event. **Later** means it waits until after the pilot.

## Who uses the core

| Who | What they need from the core |
|---|---|
| **Participant** | an account, a profile they control, notices that reach them, their data on request |
| **Department lead or admin** | to run their own department: its details, its people and positions, its own admins, and nobody else's |
| **Site admins** (several separate jobs) | to manage accounts, access, departments, events and settings, each job held by whoever the regional chooses |
| **Superuser** | break-glass access to everything, for development and emergencies only |
| **Operator** | to install, upgrade, back up and troubleshoot one copy, without being an expert |
| **Maintainer** | to see errors and trace a reported problem to its cause (D20) |
| **Module author** | one contract to plug into, and a reference module that uses all of it |

## Ground rules for every core feature

These apply everywhere, so the specs don't repeat them.

- **A person is not an account.** `people` holds the person (profile, contacts); `identity`
  holds how they log in. A person can exist without an account (for example, added by a lead
  or imported later), and later gain one. Modules refer to the person ID, never the account.
- **Separation of duties** (D23). No everyday role can do everything. Each admin job is its
  own permission set, departments run themselves, and a regional can forbid combinations
  (for example, "board members never see incident reports"). Only the break-glass superuser
  sees everything.
- **Mobile first.** Every screen works on a phone at the event. WCAG 2.1 AA.
- **Ready for translation.** Every string goes through the translation layer, from day one.
  English at launch, Spanish soon, then possibly others; the default language is a setting per
  regional (D28). Terminology (what the regional calls an "event", a "department") is a
  setting, applied through the same layer.
- **Works on a read-only copy (Q-OFFLINE).** Every read screen works when the database is
  read-only; every write fails with a clear "this copy is read-only" message, not an error
  page. Nothing a read does may need a database write (sessions, audit; see below).
- **Permission checked, audited, traced.** Every operation names its permission; every read of
  personal data by someone else is audited; every request is traceable (D20).
- **UUIDs for every ID,** so records created on site and off site never collide.

## Milestones

Each milestone is something the owner can try on bms-vm, not just code.

| Milestone | Features | You can... |
|---|---|---|
| **M0. Skeleton** | 1 | install with `bms migrate`, start it, see the empty shell, see its traces in Grafana; a "hello" module shows up |
| **M1. Accounts** | 2, 8 (basic) | create the superuser by command, sign up, log in and out, disable an account |
| **M2. People and permissions** | 3, 4, 9 (basic) | edit a profile, give someone an admin job, see menus change, see who viewed whose contact details |
| **M3. Departments and events** | 5, 7 | set up departments and an event, appoint a department's leadership, have that department manage itself, archive a department |
| **M4. Ready for a module** | 6, 11 (basic) | receive e-mail, reset a forgotten password by e-mail, export or delete one person by command; start `volunteers` |

```
 M0  1. Installer and harness
      │
 M1  2. Accounts and login ──────── 8. Audit log (writes start here)
      │
 M2  3. People and profiles ─▶ 4. Roles, permissions, separation of duties    9. Settings (basic)
      │
 M3  5. Departments and events ─▶ 7. Admin screens
      │
 M4  6. Notifications ─▶ e-mail password reset · 11. Export and deletion (basic)
      │
      ▼ volunteers module (Phase 1)

 Later: 10. Files · 11. Export and deletion (self-service) · 12. Offline switchover · 13. Trust
```

## 1. Installer and harness (MVP, M0)

The framework every feature plugs into. Nothing a participant sees yet, but everything after
it depends on getting this right.

**People can:**
- *Operator:* run `bms migrate` to install or upgrade, `bms create-superuser` to create the
  break-glass account, `bms serve` to run it, and `bms check` to see whether the
  configuration, database and schema versions are fine without starting.
- *Maintainer:* follow any error reference from a screen to its trace and logs in Grafana.
- *Module author:* write a module that adds a migration, a route, a screen, a permission and
  a signal, and see the core pick it up.

**Done when:**
- `bms migrate` applies the core's and each enabled module's migrations, one transaction per
  migration file, recording the version in the same transaction (D17), and refuses to run
  on a read-only database.
- `bms serve` stops with a message naming the module and both versions if any schema is out of
  date. It never changes the schema.
- Configuration errors are all reported at once, with the environment variable's name.
- `bms migrate` on a new installation generates the secrets file and prints the back-up notice.
- The registry refuses to start when a required capability has no provider, or two modules
  provide the same one (D19), and says which.
- `/healthz` (process up) and `/readyz` (database reachable, writable or read-only) respond;
  `/readyz` reports read-only from `db`'s probe.
- Shutdown finishes requests in flight, within a deadline.
- A request gets a trace ID; error responses carry a short reference; the web shell reports its
  errors to `POST /api/client-errors` (D20).
- A collector that is down or absent never fails start-up, a request or shutdown; stdout
  logs carry the trace ID without any collector.
- The web shell renders its frame, sign-in page and a "this copy is read-only" banner.
- A `hello` module exercises every part of the module contract and is the reference for
  module authors.

**Specs:** `model`, `config`, `module-contract`, `db`, `signals`, `telemetry`, `api`,
`web-shell`, `app`.

**Settled here:** signals (in-process domain events, D24) are delivered **after commit**, with
the subscriber's failures logged and counted, and, from M4, retryable ones handed to `jobs`;
nothing is durable before delivery (signals.md decisions 2 and 3); a failure never rolls back
the publisher. The `signals` spec confirms the details.

## 2. Accounts and login (MVP, M1)

**People can:**
- *Participant:* create an account with their e-mail address and a password, and log in with
  that address (D21); log in and out; change their password; see and end their other
  sessions; reset a forgotten password by e-mail (from M4, Q-CORE2).
- *Account admin:* disable and re-enable an account; set a temporary password that must be
  changed at next login; see when an account last logged in.

**Done when:**
- **The login name is the person's primary e-mail address** (D21), unique ignoring case. It's
  `personal` data: shown only to the person and to roles with the right permission (account
  admins, volunteer admins), never on lists other participants see. People are known by their
  playa name everywhere else.
- Changing the primary address changes the login; it's audited, and (once e-mail exists, M4)
  the new address is confirmed and the old one told.
- Passwords are hashed with Argon2id; minimum 12 characters, checked against a list of common
  passwords; no other composition rules.
- Failed logins are rate-limited per account and per address, and the error doesn't reveal
  whether an account exists for that address.
- Sessions are an HTTP-only, same-site cookie, with CSRF protection on every change.
- **Sessions work on the read-only copy:** the session is a signed token checked without a
  database write; logout and "end all sessions" work by a per-account counter that the token
  must match. (Ends the need to write a session row on every request.)
- Logins, failures, password changes and account changes are audited.
- Sign-up is open by default and can be switched to invitation only in settings (Q-CORE1).
- **The superuser** (D23) is created and reset only by command, never through the web. Its
  sessions are short, every login alerts the maintainers and the site admins, every action is
  audited, and the UI shows a banner while it's in use.

**Later:** e-mail verification at sign-up, magic links, passkeys, two-factor authentication
(first for the superuser and site admins), OIDC with the regional's own provider (D15).

**Specs:** `identity`.

## 3. People and profiles (MVP, M2)

**People can:**
- *Participant:* fill in and edit their profile; add, change and remove emergency contacts;
  choose who can see optional fields; record consent where a field needs it.
- *Department or site admin (with permission):* find people by playa name (or e-mail, if their
  role may see it), and see the fields their role allows.
- *Module:* add sections to a person's page and to "my page" through an extension point
  (D19), such as "departments I volunteer with" from `volunteers`.

**Profile fields** (sensitivity per the `privacy-data` skill):

| Field | Level | Required |
|---|---|---|
| Playa name (the name people know them by) | internal | yes |
| Pronouns | internal | no |
| Legal name | personal | setting (some regionals need it for waivers) |
| E-mail (primary; also the login, D21) | personal | yes |
| Phone | personal | setting |
| Emergency contacts: **any number**, each a name, phone and relationship | personal | **never required** (Q-CORE7) |

A regional can make legal name and phone required or hidden in settings. No address, no
photo, no age and no free-text "about me" in the MVP: collect only what's needed. Age,
training and certificates come with qualifications (Q-QUAL), not as profile fields.

**Done when:**
- Every read of a `personal` field by someone other than the person is audited.
- Search never reveals a person the searcher may not see, and results show only `internal`
  fields.
- Profile sections from modules appear and disappear with the module.
- A person can be created without an account (by an admin) and linked to one later.

**Specs:** `people`, plus the person-page extension point in `contracts/people`.

## 4. Roles, permissions and separation of duties (MVP, M2)

**People can:**
- *Access admin:* create roles from the permissions the core and modules declare; grant a role
  to a person site-wide or for one department; revoke it; see everyone who holds a
  permission; set the regional's separation-of-duties rules.
- *Department admin:* grant and revoke department roles **within their own department** only,
  including appointing other department admins.
- *Participant:* see only the menus, screens and buttons they're allowed to use.

**Done when:**
- Permissions are declared in code by each module (`volunteers.shifts.manage`) with a
  description an admin can read, and the highest sensitivity level they reach.
- **No everyday role holds every permission** (D23). The starter roles, which a regional can
  change, split the site admin's work:

  | Starter role | Can |
  |---|---|
  | Account admin | manage accounts: disable, re-enable, temporary passwords |
  | Access admin | manage roles, grants and separation-of-duties rules |
  | Department steward | create and archive departments, set and reset a department's leadership |
  | Event admin | create, change and close events |
  | Settings admin | branding, terminology, module settings, SMTP |
  | Audit reviewer | search the audit log; export who viewed a person's data |
  | Department lead / department admin | run one department: details, people, positions, its own roles |
  | Participant | everyone signed in |

- **Separation-of-duties rules** are data a regional sets: "a person holding role A may never
  hold permission B" (for example, board members never `incidents.reports.read`). A rule
  blocks the grant, and is checked again on every decision, so a later role change can't get
  around it. Rules are audited, and changing them needs its own permission.
- **The superuser** bypasses roles and rules. It isn't a role and can't be granted (D23).
- A grant can have an end date (for example, a regional that has leads re-apply every year
  sets the end date when it appoints them; Q-CORE11). Nothing renews automatically.
- A person can't grant a permission they don't hold, or outside the department they hold it
  for. At least one person must keep the access admin role.
- The `Authorizer` answers in batches ("which of these 50 records may this person see?") so
  lists don't make a query per row.
- Granting and revoking are audited.
- The web shell gets the person's permissions once per page load and hides what they can't use;
  the server checks again on every call.

**Specs:** `authz`.

## 5. Departments and events (MVP, M3)

**Departments** are standing parts of the regional, not tied to any event (Q-CORE9).

**People can:**
- *Department steward:* create a department; archive one that's no longer needed (and restore
  it); set or reset its leadership, so a department whose leads have all left doesn't need
  the superuser.
- *Department admin:* change their department's description and contact address, and run it
  (its people and, through `volunteers`, its positions).
- *Event admin:* create an event (name, kind, a start and end date-time, and time zone), and
  close it when it's over. An event may also have scheduled times within it, such as gate hours
  or the temple burn; how those are modelled is for `departments-events` to settle.
  Several events can be open at once (a burn and its decompression party).
- *Participant:* see the departments and the upcoming events.

**Done when:**
- Departments are flat: no sub-teams (Q-CORE5). Roles within a department, such as Khaki or
  R12 on a Ranger team, are **positions**, which belong to `volunteers`, with requirements
  (Q-QUAL). Department leads check off (verify) the qualifications related to their department.
- An archived department keeps its history, takes no new work, and disappears from lists
  except to stewards. A department with nothing for an event simply shows nothing for it.
- Leadership is a department-scoped grant (step 4); there's no separate leads table.
- **An event is a gathering the regional puts on** (Q-CORE8, Q-CORE13): usually a burn, but
  also, for example, a decompression party. An event has its own dates and a kind (burn,
  decompression, other) the regional can name.
- **Work is not an event.** Land prep, build, strike, inventory and training are activities
  that may relate to an event and fall well outside its dates (Q-CORE10); they belong to
  modules such as `volunteers`.
- Modules attach their data to a department and, where it applies, an event, by ID (D19).
  Some work belongs to a department and no event at all (for example, winter inventory).
- Creating a new event calls an extension point, so modules can copy what they need
  (for example, shift templates).

**Specs:** `departments-events`.

## 6. Notifications (MVP, M4)

**People can:**
- *Participant:* receive e-mail from the regional (account, password reset, and module notices
  such as shift reminders).
- *Settings admin:* set the SMTP server, the sender address, and edit the wording of each
  template; send a test e-mail.

**Done when:**
- Notifications are queued in the database and sent by `jobs`, only on the copy that's
  writing (Q-OFFLINE), with retries, and never twice.
- Templates use the regional's name and terminology; module templates have defaults the
  regional can override.
- Notices carry no sensitive data: "you have a new message, sign in to read it", with a link
  (privacy-data).
- E-mail password reset (single-use, expiring link) is added to accounts once this exists.

**Later:** per-person preferences, digests, SMS or chat channels behind the same contract.

**Specs:** `notify`, `jobs`.

## 7. Admin screens (MVP, M3)

**People can:**
- *Each admin:* reach the screens for the jobs they hold, and only those: accounts, roles and
  grants, separation-of-duties rules, departments, events, settings, audit.
- *Department admin:* a page per department they run.
- *Any site admin:* see which modules are compiled in and enabled, the provides/uses graph
  (D19) and each schema version.

**Done when:** each screen is built from the features above (no admin-only back doors), and
each action is permission-checked and audited like any other.

**Specs:** part of `web-shell` (frame) and each feature's own spec (its screens).

## 8. Audit log (MVP, from M1)

**People can:**
- *Audit reviewer:* search the log by person, actor, record and date; export the detailed
  record of who viewed one person's data, as an internal process. The export is itself
  audited.
- *Participant (Later, Q-CORE6, D22):* see **that** their personal data was viewed, never
  by whom, as far as the regional's setting allows.

**Done when:**
- Entries record who, what action, which record, **which fields**, the viewer's role or
  position title at that moment, when, from which copy, and the trace ID. Never the personal
  data itself. Recording fields and title from the start keeps every D22 setting possible
  later without a migration.
- Superuser actions are marked, so they're easy to review.
- Nobody can change or delete entries through the app; the database role the app uses can only
  insert and read.
- **On a read-only copy,** audited reads are written to a local append-only file and imported
  when that copy writes again, so audit isn't lost and reads don't need the database to write.
- Entries are kept for a set time (a setting; the default is to keep them until a regional sets
  a period, D30).

**Specs:** `audit`.

## 9. Settings and branding (MVP, basic, from M2)

**People can:**
- *Settings admin:* set the regional's name, logo, colors, contact address, public URL and
  default language (D28); choose terminology; change each module's settings.

**Done when:**
- Settings are typed, have defaults, and are validated before saving; a module declares its
  settings in its manifest.
- Changes are audited and take effect without a restart.
- The public URL is the installation's identity (D18).

**Specs:** `settings`.

## 10. Files (Later)

Attach files to records (incident photos, art proposals) under the record's permissions.
Local disk first, S3-compatible storage optional. Files follow the data to the on-site copy
(Q-OFFLINE). **Specs:** `files`.

## 11. Data export and deletion

- **MVP (M4):** an account admin exports one person's data (JSON, every module through the
  extension point) or deletes it, by command, with the action audited.
- **Later:** a person downloads their data from their page; asks for deletion, which an admin
  reviews; retention periods set by the regional (default: keep forever, D30) delete data
  automatically.

**Specs:** part of `people` (the extension point), `jobs` (retention).

## 12. Offline switchover (Later; before the first event that needs it)

Run a copy on site, switch the writer to it for the event, and switch back afterwards
(Q-OFFLINE). The MVP already guarantees what makes this possible: UUIDs, read-only mode
everywhere, sessions and audit that don't need writes, jobs and e-mail only on the writer.
**Specs:** a later `switchover` spec.

## 13. Trust between regionals (Later)

Regionals that trust each other recognize records such as volunteer credit (D18, Q-TRUST).
The MVP keeps the public URL as the installation's identity and keeps shareable records as
distinct, dated records.

## Not in the core

Shifts, positions, sign-ups, camps, incidents, reports and the rest are **modules**, built on
top of this list. See [02-scope.md](02-scope.md). Notes for the `volunteers` spec, gathered so
far: positions with requirements (Q-QUAL), checked off by department leads for their department
(some qualifications, such as ICS-100, apply to several departments; most apply to one); shifts before, during and after the event (land
prep, build, strike, inventory); a shift or activity can be open to all, invitation only, or
limited to certain positions (Q-CORE10).

## Specs to write, in order

The architect drafts them in this order; each is reviewed before the next that depends on it.

| Order | Spec | For | Milestone |
|---|---|---|---|
| 1 | `model` | shared IDs and value types | M0 |
| 2 | `module-contract` | the manifest, registry, contracts and extension points | M0 |
| 3 | `config` | process configuration | M0 |
| 4 | `db` | pool, read-only detection, migrations, schema versions | M0 |
| 5 | `telemetry` | logs, traces, metrics, error references, client errors | M0 |
| 6 | `signals` | in-process domain events, after commit (D24) | M0 |
| 7 | `api` | router, middleware, error mapping | M0 |
| 8 | `web-shell` | frame, API client, screen registry (one folder per module under `web/`, build-time, D31), error reporting | M0 |
| 9 | `app` | commands, wiring, lifecycle | M0 |
| 10 | `audit` | the log and its read-only spool | M1 |
| 11 | `identity` | accounts, passwords, sessions, the superuser | M1 |
| 12 | `settings` | typed settings and terminology | M2 |
| 13 | `people` | profiles, consent, person-page and export extension points | M2 |
| 14 | `authz` | permissions, roles, department-scoped grants, separation of duties | M2 |
| 15 | `departments-events` | departments, archiving, leadership, events | M3 |
| 16 | `jobs` | queued and scheduled work, writer only | M4 |
| 17 | `notify` | e-mail and templates | M4 |
| — | `files`, `switchover` | Later | — |

## Owner's answers (2026-10-16)

- **Q-CORE1. Sign-up:** open by default, with a setting to switch to invitation only.
- **Q-CORE2. Password reset:** by e-mail, in M4.
- **Q-CORE3. Login name:** the person's primary e-mail address is the username, visible only to
  those with the rights to see it (admins, volunteer admins). → D21
- **Q-CORE4. Minors / over 18:** not asked for now. Age is one of several **requirements** that
  volunteer shifts and positions will have (training done, certificates such as ICS-100,
  over 18), to be designed with qualifications. → Q-QUAL
- **Q-CORE5. Sub-teams:** none. Khaki and R12 are **positions** on the Ranger team, not
  sub-teams; positions have requirements (Khaki needs ICS-100 and training, R12 needs
  ICS-200 and training). Positions belong to `volunteers`.
- **Q-CORE6. Showing people their audit trail:** yes, as a later feature: a person can see
  *that* their personal data was viewed, never by whom. How much is shown is configurable per
  regional. The detailed "who" is an internal export for people with the right permission.
  → D22
- **Q-CORE7. Emergency contacts:** optional, and a person can have several.
- **Q-CORE8. "Edition":** not a word burners use, and likely misunderstood. Use **event**.
  → D24
- **Q-CORE9. Departments:** standing, not tied to an event; an unused department just has
  nothing for that event. Departments can be archived. A permission to archive departments
  and to set or reset leadership exists, so this doesn't need the superuser.
- **Q-CORE10. Work outside the event:** there are pre- and post-event activities (land prep,
  inventory and more). Some are open to all, some invitation only, some only for certain
  positions. → notes for `volunteers` above.
- **Q-CORE11. Leadership terms:** some regionals make leads re-apply every year, with no
  automatic reinstatement; others don't. Not built in for now; a grant's optional end date is
  enough.
- **Q-CORE12. Superuser and separation of duties:** a superuser role has full access, for
  development and break-glass only, not normal use. Everyone else has separated duties;
  each department has its own admins and manages itself; a regional can forbid combinations,
  for example that board (LLC) members never review incident reports. → D23
- **Q-CORE13. What counts as an event:** usually a burn, but anything the regional puts on as
  a gathering, such as an after-burn decompression party. Clearing land before a burn is not
  an event. Training is probably an event-related activity, not an event. → D24
