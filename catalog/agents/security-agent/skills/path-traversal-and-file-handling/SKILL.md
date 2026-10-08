---
name: path-traversal-and-file-handling
category: security
description: Use when the diff builds a filesystem path from input, extracts an archive, accepts an upload, serves a file or writes a temp file - traversal, symlinks, zip-slip, upload type and served content-type
tech_stack: Web & API
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); OWASP Top 10:2025 A01 and CWE cited by name only
---
# Path Traversal and File Handling

## Overview

`Join`, `normalize`, `resolve` and `clean` are **lexical**: they rewrite a string, they do not ask the filesystem where it points. A path check that runs on the string and an `open` that runs on the filesystem disagree whenever there is a `..`, an absolute path, or a symlink.

**Core principle:** resolve the real path first, then check containment by path components, then open what you checked — or use an API that confines the open itself.

## 1. Traversal (CWE-22)

```go
// ❌ HasPrefix on a cleaned string: "/srv/files-private/x" passes for base "/srv/files",
//    and a symlink inside base escapes entirely
p := filepath.Join(base, name)
if !strings.HasPrefix(filepath.Clean(p), base) { return errBadPath }
f, _ := os.Open(p)

// ✅ Go 1.24+: the root confines every lookup, symlinks included
root, _ := os.OpenRoot(base)
f, err := root.Open(name)
// older Go: reject non-local names first — filepath.IsLocal(name) (Go 1.20+)
```

```java
// ❌ normalize() is lexical; a symlink under base still escapes
Path p = base.resolve(name).normalize();
if (!p.startsWith(base)) throw new SecurityException();

// ✅ compare real paths (Path.startsWith is component-wise, String.startsWith is not)
Path real = base.resolve(name).toRealPath();
if (!real.startsWith(base.toRealPath())) throw new SecurityException();
```

```python
# ❌ os.path.join drops base entirely when name is absolute ("/etc/passwd")
path = os.path.join(UPLOAD_DIR, name)

# ✅
root = Path(UPLOAD_DIR).resolve()
path = (root / name).resolve()
if not path.is_relative_to(root):   # Python 3.9+
    abort(400)
```

```ts
// ❌ startsWith without a trailing separator; "/data/app-secrets" passes for "/data/app"
const p = path.resolve(base, name); if (!p.startsWith(base)) throw ...
// ✅
const p = path.resolve(base, name); if (!p.startsWith(base + path.sep)) throw ...
// and for symlinks: fs.realpathSync(p) before the check
```

Flask: `send_from_directory`. Express: `res.sendFile(name, { root: base })` (rejects escaping paths) rather than `res.sendFile(path.join(base, name))`.

## 2. Symlinks (CWE-59)

Lexical checks never see a symlink. It matters when the attacker can create files inside the base (an upload directory, an extracted archive, a cloned repository, a shared volume). Resolve with `realpath`/`EvalSymlinks`/`toRealPath`, or open with `O_NOFOLLOW` / `os.Root`. A check-then-open gap on a directory the attacker writes to is a race (CWE-367) — report it only when the attacker demonstrably controls that directory.

## 3. Archive Extraction (zip-slip)

Archive entry names are attacker input: `../../.ssh/authorized_keys`, absolute paths, and symlink entries that point outside and are then written through.

```java
// ❌
File out = new File(destDir, entry.getName());
Files.copy(zis, out.toPath());
// ✅
Path out = destDir.resolve(entry.getName()).normalize();
if (!out.startsWith(destDir)) throw new IOException("bad entry: " + entry.getName());
```

- Python `tarfile.extractall()` without `filter="data"` (the safe default only from Python 3.14); `shutil.unpack_archive` on tar. `zipfile.extractall` sanitises `..` and absolute names — not a finding by itself.
- Go `archive/zip` and `archive/tar`: check `filepath.IsLocal(hdr.Name)` and skip or reject symlink entries.
- Node: hand-rolled extraction with `adm-zip`/`unzipper`/`yauzl` joining `entry.path`.

Decompression bombs are a resource issue — excluded.

## 4. Uploads (CWE-434)

- **Where it is stored:** under a web root or a directory the server executes from (PHP, JSP, CGI) → upload becomes code. Store outside, under a generated name.
- **What name it gets:** the client filename used in the path → traversal (section 1). Use a server-generated id; keep the original name only as metadata.
- **What type it is served as:** an uploaded `.html` or `.svg` served inline from the app's own origin is stored XSS. Serve user files with `Content-Disposition: attachment`, a fixed safe `Content-Type`, `X-Content-Type-Options: nosniff`, or from a separate cookieless domain.
- **How the type is checked:** the client's `Content-Type` and the extension are claims. Allowlist extensions and verify the magic bytes when the type decides how the file is processed (image libraries, document parsers).
- **Size:** unlimited size is resource exhaustion — excluded; do not report it.

## 5. Serving and Static Routes

- A static mapping widened to the project root exposes `.env`, `.git/`, source and backups.
- A download endpoint that takes a full path or an object-storage key from the client — combine with access-control-and-idor: can user A fetch `tenants/B/report.pdf`?
- Signed URLs whose key is built from user input without the tenant prefix.

## 6. Temporary Files (CWE-377)

Predictable names in a shared temp directory (`/tmp/export-` + userId) let a local attacker pre-create or symlink the file. Use `os.CreateTemp`, `tempfile.mkstemp`/`NamedTemporaryFile`, `Files.createTempFile`, `fs.mkdtemp`. Only a finding when other local users or tenants share the machine.

## Severity Guide

Reading arbitrary server files or writing outside the intended directory → HIGH; CRITICAL when the write reaches code, config or `authorized_keys`, or the read reaches credentials. Inline-served HTML/SVG upload → HIGH (stored XSS).

## Common Mistakes

- Reporting traversal where the value is a server constant, an enum, or a UUID parsed before use.
- Reporting path traversal in browser or mobile client code — it is the user's own filesystem.
- Trusting `filepath.Clean`, `path.normalize` or `os.path.normpath` as a containment check.
- Missing that Flask's default `<name>` converter rejects `/`, while `<path:name>` accepts it.

## Red Flags

- `Join`/`resolve` followed by a string `startsWith`/`HasPrefix` check.
- `entry.getName()`, `hdr.Name`, `member.name` used to build an output path.
- `extractall(` with no `filter=` on Python < 3.14.
- An upload handler that writes `file.originalname` / `filename` into the path.
- `Content-Type` taken from the upload when serving it back.

## References (names and links only)

CWE-22, CWE-59, CWE-367, CWE-377, CWE-434 at [cwe.mitre.org](https://cwe.mitre.org/) · [OWASP Top 10:2025 A01 Broken Access Control](https://owasp.org/Top10/2025/) · [ASVS 5.0 V5 File Handling](https://github.com/OWASP/ASVS/tree/master/5.0/en)
