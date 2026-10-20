# Contributing

Thanks for helping. bms is meant to be built by many hands, so the process is written down
rather than kept in anyone's head.

## Ways to help

- **Tell us what your regional needs.** Open an issue describing the job to be done (who does
  it, when, and what goes wrong today). Features grow from real use.
- **Design.** Every abstraction and module starts as a design spec in `plan/design/`. Specs
  are reviewed before any code is written. The template is in
  `.claude/skills/design-spec/SKILL.md`.
- **Code.** Pick a spec marked **Approved** in `plan/design/README.md`.
- **Docs, translation, accessibility testing.** All just as valuable as code.

## Ground rules

- Read [`plan/00-principles.md`](plan/00-principles.md) first. It's short.
- Code is written against an Approved spec. If the spec is wrong, change the spec first.
- A module never imports another module. If two modules need to share something, it belongs
  in the core, and that change needs a spec.
- **Never put real personal data** in issues, fixtures, seeds, screenshots or tests.
- `make check` must pass.
- Releases follow [semantic versioning](https://semver.org/). The module contract (the
  `Module` interface, `Manifest`, `Deps` and the core contracts modules import) is the public
  API for forks' own modules: it breaks only in a major version.

## Working with Claude Code

The repo includes project agents and skills in `.claude/`. They follow the same workflow as
human contributors: design, then build and test in parallel, then mechanical checks, then
review. See [`plan/08-agent-workflow.md`](plan/08-agent-workflow.md).

## License

By contributing, you agree that your contributions are licensed under the
[AGPL-3.0](LICENSE).

## Sign-off (DCO)

Every commit is signed off under the [Developer Certificate of Origin](https://developercertificate.org/):
by adding a `Signed-off-by: Your Name <you@example.org>` line (`git commit -s`), you certify
that you wrote the change or otherwise have the right to submit it under the project's
license. There is no CLA: everyone, maintainers included, contributes on the same AGPL terms.

## Code of conduct

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).
