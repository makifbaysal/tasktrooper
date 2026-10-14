---
name: reuse-before-you-write
category: quality
description: Use before writing any new function, helper, component or module - find what the repository, the standard library and the installed dependencies already provide, and use an established library instead of hand-rolled code for a non-trivial problem
source: DietrichGebert/ponytail (MIT), adapted
---
# Reuse Before You Write

## Overview

Every line you add is a line someone reviews, tests and fixes later. Most of what a task needs already exists: in this repository, in the standard library or framework, in a dependency the project already ships, or in a library the whole ecosystem trusts. Writing it again costs review time, brings back bugs that library fixed years ago, and leaves the repository with two ways of doing one thing.

**Core principle:** the best code is the code you did not have to write. Be lazy about the solution, never about the change: the callers, tests and fixtures your change touches are still yours to finish.

## The ladder: take the first rung that fully works

1. **Does it need to exist?** Build what the task, its criteria and the human's comments ask for. Options, flags, config and "for later" extension points nobody asked for are not built.
2. **Already in this repository?** A helper, service, component, hook, repository method, error type, test fixture or builder. Call it the way its neighbours do. If it almost fits, extend it in place (a parameter, a case) instead of writing a near-copy beside it.
3. **Standard library, framework or platform?** Use what the language, the framework and the runtime already give you. If the repository wraps one (a house HTTP client, logger or date helper), use the house version.
4. **A dependency already installed?** Read the manifest and lockfile and use what is there. A second library for a job an installed one already does (two HTTP clients, two date libraries, two validators) is a finding.
5. **A non-trivial, well-understood problem?** Use the established library its ecosystem uses (see "When a library beats your own code"), vetted and added the way "Adding a library" says.
6. **Only now: the minimum new code** that solves the task, in the repository's existing structure, layers and conventions.

Whatever rung you land on, keep the validation at trust boundaries, the error handling that prevents data loss, the security checks and the accessibility. Reuse never cuts those.

## How to look before writing anything new

- `codebase_search` for the concept ("retry with backoff", "format money", "parse csv", "debounce", "object pool"). Names differ between repositories; concepts don't.
- `grep_code` for the usual names: `retry`, `backoff`, `slug`, `debounce`, `paginate`, `validate`, `format`, `parse`, `pool`, and the usual homes: `util`, `helpers`, `common`, `shared`, `lib`, `pkg`, `core`, `platform`.
- `get_symbol_skeleton` / `expand_symbol_context` on a candidate: confirm it does what you need and see how its callers use it.
- `search_memory` (scope=project) for this repository's earlier choices ("http client", "date library").
- The manifest: `go.mod`, `pom.xml` / `build.gradle(.kts)`, `package.json`, `pubspec.yaml`, `pyproject.toml` / `requirements*.txt`, `Packages/manifest.json` (Unity), `addons/` (Godot), `.uproject` / `.uplugin` (Unreal), `Cargo.toml`.

Two or three searches per concept is enough. Nothing found → go down the ladder; don't spend the run searching.

## When a library beats your own code

Hand-rolling one of these is a review finding, not a shortcut:

| Problem | Why your own version loses |
|---------|----------------------------|
| Password hashing, encryption, signatures, JWT/OAuth/OIDC, random tokens | One subtle mistake is a security hole; the library has been attacked for years. |
| Dates, times, time zones, durations, calendars | DST, leap years and zone rules break hand-written `86400`-second math. |
| Money and decimals | Binary floats round; money needs a decimal type or integer minor units. |
| Parsing and serialization: CSV, JSON schemas, YAML, TOML, XML, HTML, Markdown, URLs, e-mail addresses, phone numbers | Quoting, escaping, encodings and malformed input; a `split(",")` or a regex covers the happy path only. |
| Input validation of whole request or form objects | A schema library gives field-specific messages and one error shape for free. |
| Retries with backoff and jitter, circuit breakers, rate limiting | Easy to get wrong under load; see resilient-io guidance where your skills have it. |
| HTTP/gRPC clients, file formats (PDF, images, zip, spreadsheets) | Protocol and format edge cases you will not think of. |
| Plurals, number, currency and date formatting per locale | CLDR rules; the platform's `Intl` / `intl` already has them. |
| Sanitizing HTML or user content for display | XSS; use the sanitizer, never a regex. |

A small transformation of your own data (map a DTO, sum a list, build a label) is not on this list. Write it.

## Adding a library

Every point must hold, or go back down the ladder:

