---
name: web-design-system-foundation
category: frontend
description: Use when the repository has no design system yet (no tokens, no component library - a new site/app, an "Apply design system" task, or a task that asks for one), before building any page section - writes the approved design system's generated files from get_design_system (files true) and builds the tokens on them when there is one.
source: anthropics/skills frontend-design (Apache-2.0), adapted
---
# Web Design System Foundation

## Overview

A page built before tokens, a `cn()` helper, and the atomic folders exist turns into hex colors, one-off spacing, and a component library that never forms. This skill lays that foundation once, in order, so every page after it is composition, not invention.

**Core principle:** Lay the foundation before the first page section — tokens, folders, and the first atoms come before any feature work.

## Detect first

`get_design_system` with `files: true` for this repository before anything else. When it returns a design system, the direction is already decided and approved: skip the design direction below and materialize it instead. Write every file in `files` verbatim at its `path` — `DESIGN.md`, `design/tokens.json`, `design/tokens.css`, `design/INVENTORY.md`; each carries a generated-file header and is never edited by hand, never transcribed. Then build the token block in step (c) on them: `@import "../design/tokens.css";` right after `@import "tailwindcss";`, and every value in the step (c) block points at the generated variable by role instead of holding a value — each token is a CSS variable named by its path (`color.light.primary` → `--color-light-primary`), so `--primary: var(--color-light-primary)` in `:root`, `--primary: var(--color-dark-primary)` in `.dark`, `--font-sans: var(--font-family-sans)` in `@theme`. A generated name that already is a Tailwind theme variable (`--radius-md`, `--shadow-sm`, `--font-weight-semibold`) is used as it is: drop the step (c) line that would define it a second time, never point it at itself. `src/components/INVENTORY.md` lists the repository's components under the names `design/INVENTORY.md` gives them. A token block that already exists but differs keeps its structure and gets its values pointed at the generated variables. Only an empty answer leaves the direction to you.

Greenfield (use this skill) vs existing (map and follow `component-composition`, never restructure):
- `grep -r "@theme\|:root" src/index.css` (v4) or `tailwind.config.*` `theme.extend.colors` (v3) — tokens defined?
- `ls src/components` — do `atoms/molecules/organisms/templates` already exist, or a `components.json` (shadcn already initialized)?
- `cat src/components/INVENTORY.md` present → library exists, reuse it.
- Tailwind major: `grep '"tailwindcss"' package.json` (`^4` → `@theme`; `^3` → config + CSS vars).

Any of these present → stop, this skill doesn't apply; follow `component-composition` against the existing structure.

## Design direction (5 minutes, written into the plan, not a separate doc)

Only when `get_design_system` returned nothing. One line each, from the task brief (the brief's own words win over any default below): **audience + tone** (e.g. "B2B finance ops, calm and precise"); **4–6 named brand colors** mapped onto the semantic tokens (primary/accent/destructive at minimum — use the brief's colors if given); **type pairing**, max 2 families, self-hosted or Google Fonts `<link>` with `&display=swap`; **radius + shadow** choice (one `--radius`, elevation via `shadow-sm`/`shadow-md` only); **ASCII wireframe** of the key page at phone (360) and desktop (1440).

Avoid the generic-AI defaults (warm-cream+serif+terracotta; near-black+one acid accent; identical-card SaaS kit) unless the brief asks for one — see `ui-ux-craft`'s anti-generic-look list.

## Foundation steps, in order (each a committable step)

**(a) `@/` alias.** `tsconfig.app.json` → `"compilerOptions": { "paths": { "@/*": ["./src/*"] } }` (no `baseUrl` — TS 6 deprecates it; `paths` alone resolves under `moduleResolution: "bundler"`). `vite.config.ts`:
```ts
import path from 'node:path'
resolve: { alias: { '@': path.resolve(import.meta.dirname, './src') } }
```
Use `import.meta.dirname`, not `__dirname` (Vite 8 warns and will require it).

