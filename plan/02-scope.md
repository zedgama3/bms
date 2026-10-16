# Scope

## Goals

1. A regional can install bms on one small server and run its volunteer, registration and
   incident work in it.
2. A regional can turn off what it doesn't use, and add a module of its own, without forking
   the core.
3. The data a regional collects stays under its own control, with privacy controls that make
   doing the right thing the default.
4. New contributors can understand the system from `plan/` and ship a change against an
   Approved spec.

## Non-goals (for now)

- **Hosting several regionals in one deployment** (multi-tenant). Each regional runs its own
  copy (D4).
- **Runtime-loaded third-party plugins.** Modules are compiled in (D5).
- **Replacing ticketing vendors.** Whether to integrate is Q-TICKET.
- **Native mobile apps.** The web UI is mobile-first instead.

## Core (always present)

Accounts and profiles · roles and permissions · editions (event years) · departments ·
notifications (e-mail first) · audit log · file attachments · settings and terminology ·
data export and deletion.

## Modules

| Module | Covers | Status |
|---|---|---|
| `volunteers` | departments' roles, shifts, sign-up, leads, check-in, hours, reporting | First candidate (Q-MVP) |
| `registrations` | theme camps, RVs and vehicles, art, calendar events: application, review, approval | Planned |
| `incidents` | report, triage, assign, follow up, close; strict access and audit | Planned; depends on Q-OFFLINE |
| `after-reports` | per-department after-burn reports, lessons learned, carried into next edition | Planned |

Later, in the core: **trust between regionals** (D18, Q-TRUST), so that records such as
volunteer credit can be recognized across a group of regionals.

Possible later modules (not committed; listed so the core doesn't rule them out): placement
and mapping, art grants and budgets, early arrival and work access passes, gate and
check-in, inventory and assets, announcements, surveys, ticketing integration.

## MVP

To be set with Q-MVP. Proposed: the core features in [09-core-features.md](09-core-features.md)
marked **MVP**, plus the `volunteers` module, used by at least one department at one real
event.
