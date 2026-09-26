# Contributing

## Prerequisites

| tool | version | why |
|---|---|---|
| Go | see `server/go.mod` | backend |
| Node | 22+ | UI and desktop shell |
| an agent CLI (`claude`, `cursor-agent`, `agy` or `opencode`), or a model API key | signed in | runs the agents |
| `git`, `rg` | any | clones, `grep_code` tool |

Postgres is not a prerequisite: the backend downloads and runs an embedded one under `server/data/` (or the app's data directory).

## Loops

```sh
make setup      # once
make dev        # backend + UI in a browser at http://localhost:3200
make desktop    # the Electron app, dev mode
make test       # everything CI runs
```

## Layout

```
server/       Go backend: API, board, agent runtime, embedded Postgres
desktop/      Electron shell: supervises the backend, serves the UI
desktop/ui/   React UI (also runs in a browser against `make dev`)
docs/         short, factual docs
```

## Rules

- Hexagonal boundaries in `server/`: `domain` and `application` never import `adapter`.
- No code comments that explain WHAT; only a non-obvious invariant, workaround or WHY.
- Every PR keeps `make test` green.
- Conventional Commits (`feat(board): …`, `fix(desktop): …`).
