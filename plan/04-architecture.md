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
- **One database.** The core owns the `core` schema. Each module owns its own schema and
  migrations (D10).

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

Open (Q-OFFLINE). Whatever the answer, the module contract must not rule out a local,
on-site instance that syncs later.