**(b) Deps.** `npm i clsx tailwind-merge class-variance-authority lucide-react` (runtime dependencies; `lucide-react` is the one icon library). Add `@radix-ui/react-<x>` only when a specific primitive is needed (dialog, popover, tabs...) — install that one package, not a bundle.
Do not run `npx shadcn init` on a Vite 8 + TS 6 + Tailwind 4.3 project: verified it does not resolve the `@/*` tsconfig path and writes `components/ui/` and `lib/utils.ts` into a literal `./@/` directory at the project root instead of `src/`, and it pulls in extra dependencies and tokens this foundation doesn't use (a bundled `radix-ui`/`cn` package, `@fontsource-variable/geist`, `tw-animate-css`, `--chart-*`/`--sidebar-*` CSS vars). Hand-write primitives in `components/ui/` on top of the individually installed `@radix-ui/react-<x>` packages instead.

**(c) Tokens in `src/index.css`** (Tailwind v4 — `@theme` maps semantic names to CSS vars, `:root` holds the light values, `.dark` the dark override):
```css
@import "tailwindcss";

@theme {
  --color-background: var(--background);
  --color-foreground: var(--foreground);
  --color-card: var(--card);
  --color-card-foreground: var(--card-foreground);
  --color-popover: var(--popover);
  --color-popover-foreground: var(--popover-foreground);
  --color-primary: var(--primary);
  --color-primary-foreground: var(--primary-foreground);
  --color-secondary: var(--secondary);
  --color-secondary-foreground: var(--secondary-foreground);
  --color-muted: var(--muted);
  --color-muted-foreground: var(--muted-foreground);
  --color-accent: var(--accent);
  --color-accent-foreground: var(--accent-foreground);
  --color-destructive: var(--destructive);
  --color-destructive-foreground: var(--destructive-foreground);
  --color-border: var(--border);
  --color-input: var(--input);
  --color-ring: var(--ring);
  --radius-sm: calc(var(--radius) - 4px);
  --radius-md: calc(var(--radius) - 2px);
  --radius-lg: var(--radius);
  --radius-xl: calc(var(--radius) + 4px);
  --font-sans: "<body face from the design direction>", ui-sans-serif, system-ui, sans-serif;
  /* --font-display: "<display face>", serif;  optional second family; load faces via <link> in index.html with &display=swap */
}

:root {
  --background: oklch(1 0 0);
  --foreground: oklch(0.18 0.01 260);
  --card: oklch(1 0 0);
  --card-foreground: oklch(0.18 0.01 260);
  --popover: oklch(1 0 0);
  --popover-foreground: oklch(0.18 0.01 260);
  --primary: oklch(0.42 0.17 264);        /* ← brand color from the design direction */
  --primary-foreground: oklch(0.98 0.005 260);
  --secondary: oklch(0.96 0.01 260);
  --secondary-foreground: oklch(0.24 0.02 260);
  --muted: oklch(0.96 0.01 260);
  --muted-foreground: oklch(0.5 0.02 260);
  --accent: oklch(0.93 0.03 264);
  --accent-foreground: oklch(0.24 0.02 260);
  --destructive: oklch(0.55 0.22 27);
  --destructive-foreground: oklch(0.98 0.005 260);
  --border: oklch(0.9 0.01 260);
  --input: oklch(0.9 0.01 260);
  --ring: oklch(0.42 0.17 264);
  --radius: 0.5rem;
}

.dark {
  --background: oklch(0.18 0.01 260);
  --foreground: oklch(0.96 0.005 260);
  /* ...same keys, dark values. Optional: only add if the brief needs dark mode. */
}

@layer base {
  * {
    border-color: var(--color-border);
    outline-color: color-mix(in oklch, var(--color-ring) 50%, transparent);
  }
  body {
    background-color: var(--color-background);
    color: var(--color-foreground);
    color-scheme: light;
    text-rendering: optimizeLegibility;
  }
  .dark body { color-scheme: dark; }
  :focus-visible { outline: 2px solid var(--color-ring); outline-offset: 2px; }
  ::selection { background-color: var(--color-primary); color: var(--color-primary-foreground); }
}
```
Verified: `npm run build` includes `.bg-primary`, `.text-muted-foreground`, `.rounded-lg { border-radius: var(--radius-lg) }` in the built CSS from this block alone.

