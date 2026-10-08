---
name: go-security-pitfalls
category: security
description: Use when reviewing Go changes - net/http and Fiber handlers, database/sql and GORM queries, os/exec, filepath and os.Root, html/template, crypto, TLS and SSH clients, JSON binding
tech_stack: Go
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); gosec rule ids, Go standard library and GORM security documentation cited, not reproduced
---
# Go Security Pitfalls

## Overview

Go removes whole classes of bugs (memory safety, implicit shell, unsafe default deserialization), which makes the remaining ones look innocent: a `fmt.Sprintf` in a query builder, a `HasPrefix` path check, a `template.HTML` conversion. Memory-safety findings in pure Go are excluded; `unsafe` and cgo are the exception.

## 1. SQL: database/sql, sqlx, GORM (CWE-89)

```go
// ❌
db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM tasks WHERE owner = '%s'", owner))
// ✅
db.QueryContext(ctx, "SELECT * FROM tasks WHERE owner = $1", owner)
```

GORM accepts raw SQL in more places than people expect:

```go
// ❌ all of these take SQL text
db.Where(fmt.Sprintf("name = '%s'", name)).Find(&users)
db.Order(r.URL.Query().Get("sort")).Find(&users)   // ORDER BY injection
db.First(&user, r.PathValue("id"))                // a string id is used as an inline condition
db.Raw("SELECT … " + filter).Scan(&rows)
// ✅
db.Where("name = ?", name).Find(&users)
db.Order(clause.OrderByColumn{Column: clause.Column{Name: sortCols[key]}}).Find(&users)
db.First(&user, "id = ?", id)
```

## 2. Commands: os/exec (CWE-78, CWE-88)

`exec.Command` never invokes a shell — unless you pass `sh -c`. With separate args the risk is argument injection:

```go
// ❌ shell
exec.Command("sh", "-c", "git clone "+repoURL).Run()
// ❌ no shell, but repoURL = "--upload-pack=…" is an option
exec.Command("git", "clone", repoURL, dir).Run()
// ✅
exec.Command("git", "clone", "--", repoURL, dir).Run()
```

Also check `cmd.Env` built from request data (`NODE_OPTIONS`, `LD_PRELOAD`, `GIT_SSH_COMMAND` — injection-and-dangerous-sinks).

## 3. Paths: filepath, os.Root (CWE-22)

```go
// ❌ lexical check: a symlink inside base escapes it, and
//    "/srv/data-other/x" passes HasPrefix(p, "/srv/data")
p := filepath.Join(base, name)
if !strings.HasPrefix(p, base) { … }

// ✅ Go 1.24+: lookups confined to the root, symlinks included
root, err := os.OpenRoot(base)
f, err := root.Open(name)
// ✅ older Go: reject anything non-local before joining
if !filepath.IsLocal(name) { return errBadPath }
```

`http.FileServer(http.Dir(dir))` blocks `..` but follows symlinks inside `dir` and serves dotfiles — a finding when `dir` is the repo root or an upload directory. Archive extraction: check `filepath.IsLocal(hdr.Name)` per entry.

## 4. Templates (CWE-79)

- `html/template` escapes by context; `text/template` does not — rendering HTML with `text/template` is XSS when data is user-controlled.
- Conversions switch escaping off: `template.HTML(userBio)`, `template.JS(…)`, `template.URL(…)`, `template.HTMLAttr(…)` on user data.
- `template.New("x").Parse(userText)` — the user writes the template (server-side template injection; with registered funcs that touch the system, worse).

## 5. JSON Binding (CWE-915)

```go
// ❌ the request decodes straight into the domain type, Role and OrgID included
var u domain.User
json.NewDecoder(r.Body).Decode(&u)
// ✅ a request DTO with only client-settable fields, mapped explicitly
var req struct{ Name, Email string }
json.NewDecoder(r.Body).Decode(&req)
```

`encoding/json` matches keys case-insensitively and lets the last duplicate win: a gateway or validator that checks `"role"` misses `"ROLE"`. Relevant only when a different component validates the raw JSON.

## 6. Crypto and Transport (CWE-295, CWE-338)

- `math/rand` / `math/rand/v2` for tokens → `crypto/rand` (`rand.Text()` in Go 1.24+).
- `tls.Config{InsecureSkipVerify: true}` on a production client.
- `ssh.InsecureIgnoreHostKey()` → MITM on every SSH connection (CWE-322).
- Secret comparison: `subtle.ConstantTimeCompare` / `hmac.Equal` (a note at most when missing).

## 7. HTTP Handlers and Middleware

- A route added on the bare router/app instead of the authenticated group (security-diff-review-method).
- `http.Redirect(w, r, r.URL.Query().Get("next"), http.StatusFound)` — open redirect; high confidence only.
- Client IP from `X-Forwarded-For` or a Fiber/Echo/Gin proxy-header setting without a trusted-proxy list, used for an allowlist or an auth decision.
- An error from the authorization lookup that falls through to the handler (fail open).
- Outbound `http.Client` fetching user URLs: use a dialer `Control` check and `Proxy: nil` (ssrf-and-url-allowlists).

## 8. unsafe and cgo

`unsafe.Pointer` arithmetic, `unsafe.Slice`/`unsafe.String` over attacker-sized buffers, and C code reached through cgo are memory-unsafe again — trace sizes and lengths from input.

## Common Mistakes

- Reporting `exec.Command(bin, args...)` as shell injection.
- Reporting `db.Query("… $1", v)` or GORM `Where("x = ?", v)` as SQL injection.
- Reporting `gopkg.in/yaml.v3` or `encoding/gob` as unsafe deserialization — they do not instantiate arbitrary types.
- Reporting races between goroutines on non-security state, or integer-conversion lint hits (gosec G115) with no attacker-controlled size.

## Red Flags

- `fmt.Sprintf` feeding `Query`, `Exec`, `Raw`, `Where`, `Order`, `Group`, `Having`.
- `"sh", "-c"` / `"bash", "-c"` in `exec.Command`.
- `strings.HasPrefix` on a joined path.
- `template.HTML(` around anything not a constant.
- `InsecureSkipVerify: true`, `InsecureIgnoreHostKey()`, `math/rand` near "token" or "secret".

## References (names and links only)

[Go security best practices](https://go.dev/doc/security/best-practices) · [os.Root (Go 1.24)](https://go.dev/blog/osroot) · [gosec rules](https://github.com/securego/gosec#available-rules) · [GORM security](https://gorm.io/docs/security.html) · [govulncheck](https://go.dev/doc/security/vuln/)