- **Established:** the de-facto choice in this ecosystem, the one the framework's own docs point to, widely used and documented. Not a single-maintainer package almost nobody installs.
- **Maintained:** a release in roughly the last year, and no open, unfixed security advisory.
- **Licence:** MIT, Apache-2.0, BSD, ISC, or one the repository already ships. GPL, AGPL, SSPL or no licence at all: stop and ask with add_task_comment.
- **Fits:** works with the repository's language, framework or engine version and its targets (browser bundle size, iOS/Android support, Python version, Unity version).
- **Real:** the exact name and current stable version from the registry or the official docs (`fetch_url`, `web_search`), never from memory. A name one letter off a popular package is a typosquat.
- **Proportionate:** it does something worth a dependency. A big package for one ten-line function → write the function.
- **Added properly:** with the repository's own package manager (`go get`, `npm`/`pnpm`/`yarn add`, `flutter pub add`, `uv add`/`poetry add`, Gradle/Maven, the engine's package manager), version pinned like its neighbours, and the lockfile change in the same commit.

Call the library directly, the way its documentation does. No wrapper or interface around it unless the repository already wraps its neighbours that way (a hexagonal adapter behind a port is the repository's structure, not an extra layer).

Name every new dependency in your closing message: what it is, and why nothing installed could do the job.

## Per stack

The repository's existing choice always wins. These are where to look first, and the usual established choices when nothing installed covers the job:

| Stack | Look first | Usual established choices |
|-------|------------|---------------------------|
| **Go** | `internal/platform`, `pkg/`, the stdlib (`net/http`, `encoding/*`, `time`, `slices`, `maps`, `errors`, `context`, `crypto/*`) | `github.com/google/uuid`, `golang-jwt/jwt/v5`, `shopspring/decimal`, `go-playground/validator/v10`, `golang.org/x/*` |
| **Java** | the service's shared packages, the JDK (`java.time`, `java.net.http`, `BigDecimal`), Quarkus/Spring built-ins | Jakarta Validation, Jackson, SmallRye Fault Tolerance (Quarkus) / Resilience4j (Spring) |
| **Web (TS/React)** | `src/lib`, `src/hooks`, `components/ui`, the platform (`fetch` + `AbortController`, `URL`, `Intl`, `crypto.randomUUID`, `<input type="date">`, `<dialog>`) | Radix/shadcn primitives, zod, react-hook-form, TanStack Query, date-fns, DOMPurify |
| **Flutter** | `lib/core`, `lib/shared`, the component library, the Flutter/Dart SDK | the repository's state library (Riverpod/Bloc), dio, go_router, intl, flutter_secure_storage, freezed/json_serializable |
| **Native mobile** | Foundation/SwiftUI, Jetpack, the Kotlin stdlib | URLSession + Codable; Retrofit/OkHttp, kotlinx.serialization, Room |
| **Data (Python)** | the package's own utils, the stdlib (`pathlib`, `datetime`/`zoneinfo`, `decimal`, `csv`, `itertools`, `functools`) | pandas/Polars, NumPy, scikit-learn (pipelines, splitters, metrics), SciPy/statsmodels, pydantic, pandera |
| **Game** | the project's pool, event bus, state machine and save service; the engine's built-ins (Unity `ObjectPool<T>`, Input System, Addressables; Godot signals, `Tween`, `AStar2D`, NavigationServer; Unreal GAS, Enhanced Input, timers) | the engine's official packages and plugins before third-party; never a hand-written physics step, pathfinder or serializer when the engine ships one |

## Worked Example

Task: "retry the webhook delivery up to 3 times."

- Rung 2: `grep_code "backoff|retry"` finds `internal/platform/httpx/retry.go`, already used by two other clients. Call it; the whole change is the call plus a test. Done.
- Had it not existed, rung 4: `go.mod` already lists `github.com/cenkalti/backoff`. Use it, as its docs show.
- Had neither existed: a three-attempt loop with jittered backoff is a few lines in the delivery adapter, and resilient-io guidance covers it. A new dependency for that is not proportionate.

Task: "let users pick a due date." The web repository's `components/ui` already has a `DatePicker` → use it. No house component → `<input type="date">`. Not a new date-picker package plus a wrapper component.

## Handoff

See your prompt's closing step for how to end the run. This skill decides what you write, not how you hand it off; name any new dependency in that closing message.

## Red Flags

- "It's quicker to write it than to find it." Finding it is one search.
- A new `utils`/`helpers` file next to an existing one, or a function that duplicates one in another package under a different name.
- A copied block from a neighbouring file instead of calling the original.
- Date math with `86400`, `split(",")` on CSV, a regex for e-mail or HTML, `Math.random()` for an id or token, hand-written HMAC/JWT handling.
- A new dependency for a few lines, or for a job an installed dependency already does.
- A new dependency without its lockfile change, or with a version typed from memory.
- A wrapper that only forwards calls to a library, or an interface with one implementation nobody asked for.
