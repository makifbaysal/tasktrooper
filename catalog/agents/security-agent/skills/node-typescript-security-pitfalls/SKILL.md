---
name: node-typescript-security-pitfalls
category: security
description: Use when reviewing Node.js or TypeScript server code - Express/Fastify/Nest handlers, Next.js server actions and route handlers, child_process, ORM raw queries, prototype pollution, template engines, JWT and outbound HTTP clients
tech_stack: TypeScript & Node
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording) and anthropics/claude-code security-guidance plugin (ideas only); Node.js, Next.js and library security advisories cited, not reproduced
---
# Node and TypeScript Security Pitfalls

## Overview

TypeScript types vanish at runtime: `req.query.id: string` can arrive as an array or an object. Most Node findings come from that gap, from `exec` with a template string, from raw-query escape hatches in otherwise safe ORMs, and — in Next.js — from forgetting that a server action is a public endpoint.

## 1. Runtime Types Are Not Declared Types

```ts
// ❌ ?id[$ne]=x or ?id[]=a&id[]=b gives an object or array, not a string
const doc = await Orders.findOne({ id: req.query.id as string, owner: user.id });
// ✅ validate at the boundary (zod/valibot/class-validator) and coerce
const { id } = z.object({ id: z.string().uuid() }).parse(req.query);
```

Length, `includes` and equality checks on an unvalidated query value can be bypassed the same way.

## 2. child_process (CWE-78, CWE-88)

```ts
// ❌ shell
exec(`convert ${file} out.png`);
spawn("git", ["clone", url], { shell: true });
// ✅ no shell, end of options
execFile("git", ["clone", "--", url, dir]);
```

Also check the `env` option for request-controlled keys (`NODE_OPTIONS=--require …`).

## 3. Raw Queries in Safe ORMs (CWE-89)

| Library | Unsafe | Safe |
|---------|--------|------|
| Prisma | `$queryRawUnsafe`, `$executeRawUnsafe`, `Prisma.raw()` | `$queryRaw` tagged template, model methods |
| TypeORM | `query("… " + x)`, `.where(\`id = ${id}\`)` | `.where("id = :id", { id })` |
| Sequelize | `sequelize.query(\`… ${x}\`)`, `literal(x)` | `replacements` / `bind` |
| Knex | `knex.raw(\`… ${x}\`)` | `knex.raw("… ?", [x])` |
| Drizzle | `sql.raw(x)` | `sql\`… ${x}\`` |

Mongo/Mongoose: operator injection from JSON bodies (section 1), `$where`.

## 4. Next.js Server Actions and Route Handlers

- Every exported async function in a `"use server"` file is a callable POST endpoint, whether or not a page renders a form for it. Each must authenticate and authorize **inside** the action.

```ts
// ❌ the page is admin-only, the action is not
"use server";
export async function deleteUser(id: string) { await db.user.delete({ where: { id } }); }
// ✅
export async function deleteUser(id: string) {
  const session = await auth();
  if (session?.user.role !== "admin") throw new Error("forbidden");
  await db.user.delete({ where: { id } });
}
```

- Authorization done only in `middleware.ts`: middleware matchers miss paths, and Next.js < 15.2.3 / 14.2.25 / 13.5.9 / 12.3.5 let an `x-middleware-subrequest` header skip middleware entirely (CVE-2025-29927). Check in the handler or data layer too.
- Server components passing whole records to client components serialise every field into the page payload.
- `NEXT_PUBLIC_*` variables are shipped to the browser.

## 5. Prototype Pollution (CWE-1321) — high confidence only

Report only with a gadget: a recursive merge/set of user JSON (`lodash.merge`, `_.set` with user paths, hand-written deep merge) where `__proto__` or `constructor.prototype` reaches a property later read for an auth decision, a `child_process` option (`shell`, `env`), or a template option.

```ts
// ❌ deepMerge(defaults, req.body) with {"__proto__": {"isAdmin": true}}
// ✅ validate the body against a schema first; build lookups with Object.create(null) or Map
```

## 6. Templates and Rendering

- EJS `<%- user %>`, Handlebars `{{{ user }}}`, Pug `!= user` → XSS.
- `res.render(view, req.query)` or `{ ...req.body }` as locals: template options ride along (EJS `outputFunctionName`/`escapeFunction`, CVE-2022-29078 class) → RCE.
- User-controlled template source compiled with `Handlebars.compile`, `pug.compile`, `ejs.render`.

## 7. Sandboxes and Code Loading

- `vm`/`vm.runInNewContext` is not a security boundary; `vm2` is discontinued after sandbox escapes. Running user code in either is CRITICAL when reachable.
- `eval`, `new Function`, `require(userPath)`, dynamic `import(userPath)`.
- `js-yaml` 3.x `load()` instantiates JavaScript types (`!!js/function`) — `safeLoad` or js-yaml 4's `load`.
- `node-serialize` `unserialize` on input.

## 8. HTTP Clients and Tokens

- axios: an absolute URL passed where a path was expected overrides `baseURL` and sends the configured auth headers to that host (CVE-2025-27152 class) — validate that user-supplied paths are relative.
- Redirect following by default in axios/got/node-fetch on user URLs (ssrf-and-url-allowlists).
- `jsonwebtoken`: `jwt.decode` does not verify; `jwt.verify` without `algorithms`.
- `crypto.timingSafeEqual` for MACs (throws on length mismatch — compare lengths first); `Math.random` for tokens.

## 9. Express and Fastify Specifics

- `app.set("trust proxy", true)` trusts every hop's `X-Forwarded-For`.
- `express.static(".")` or `express.static(__dirname)` serves source and `.env`.
- `res.sendFile(path.join(dir, name))` instead of `res.sendFile(name, { root: dir })`.
- Multer `file.originalname` used as the stored path.
- A route registered before `app.use(authMiddleware)` runs without it.

## Common Mistakes

- Reporting `$queryRaw` tagged templates, TypeORM parameters or `execFile` arrays as injection.
- Reporting prototype pollution with no gadget.
- Reporting `JSON.parse` as unsafe deserialization.
- Missing that a server action is reachable without the page that renders it.

## Red Flags

- Template literals inside `exec(`, `query(`, `raw(`, `$queryRawUnsafe(`.
- `shell: true`.
- `"use server"` files with exported mutations and no `auth()` call inside them.
- `as string` on `req.query` / `req.params` values used in a query.
- `merge(`, `defaultsDeep(`, `set(` fed with `req.body`.

## References (names and links only)

[Node.js security best practices](https://nodejs.org/en/learn/getting-started/security-best-practices) · [Next.js data security](https://nextjs.org/docs/app/guides/data-security) · CVE-2025-29927, CVE-2025-27152, CVE-2022-29078 · CWE-78, 89, 1321 · [OWASP Top 10:2025](https://owasp.org/Top10/2025/)
