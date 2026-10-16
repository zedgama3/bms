# Core Features

> **Draft for the owner's review (2026-10-16).** Q-MVP: agree this plan, its MVP cut and the
> questions at the end before the architect starts the specs.

This is what the core lets people *do*, in the order it has to be built, and how each feature
breaks down into specs. [07-abstractions.md](07-abstractions.md) describes the code that
provides it, and specs in [design/](design/) describe each piece in detail.

**MVP** means it's needed before the first module (likely `volunteers`) can be used at a
real event. **Later** means it waits until after the pilot.

## Who uses the core

| Who | What they need from the core |
|---|---|
| **Participant** | an account, a profile they control, notices that reach them, their data on request |
| **Department lead** | to see and manage the people in their department for an edition, and nobody else's |
| **Administrator** | to set up the regional: editions, departments, roles, settings, accounts |
| **Operator** | to install, upgrade, back up and troubleshoot one copy, without being an expert |
| **Maintainer** | to see errors and trace a reported problem to its cause (D20) |
| **Module author** | one contract to plug into, and a reference module that uses all of it |

## Ground rules for every core feature

These apply everywhere, so the specs don't repeat them.

- **A person is not an account.** `people` holds the person (profile, contacts); `identity`
  holds how they log in. A person can exist without an account (for example, added by a lead
  or imported later), and later gain one. Modules refer to the person ID, never the account.
- **Mobile first.** Every screen works on a phone at the event. WCAG 2.1 AA.
- **Ready for translation.** Every string goes through the translation layer, English only at
  launch (Q-I18N). Terminology (what the regional calls "edition", "department") is a
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
| **M1. Accounts** | 2, 8 (basic) | create the first admin by command, sign up, log in and out, have an admin disable an account |
| **M2. People and permissions** | 3, 4, 9 (basic) | edit a profile, give someone a role, see menus change, see who viewed whose contact details |
| **M3. Event structure** | 5, 7 | set up an edition and departments, appoint leads for one department and one edition |
| **M4. Ready for a module** | 6, 11 (basic) | receive e-mail, reset a forgotten password by e-mail, export or delete one person by command; start `volunteers` |

