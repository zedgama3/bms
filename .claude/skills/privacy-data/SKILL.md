---
name: privacy-data
description: How bms classifies, stores, accesses, audits, exports and deletes personal and sensitive data — sensitivity levels, least-privilege access, audit of sensitive reads, keeping personal data out of logs, signals and fixtures, retention, and per-person export and deletion. Load before designing, implementing or reviewing anything that touches information about people (profiles, contacts, shifts, registrations, incidents, reports).
---

# Personal and sensitive data

Regionals hold information that can hurt people if it leaks: legal names, addresses, phone
numbers, emergency contacts, medical notes, incident reports. The rules below make the safe
path the default. They're a starting point the owner refines.

## Sensitivity levels

Every column or field that holds information about a person gets one level in its spec.

| Level | Examples | Who can see it | Audited reads |
|---|---|---|---|
| `public` | playa name on a published camp listing | anyone the module shows it to | no |
| `internal` | playa name, department, shift times | signed-in people with the module's read permission | no |
| `personal` | legal name, e-mail, phone, address, emergency contact | the person, and roles with an explicit permission | yes, for anyone but the person |
| `sensitive` | medical notes, incident narratives, safety reports, legal matters | named roles only, scoped narrowly | always |

When in doubt, pick the higher level.

## Rules

- **Collect only what the feature needs.** The spec says why each `personal` or `sensitive`
  field is needed.
- **Access goes through the authorizer.** Lists, searches, exports and counts are filtered
  too. A list must not reveal that a hidden record exists, and a single record the caller
  may not see is answered `errs.NotFound`, never `errs.Permission`, so a `403` never
  confirms that an ID exists; `Permission` is for an action the caller may not take on a
  record they may see.
- **Audit.** Every read of a `personal` field by someone other than the person, and every
  read of a `sensitive` field, writes an audit record (who, what record, when, why if the
  workflow asks).
- **Never in logs, signals, notifications or error messages.** Use IDs. An e-mail
  notification says "a new incident was assigned to you" with a link, never the narrative.
- **Never in the browser's storage, URLs or analytics.** There's no third-party analytics.
- **Never real data in development.** Fixtures, seeds, screenshots and tests use invented
  people.
- **Retention is a setting** per module and level, with a job that enforces it. The
  shipped default is **keep forever**; a regional sets shorter periods (owner, 2026-10-16,
  Q-RETAIN). Deleted means deleted (or irreversibly anonymized), including attachments.
- **Export and delete.** Every module answers "export everything about person X" and
  "delete everything about person X" (or anonymize, where the module's spec explains a
  legal or safety reason to keep a record).
- **Consent.** Where data is optional (for example, sharing contact details with a camp),
  the person opts in, and the consent is recorded in `people`.
- **Backups contain everything.** The operator docs say so and how to protect them.