Tailwind v3 equivalent: same `:root`/`.dark` variable block, plus in `tailwind.config.ts` → `theme.extend.colors = { background: "var(--background)", primary: { DEFAULT: "var(--primary)", foreground: "var(--primary-foreground)" }, ... }` for every semantic pair, and `borderRadius: { lg: "var(--radius)", md: "calc(var(--radius) - 2px)", sm: "calc(var(--radius) - 4px)" }`.

**(d) `src/lib/utils.ts`:**
```ts
import { clsx } from 'clsx'
import type { ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

**(e) Folders** — create exactly this tree:
```
src/
  index.css                 tokens (@theme) + base layer
  lib/utils.ts              cn()
  components/
    ui/                     Radix/shadcn primitives (atom-level vendor layer)
    atoms/                  Button, Input, Label, Badge, Icon, Heading, Text, Container, Stack, Link
    molecules/              FormField, NavLink, PriceTag, …
    organisms/              SiteHeader, MobileNav, PricingTable, ContactForm, SiteFooter, …
    templates/              MarketingLayout, AppLayout — page skeletons, own grid + breakpoints, no data
    INVENTORY.md            one line per component: level · name · purpose · variants/props
  pages/                    route components: data + state wiring
  hooks/ , api/             data hooks and API clients — imported by pages only
```
One component per file, PascalCase file = export name, tests co-located (`atoms/Button.tsx` + `atoms/Button.test.tsx`), direct imports via `@/components/atoms/Button` — no barrel `index.ts` files. Start `INVENTORY.md` with the header `One line per component: level · name · purpose · variants/props.` and add a bullet for every component in the same commit that creates it.

**(f) Base atoms first.** `Button` in full (cva variants × sizes, loading keeps the label and adds a spinner, `min-h-11` touch target on phone, `focus-visible` ring, `ref` as a plain prop — React 19 needs no `forwardRef`):
```tsx
import type { ButtonHTMLAttributes, Ref } from 'react'
import { cva } from 'class-variance-authority'
import type { VariantProps } from 'class-variance-authority'
import { Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        primary: 'bg-primary text-primary-foreground hover:bg-primary/90',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        outline: 'border border-input bg-background hover:bg-accent hover:text-accent-foreground',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
        destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        sm: 'h-9 min-h-11 sm:min-h-9 px-3 text-sm',
        md: 'h-10 min-h-11 sm:min-h-10 px-4 text-sm',
        lg: 'h-11 min-h-11 px-6 text-base',
        icon: 'h-10 w-10 min-h-11 min-w-11 sm:min-h-10 sm:min-w-10',
      },
    },
    defaultVariants: { variant: 'primary', size: 'md' },
  },
)

export interface ButtonProps
  extends ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  loading?: boolean
  ref?: Ref<HTMLButtonElement>
}