```
 M0  1. Installer and harness
      │
 M1  2. Accounts and login ──────── 8. Audit log (writes start here)
      │
 M2  3. People and profiles ─▶ 4. Roles and permissions      9. Settings (basic)
      │
 M3  5. Editions and departments ─▶ 7. Admin screens
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
- *Operator:* run `bms migrate` to install or upgrade, `bms create-admin` to create the first
  administrator, `bms serve` to run it, and `bms check` to see whether the configuration,
  database and schema versions are fine without starting.
- *Maintainer:* follow any error reference from a screen to its trace and logs in Grafana.
- *Module author:* write a module that adds a migration, a route, a screen, a permission and
  an event, and see the core pick it up.

**Done when:**
- `bms migrate` applies the core's and each enabled module's migrations in one transaction per
  module, records each schema version (D17), and refuses to run on a read-only database.
- `bms serve` stops with a message naming the module and both versions if any schema is out of
  date. It never changes the schema.
- Configuration errors are all reported at once, with the setting's name.
- The registry refuses to start when a required capability has no provider, or two modules
  provide the same one (D19), and says which.
- `/healthz` (process up) and `/readyz` (database reachable, writable or read-only) respond.
- Shutdown finishes requests in flight, within a deadline.
- A request gets a trace ID; error responses carry a short reference; the web shell reports its
  errors to `POST /api/client-errors` (D20).
- The web shell renders its frame, sign-in page and a "this copy is read-only" banner.
- A `hello` module exercises every part of the module contract and is the reference for
  module authors.

**Specs:** `model`, `config`, `module-contract`, `db`, `events`, `telemetry`, `api`,
`web-shell`, `app`.

**Settled here:** domain events are delivered **after commit**, in process, with the
subscriber's failures logged and retried by `jobs`, never rolling back the publisher. The
`events` spec confirms the details.

## 2. Accounts and login (MVP, M1)

**People can:**
- *Participant:* create an account with a username and password (Q-LOGIN), giving an e-mail
  address as a contact detail; log in and out; change their password; see and end their
  other sessions.
- *Administrator:* disable and re-enable an account; set a temporary password that must be
  changed at next login; see when an account last logged in.

**Done when:**
- Usernames are unique ignoring case. E-mail addresses are unique too, so they can be used for
  reset later.
- Passwords are hashed with Argon2id; minimum 12 characters, checked against a list of common
  passwords; no other composition rules.
- Failed logins are rate-limited per account and per address, and the error doesn't reveal
  whether the username exists.
- Sessions are an HTTP-only, same-site cookie, with CSRF protection on every change.
- **Sessions work on the read-only copy:** the session is a signed token checked without a
  database write; logout and "end all sessions" work by a per-account counter that the token
  must match. (Ends the need to write a session row on every request.)
- Logins, failures, password changes and account changes are audited.
- Sign-up can be turned off in settings (invitation by an admin only).

**Later:** e-mail verification, self-service reset (moves to M4 if Q-CORE2 says so), magic
links, passkeys, two-factor authentication for admins, OIDC with the regional's own provider
(D15).

**Specs:** `identity`.

## 3. People and profiles (MVP, M2)

**People can:**
- *Participant:* fill in and edit their profile; choose who can see optional fields; record
  consent where a field needs it.
- *Lead or admin (with permission):* find people by playa name or username, and see the fields
  their role allows.
- *Module:* add sections to a person's page and to "my page" through an extension point
  (D19), such as "departments I volunteer with" from `volunteers`.

**Profile fields** (sensitivity per the `privacy-data` skill):

| Field | Level | Required |
|---|---|---|
| Playa name (the name people know them by) | internal | yes |
| Pronouns | internal | no |
| Legal name | personal | setting (some regionals need it for waivers) |
| E-mail | personal | yes (also the account's contact address) |
| Phone | personal | setting |
| Emergency contact (name, phone, relationship) | personal | setting |
| Date of birth or "I am 18 or over" | personal | setting (Q-CORE4) |

Each regional can make the optional fields required or hidden in settings. No address, no
photo and no free-text "about me" in the MVP: collect only what's needed.

**Done when:**
- Every read of a `personal` field by someone other than the person is audited.
- Search never reveals a person the searcher may not see, and results show only `internal`
  fields.
- Profile sections from modules appear and disappear with the module.
- A person can be created without an account (by an admin) and linked to one later.

**Specs:** `people`, plus the person-page extension point in `contracts/people`.

## 4. Roles and permissions (MVP, M2)

**People can:**
- *Administrator:* create roles from the permissions the core and modules declare; grant a
  role to a person, optionally limited to one department, one edition, or both; revoke it;
  see everyone who holds a permission.
- *Participant:* see only the menus, screens and buttons they're allowed to use.

**Done when:**
- Permissions are declared in code by each module (`volunteers.shifts.manage`) with a
  description an admin can read.
- Built-in roles: **administrator** (everything, can't be deleted, at least one must remain),
  **department lead** (meant to be granted per department and edition), **participant**
  (everyone signed in).
- Grants can expire (for example, at the end of an edition).
- The `Authorizer` answers in batches ("which of these 50 records may this person see?") so
  lists don't make a query per row.
- Granting and revoking are audited. A person can't grant a permission they don't hold.
- The web shell gets the person's permissions once per page load and hides what they can't use;
  the server checks again on every call.

**Specs:** `authz`.

## 5. Editions and departments (MVP, M3)

**People can:**
- *Administrator:* create an edition (name, dates, time zone), mark one as current, and close
  it when it's over; create departments with a description and contact address; turn a
  department on or off for an edition; appoint leads.
- *Participant:* see the current edition and the departments taking part.

**Done when:**
- An edition is one event, not one year: a regional with a spring and a fall event has two
  editions in a year.
- Departments are defined once and **carry over**: a new edition starts with the previous
  edition's departments switched on.
- Departments can have sub-teams one level deep (for example, Rangers > Khaki). Q-CORE5.
- A lead is a department-lead grant scoped to that department and edition (step 4), so
  "leads of Rangers for 2027" needs no separate table.
- Starting a new edition calls an extension point, so modules can copy what they need
  (for example, shift templates).
- Modules attach their data to an edition and, where it applies, a department, by ID (D19).

**Specs:** `editions-departments`.

## 6. Notifications (MVP, M4)

**People can:**
- *Participant:* receive e-mail from the regional (account, password reset, and module notices
  such as shift reminders).
- *Administrator:* set the SMTP server, the sender address, and edit the wording of each
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
- *Administrator:* manage people and accounts, roles and grants, editions, departments and
  settings, from one admin area; see which modules are compiled in, which are enabled, and the
  provides/uses graph (D19); see the schema version of each.

**Done when:** each screen is built from the features above (no admin-only back doors), and
each action is permission-checked and audited like any other.

**Specs:** part of `web-shell` (frame) and each feature's own spec (its screens).

## 8. Audit log (MVP, from M1)

**People can:**
- *Administrator (with `core.audit.read`):* search the log by person, actor, record and date.
- *Participant:* see who viewed or changed their personal data (Q-CORE6).

**Done when:**
- Entries record who, what action, which record, when, from which copy, and the trace ID.
  Never the personal data itself.
- Nobody can change or delete entries through the app; the database role the app uses can only
  insert and read.
- **On a read-only copy,** audited reads are written to a local append-only file and imported
  when that copy writes again, so audit isn't lost and reads don't need the database to write.
- Entries are kept for a set time (a setting; Q-RETAIN).

**Specs:** `audit`.

## 9. Settings and branding (MVP, basic, from M2)

**People can:**
- *Administrator:* set the regional's name, logo, colors, contact address and public URL;
  choose terminology; change each module's settings.

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

- **MVP (M4):** an administrator exports one person's data (JSON, every module through the
  extension point) or deletes it, by command, with the action audited.
- **Later:** a person downloads their data from their page; asks for deletion, which an admin
  reviews; retention periods delete data automatically (Q-RETAIN).

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

Shifts, sign-ups, camps, incidents, reports and the rest are **modules**, built on top of
this list. See [02-scope.md](02-scope.md).

## Specs to write, in order

The architect drafts them in this order; each is reviewed before the next that depends on it.

| Order | Spec | For | Milestone |
|---|---|---|---|
| 1 | `model` | shared IDs and value types | M0 |
| 2 | `module-contract` | the manifest, registry, contracts and extension points | M0 |
| 3 | `config` | process configuration | M0 |
| 4 | `db` | pool, read-only detection, migrations, schema versions | M0 |
| 5 | `telemetry` | logs, traces, metrics, error references, client errors | M0 |
| 6 | `events` | domain events, after commit | M0 |
| 7 | `api` | router, middleware, error mapping | M0 |
| 8 | `web-shell` | frame, API client, screen registry, error reporting | M0 |
| 9 | `app` | commands, wiring, lifecycle | M0 |
| 10 | `audit` | the log and its read-only spool | M1 |
| 11 | `identity` | accounts, passwords, sessions | M1 |
| 12 | `settings` | typed settings and terminology | M2 |
| 13 | `people` | profiles, consent, person-page and export extension points | M2 |
| 14 | `authz` | permissions, roles, scoped grants | M2 |
| 15 | `editions-departments` | editions, departments, leads | M3 |
| 16 | `jobs` | queued and scheduled work, writer only | M4 |
| 17 | `notify` | e-mail and templates | M4 |
| — | `files`, `switchover` | Later | — |

## Questions for the owner

Each has a recommendation; agree or change it.

- **Q-CORE1. Sign-up.** Open sign-up, or accounts by invitation only? *Recommended:* open
  sign-up by default, with a setting to switch to invitation only.
- **Q-CORE2. Password reset in the MVP.** Admin reset only works for a handful of people; a
  volunteer drive means hundreds. *Recommended:* add e-mail reset in M4, once notifications
  exist.
- **Q-CORE3. Login name.** Username, or e-mail address as the username? *Recommended:*
  username (as you said in Q-LOGIN), with e-mail unique so reset works.
- **Q-CORE4. Minors.** Do any regionals let minors have accounts (for example, teen
  volunteers with a guardian)? *Recommended:* adults only in the MVP, with an optional
  "I am 18 or over" confirmation; minors handled later.
- **Q-CORE5. Sub-teams.** Do departments need sub-teams? *Recommended:* one level, because
  big departments (Rangers, Gate, Medical) usually have them and leads are often per
  sub-team.
- **Q-CORE6. Showing people their audit trail.** Should participants see who viewed their
  personal data? *Recommended:* yes, later (not MVP); the log records it from the start.
