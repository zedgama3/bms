# Core Features

> **Draft for the owner's review (2026-10-16).** Q-MVP: agree this list, and which items are
> MVP, before building.

This is the list of what the core lets people *do*, in the order it has to be built.
[07-abstractions.md](07-abstractions.md) describes the code that provides it, and specs
in [design/](design/) describe each piece in detail.

**MVP** means it's needed before the first module (likely `volunteers`) can be used at a
real event. **Later** means it waits until after the pilot.

## Build order

```
 1. Installer and harness ─▶ 2. Accounts and login ─▶ 3. Profiles ─▶ 4. Roles and permissions
                                                                          │
                     ┌────────────────────────────────────────────────────┘
                     ▼
 5. Editions and departments ─▶ 6. Notifications ─▶ 7. Admin screens ─▶ volunteers module
                     │
                     └─▶ 8. Audit log, 9. Settings and branding (alongside 5–7)
 Later: 10. Files · 11. Data export and deletion (full) · 12. Offline switchover · 13. Trust
```

Each step needs the ones before it. Steps 8 and 9 run alongside 5–7, because everything
after step 4 writes audit entries and reads settings.

## 1. Installer and harness (MVP)

The framework every feature plugs into. Nothing a participant sees yet.

- `bms migrate`: applies the core's and each enabled module's migrations, and records each
  schema version (D17).
- `bms create-admin` (or similar): creates the first administrator, so a new installation
  can be managed before any login provider is set up.
- `bms serve`: serves the API and the web UI. Each module checks its schema version when it
  initializes, and the app stops if any is out of date.
- The module contract and registry: turning modules on and off in configuration, and the
  capabilities, extension points and events each module provides and uses (D19).
- Health check, structured logs, clean shutdown.
- Troubleshooting (D20): traces, metrics and logs sent to Grafana, Loki and Tempo when
  configured; a short error reference on every error screen; errors in the web UI are
  reported to the server and logged for maintainers.
- An empty web shell: layout, navigation, sign-in page.

## 2. Accounts and login (MVP)

- Sign up with a username and password (Q-LOGIN). E-mail is collected as a contact detail.
- Log in and out.
- An administrator can reset a forgotten password.
- *Later:* e-mail verification and self-service reset, magic links, passkeys, and logging in
  with an OIDC provider the regional already runs (D15).
- Sessions that work on a read-only copy (Q-OFFLINE).
- Administrators can disable an account.

## 3. Profiles (MVP)

- A person's profile: playa name, legal name, pronouns, contact details, emergency contact.
- Each field has a sensitivity level and is shown only to people allowed to see it.
- People edit their own profile; consent is recorded where it's needed.
- Other modules add sections to a person's page through an extension point (D19), for
  example "departments I volunteer with" from `volunteers`.
- Administrators can find people (with permission).
- *Later:* download my data, delete my account (full version in step 11).

## 4. Roles and permissions (MVP)

- Modules declare permissions (for example `volunteers.manage-shifts`).
- Administrators group permissions into roles and grant roles to people.
- Grants can be limited to one department and/or one edition (for example, "lead of Rangers
  for 2027").
- Built-in roles to start from: administrator, department lead, participant.
- People see only the menus and screens they're allowed to use.

## 5. Editions and departments (MVP)

- **Editions:** the event years, with dates and a "current edition".
- **Departments:** the regional's departments, each with a description and leads (a
  department-scoped role grant).
- Departments carry over from one edition to the next, so they aren't re-entered each year.
- Modules hang their data off an edition and, where it applies, a department.

## 6. Notifications (MVP)

- E-mail through the regional's SMTP server.
- Templates a regional can edit, with its name and terminology.
- Sent only by the copy that's writing (Q-OFFLINE), and never twice.
- *Later:* per-person preferences, other channels (SMS, chat).

## 7. Admin screens (MVP)

- Manage people, roles and grants, editions, departments, and settings.
- See which modules are installed and enabled.

## 8. Audit log (MVP)

- Records who changed what, and who viewed sensitive data.
- Administrators can search it. Nobody can edit or delete entries.

## 9. Settings and branding (MVP, basic)

- The regional's name, logo, colors and contact address.
- Terminology: what the regional calls things ("edition" or "year", "department" or "team").
- Each module's settings, with defaults and validation.

## 10. Files (Later)

- Attach files to records (for example incident photos, art proposals), under the record's
  permissions. Local disk first, S3-compatible storage optional.

## 11. Data export and deletion (Later; basic version in MVP)

- A person can download everything the regional holds about them, across all modules.
- A person can ask for deletion; administrators review and carry it out.
- Retention periods: data is deleted automatically after a set time (Q-RETAIN).
- **MVP minimum:** an administrator can export and delete one person's data by command.

## 12. Offline switchover (Later; before the first event that needs it)

- Run a copy on site, switch the writer to it for the event, and switch back afterwards
  (Q-OFFLINE).
- The UI shows when a copy is read-only.

## 13. Trust between regionals (Later)

- Regionals that trust each other recognize records such as volunteer credit (D18, Q-TRUST).

## Not in the core

Shifts, sign-ups, camps, incidents, reports and the rest are **modules**, built on top of
this list. See [02-scope.md](02-scope.md).