export function Button({ className, variant, size, loading = false, disabled, children, ref, ...props }: ButtonProps) {
  return (
    <button
      ref={ref}
      type="button"
      className={cn(buttonVariants({ variant, size }), className)}
      disabled={disabled || loading}
      aria-busy={loading}
      {...props}
    >
      {loading ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
      {children}
    </button>
  )
}
```
Then, one line each, same pattern (cva + `cn`, `ref` prop, every state styled, no outer margin):
- **Input** — text/email/etc, `aria-invalid` red ring, `text-base` (16px, no iOS zoom) on phone.
- **Label** — wraps native `<label>`, required-marker slot.
- **Heading** — `level` 1–4 prop mapping to `h1`–`h4` + a fixed size/weight per level, `text-balance`.
- **Text** — body paragraph, `muted` boolean for `text-muted-foreground`, `text-pretty`.
- **Container** — `mx-auto w-full max-w-7xl px-4 sm:px-6 lg:px-8`, no other styling.
- **Stack** — flex column/row + `gap-*`, the only layout atom that takes a `gap` prop (never margins on siblings).
- **Badge** — status chip, variant per semantic color, never color-only (pair with text/icon).
- **Icon** — thin wrapper around one icon library (`lucide-react`) fixing size via `className="size-4"` etc.

**(g) Templates.** `MarketingLayout` / `AppLayout`: header/main/footer skeleton owning the `Container` width and the responsive grid, no data, children render inside `main`.

**(h) ui-guard**, wired as:
```json
"scripts": { "prebuild": "npm run lint:ui", "lint:ui": "node scripts/ui-guard.mjs", "build": "tsc -b && vite build" }
```
Full script (`scripts/ui-guard.mjs`, dependency-free, verified: clean on correct code, catches every violation below, exits 0 under `--warn`):
```js
#!/usr/bin/env node
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'

const WARN = process.argv.includes('--warn')
const ROOT = resolve(import.meta.dirname, '..')
const SRC = join(ROOT, 'src')

const LEVEL_ORDER = ['ui', 'atoms', 'molecules', 'organisms', 'templates', 'pages']
const PALETTE_RE = /\b(bg|text|border|ring|fill|stroke|from|via|to|outline|decoration|divide|placeholder|shadow)-(slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b/
const HEX_RE = /#[0-9a-fA-F]{3,8}\b/
const ARBITRARY_RE = /-\[[^\]]+\]/g
const ALLOWED_ARBITRARY = [
  /grid-cols-\[repeat\(auto-(fit|fill),minmax\(/,
  /max-w-\[[\d.]+ch\]/,
  /aspect-\[[^\]]+\]/,
]
const BANNED_UTILS = ['h-screen', 'space-x-', 'space-y-', 'transition-all']
const Z_ARBITRARY_RE = /\bz-\[[^\]]+\]/

function listFiles(dir) {
  const out = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    const st = statSync(full)
    if (st.isDirectory()) out.push(...listFiles(full))
    else if (/\.(ts|tsx)$/.test(entry)) out.push(full)
  }
  return out
}

function levelOf(file) {
  const rel = relative(SRC, file).replace(/\\/g, '/')
  if (rel.startsWith('components/ui/')) return 'ui'
  if (rel.startsWith('components/atoms/')) return 'atoms'
  if (rel.startsWith('components/molecules/')) return 'molecules'
  if (rel.startsWith('components/organisms/')) return 'organisms'
  if (rel.startsWith('components/templates/')) return 'templates'
  if (rel.startsWith('pages/')) return 'pages'
  if (rel.startsWith('hooks/') || rel.startsWith('api/')) return 'data'
  return 'other'
}

function resolveImport(fromFile, spec) {
  if (spec.startsWith('@/')) return join(SRC, spec.slice(2))
  if (spec.startsWith('.')) return resolve(dirname(fromFile), spec)
  return null
}

function findings(file, lines) {
  const found = []
  const fileLevel = levelOf(file)

  lines.forEach((line, i) => {
    const lineNo = i + 1
    const allow = /ui-guard-allow:/.test(line)

    const importMatch = line.match(/from\s+['"]([^'"]+)['"]/)
    if (importMatch && LEVEL_ORDER.includes(fileLevel)) {
      const resolved = resolveImport(file, importMatch[1])
      if (resolved) {
        const targetLevel = levelOf(resolved + '.tsx')
        if (targetLevel === 'data' && fileLevel !== 'pages') {
          found.push(`${lineNo}: import-direction — ${fileLevel} imports hooks/api (only pages/ may)`)
        } else if (LEVEL_ORDER.includes(targetLevel)) {
          const fromIdx = LEVEL_ORDER.indexOf(fileLevel)
          const toIdx = LEVEL_ORDER.indexOf(targetLevel)
          const sameLevelOk = fileLevel === 'organisms' && targetLevel === 'organisms'
          if (toIdx > fromIdx && !sameLevelOk) {
            found.push(`${lineNo}: import-direction — ${fileLevel} may not import ${targetLevel} (fix: move the import to a level at or right of ${fileLevel}, or lift this logic up)`)
          }
        }
      }
    }

    if (!allow) {
      if (PALETTE_RE.test(line)) found.push(`${lineNo}: raw-palette-color — use a semantic token class (bg-primary, text-muted-foreground, …) instead of a raw Tailwind palette class`)
      if (HEX_RE.test(line) && /className|style/.test(line)) found.push(`${lineNo}: hex-color — use a semantic token, not a literal hex value`)

      const arbMatches = line.match(ARBITRARY_RE) || []
      for (const m of arbMatches) {
        const isAllowed = ALLOWED_ARBITRARY.some((re) => re.test(line))
        if (!isAllowed) found.push(`${lineNo}: arbitrary-value — "${m}" is not on the allow-list; use a scale token or add "ui-guard-allow: <reason>" on this line`)
      }

      for (const banned of BANNED_UTILS) {
        if (line.includes(`"${banned}`) || line.includes(`'${banned}`) || line.includes(` ${banned}`) || line.includes(`\`${banned}`)) {
          found.push(`${lineNo}: banned-utility — "${banned}" is disallowed (use min-h-dvh/h-dvh, gap-*, or the explicit transition properties)`)
        }
      }
      if (Z_ARBITRARY_RE.test(line)) found.push(`${lineNo}: arbitrary-z-index — use the fixed z-10/20/30/40/50 scale, not an arbitrary z-[…]`)
      if (/style=\{\{/.test(line)) found.push(`${lineNo}: static-inline-style — use Tailwind classes; style={{}} is reserved for runtime-computed values (add ui-guard-allow: <reason> if genuinely dynamic)`)
    }
  })

  return found
}

function main() {
  const files = listFiles(SRC)
  let violations = 0

  for (const file of files) {
    const content = readFileSync(file, 'utf8')
    const lines = content.split('\n')
    const found = findings(file, lines)
    for (const f of found) {
      violations++
      console.log(`${relative(ROOT, file)}:${f}`)
    }
  }

  if (violations > 0) {
    console.log(`\nui-guard: ${violations} violation(s)${WARN ? ' (--warn: not failing)' : ''}`)
    process.exit(WARN ? 0 : 1)
  }

  console.log('ui-guard: clean')
  process.exit(0)
}

main()
```
On an EXISTING repo, run it with `--warn` only (report mode, always exits 0) unless a human explicitly asks for enforcement — never flip a legacy repo's build red. ESLint-based repos may add `eslint-plugin-boundaries` on top; this script stays the stack-agnostic default (Vite 8 templates ship oxlint, not ESLint).

**(i) Tests for atoms.** Co-located RTL test per `component-testing` (e.g. `Button.test.tsx` next to `Button.tsx`) — behavior-focused, `byRole`, `user-event`.

**(j) Only then page sections** — compose, don't invent; see `component-composition`.

## Worked Example

```
❌ Start writing a hero section straight away: inline `#1a1a2e` background,
   a hand-rolled `<div onClick>` button, `w-[600px]` card, no INVENTORY.md.

✅ 1. tsconfig/vite alias → 2. npm i clsx tailwind-merge class-variance-authority lucide-react
   → 3. tokens in index.css → 4. lib/utils.ts → 5. folders + INVENTORY.md
   → 6. Button, Container, Heading, Text atoms + tests → 7. ui-guard wired,
   `npm run build` green → 8. MarketingLayout template → 9. first page section,
   built entirely from atoms already in INVENTORY.md.
```

## Common Mistakes

- Writing a page section before any tokens exist — every color becomes a one-off hex.
- Running `npx shadcn init` on this stack without checking where it actually wrote files (it writes to `./@/...`, not `./src/...`, verified).
- `baseUrl` in `tsconfig.app.json` — deprecated under TS 6; `paths` alone is enough with `moduleResolution: "bundler"`.
- `__dirname` in `vite.config.ts` — use `import.meta.dirname`.
- Skipping `INVENTORY.md` — the next task re-invents a Button.
- Wiring `lint:ui` but forgetting `prebuild`, so `npm run build` never actually runs ui-guard.

## Red Flags

- A component file with a raw hex color or `bg-blue-500` anywhere in the diff.
- `src/components/` with no `INVENTORY.md`.
- `npm run build` green but `dist/*.css` has no `--color-primary`/`.bg-primary` — tokens aren't wired into Tailwind.
- A primitive hand-rolled in a page file instead of `components/ui/`.
