# Coding standards

This file links to where each package's own rules actually live — it does not
restate them, so they cannot drift out of sync. Read the linked section before
writing code in that package.

| Rule | Where it's defined |
|---|---|
| No WHAT-comments (only a non-obvious invariant, a workaround, or a WHY) | [/CLAUDE.md](../CLAUDE.md) — repeated in every package's own `CLAUDE.md` |
| `domain`/`application` never import `adapter` (hexagonal boundary) | [/CLAUDE.md](../CLAUDE.md), [server/CLAUDE.md](../server/CLAUDE.md) |
| Nothing multi-tenant, no cloud, no control plane | [/CLAUDE.md](../CLAUDE.md) |
| Secrets never on argv; children `spawn`ed with `shell: false` | [/CLAUDE.md](../CLAUDE.md), [desktop/CLAUDE.md](../desktop/CLAUDE.md) |
| Go layout, build (`CGO_ENABLED=1`), local-mode invariants | [server/CLAUDE.md](../server/CLAUDE.md), [server/.ai/architecture.md](../server/.ai/architecture.md) |
| Electron invariants (window/backend boot order, bridge contract, stop order) | [desktop/CLAUDE.md](../desktop/CLAUDE.md) |
| Atomic Design (reuse-first, atom/molecule/organism placement) | [desktop/ui/CLAUDE.md](../desktop/ui/CLAUDE.md), [desktop/ui/.ai/frontend-components.md](../desktop/ui/.ai/frontend-components.md) |
| Commit messages | [CONTRIBUTING.md](../CONTRIBUTING.md) — Conventional Commits |

See [testing-standards.md](testing-standards.md) for how each package is verified.
