# Architecture

> Sketch only. The specs in [design/](design/) are authoritative, and
> [07-abstractions.md](07-abstractions.md) sets the boundaries.

## Runtime shape

```
  browser (React SPA, mobile-first)
      │  HTTPS, session cookie
      ▼
  bms (one Go binary)
   ├─ api shell: router, auth middleware, error mapping, OpenAPI
   ├─ core: identity, authz, people, editions, departments, notify, files, audit, events, jobs
   └─ modules: volunteers, registrations, incidents, after-reports, (your module)
      │
      ▼
  PostgreSQL (one database; a schema per module)        SMTP (e-mail)        file storage
```

- **One process.** The binary serves the API and the built web UI, and runs background jobs
  (reminders, retention). Nothing else to install.
- **No proxy required** (D15). The binary serves HTTP itself; Cloudflare Tunnel, nginx or
  Caddy in front are optional deployment choices.
- **One database.** The core owns the `core` schema. Each module owns its own schema (D10).
- **Storage is a seam** (D8). Business logic talks to a storage interface; the PostgreSQL
  code sits in a `postgres/` package behind it.
- **Migrations are an install step** (D17, Proposed). `bms migrate` applies them; `bms serve`
  only checks the schema is current.

## How a module plugs in

A module is a Go package under `internal/modules/<name>/` (and a matching folder under
`web/src/modules/<name>/`). It gives the core:

- its **migrations**
- its **permissions** (for example `incidents.read-sensitive`), which the core turns into
  roles a regional can assign
- its **HTTP routes**, mounted by the api shell under `/api/<module>/`
- its **navigation entries and screens** for the UI
- the **domain events** it publishes and subscribes to
- its **background jobs** and **settings** (with defaults and validation)

The exact shape is the `module-contract` spec. Until it's Approved, this list is a sketch.

## Data ownership

| Data | Owner | Others get it through |
|---|---|---|
| accounts, profiles, contact details | core `people` | the people directory contract (with permission checks) |
| editions, departments | core | core contracts |
| roles, permissions, grants | core `authz` | the authorizer |
| shifts, sign-ups | `volunteers` | domain events (for example `volunteers.shift-completed`) |
| applications | `registrations` | domain events |
| incidents | `incidents` | nothing by default; sensitive |

## Offline at the event

Direction from the owner (Q-OFFLINE, still open in its details): an on-site copy runs on
the event's local network and becomes the **single primary** for the event; the off-site
copy keeps serving reads. There is only ever one writer, so there's no merge or conflict
resolution. A likely mechanism is PostgreSQL replication with a planned switchover.

Constraints this puts on the design (the architect checks each in its spec):

- **Read-only mode.** Any instance may be a read-only replica. Writes fail early with a clear
  error kind, and the UI says the site is read-only instead of failing on save.
- **Logging in without writing.** Sessions on a replica can't rely on writing to the
  database (for example, signed session cookies, or sessions kept outside PostgreSQL).
- **Side effects only on the primary.** Jobs, e-mail and other notifications run only where
  writes happen, so nothing is sent twice.
- **No identifiers that depend on one server.** Use IDs that don't need a central counter
  (for example UUIDs), so a switchover never collides.
- **Files follow the data.** Attachments must be on the primary's side of the switchover.
