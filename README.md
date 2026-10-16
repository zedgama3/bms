# bms: burn management system

bms (a working name) is an open-source web application for running a regional burn, built
by the burner community for the burner community. Communal effort applies to the software
too.

It covers the work that keeps an event running:

- **Volunteers:** sign-up, departments, shifts, leads, check-in and reporting
- **Incidents:** reporting, triage, follow-up, with sensitive data kept private
- **Registrations:** theme camps, RVs and vehicles, art, and events for the calendar
- **After-burn reports:** what happened, what to keep, what to change next year

and is built so that each regional can add what it needs. Every feature is a **module**. A
regional can turn modules on or off, replace one, or write its own, without patching the
core.

> **Status:** early design. There's no usable release yet. The plan, decisions and design
> specs are in [`plan/`](plan/README.md).

| | |
|---|---|
| Hosting | one copy per regional, self-hosted: one service plus PostgreSQL |
| Backend | Go, standard library first |
| UI | Vite + React + TypeScript, mobile-first and accessible |
| Database | PostgreSQL 16+ |
| Login | local accounts and any OIDC provider |
| License | [AGPL-3.0](LICENSE). If you run a modified copy, share your changes. |

## Development

Prerequisites:
- Go (the version in `go.mod`)
- Docker with Compose
- `golangci-lint` v2
- Node.js LTS and pnpm (once `web/` exists)

```sh
cp deploy/compose/.env.example deploy/compose/.env   # change the passwords
make up          # PostgreSQL, Mailpit
make check       # fmt, vet, build, unit tests, lint
make test-integration
```

## Contributing

Contributions are welcome, from code to design and docs, and to telling us what your
regional needs. See [CONTRIBUTING.md](CONTRIBUTING.md). Every abstraction gets a design spec
in [`plan/design/`](plan/design/README.md) before any code is written.
