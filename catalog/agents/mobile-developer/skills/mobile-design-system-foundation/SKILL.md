---
name: mobile-design-system-foundation
category: mobile
description: Use when the mobile repository has no theme tokens or component library yet (a new app, an "Apply design system" task, or a task that asks for one), before building any screen - writes the approved design system's generated files from get_design_system (files true) and builds the theme from them when there is one.
source: ehmo/platform-design-skills (MIT), android/skills (Apache-2.0), adapted
---
# Mobile Design System Foundation

## Overview

A screen built before tokens and an atomic component library exist turns into `Color(0x…)`, one-off paddings, and a library that never forms. This skill lays the foundation once, in order, so every screen after it is composition, not invention.

**Core principle:** Lay the foundation before the first screen — tokens, folders, and the first atoms come before any feature work.

## Detect first

`get_design_system` with `files: true` for this repository before anything else. When it returns a design system, the direction is already decided and approved: skip the design direction below and materialize it instead. Write every file in `files` verbatim at its `path` — `DESIGN.md`, `design/tokens.json`, `design/tokens.css`, `design/INVENTORY.md`; each carries a generated-file header and is never edited by hand, never transcribed. Then build the stack's theme from `design/tokens.json` by role (Flutter: `ColorScheme` light and dark plus `ThemeExtension`s for spacing, radius and sizes; SwiftUI: asset-catalog colour sets with Any/Dark appearances plus `Theme.swift` for type, spacing and radius; Compose: `Color.kt` / `Theme.kt` / `Type.kt` / `Shape.kt` with a light and a dark `ColorScheme`), each value exactly as its token holds it and named after it (`color.light.primary` → `primary`). Use the colours as given — no `ColorScheme.fromSeed` that would recompute them. The repository's own `INVENTORY.md` lists its components under the names `design/INVENTORY.md` gives them. A theme that already exists but differs from it gets its VALUES updated to the design system's, its structure kept. Only an empty answer leaves the direction to you.

A `ThemeData`/`ThemeExtension`, an asset-catalog colour set, a `MaterialTheme` wrapper, or an `INVENTORY.md` already present (and, when `get_design_system` returned a design system, the generated `design/` files present and matching it) means this skill doesn't apply — stop, map and follow the existing structure (`flutter-atomic-components` / `mobile-ui-ux`) instead.

## Design direction (5 minutes, written into the plan, not a separate doc)

Only when `get_design_system` returned nothing. One line each, from the task brief (the brief's own words win over any default below): **audience + tone**; **4–6 named brand colours** mapped onto semantic roles (primary/secondary/error at minimum); **type pairing**; **radius** choice; **ASCII wireframe** of the key screen at phone (360) and tablet (768) width.

## Per-stack tokens

**Flutter (verified: `flutter analyze` clean):**

```dart
@immutable
class AppSpacing extends ThemeExtension<AppSpacing> {
  const AppSpacing({this.xs = 4, this.sm = 8, this.md = 16, this.lg = 24, this.xl = 32});
  final double xs, sm, md, lg, xl;
  @override AppSpacing copyWith({double? xs, double? sm, double? md, double? lg, double? xl}) =>
      AppSpacing(xs: xs ?? this.xs, sm: sm ?? this.sm, md: md ?? this.md, lg: lg ?? this.lg, xl: xl ?? this.xl);
  @override AppSpacing lerp(AppSpacing? other, double t) => this;
}

ThemeData buildTheme(Brightness b) => ThemeData(
  colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF2F5BEA), brightness: b),  // brand colour lives ONLY here
  extensions: const [AppSpacing()],
);

extension AppThemeX on BuildContext {
  AppSpacing get space => Theme.of(this).extension<AppSpacing>()!;
  ColorScheme get colors => Theme.of(this).colorScheme;
}
```

Folders: `lib/ui/core/{theme,atoms,molecules,organisms}` + `lib/ui/features/<f>/` — this is the official Flutter architecture layout; follow the repo's existing layout if it already has one (e.g. `lib/ui/atoms`).

**SwiftUI:** asset-catalog colour sets with Any/Dark appearances, `enum Space { static let xs: CGFloat = 4; static let sm: CGFloat = 8; static let md: CGFloat = 16; static let lg: CGFloat = 24 }`, semantic `Font` text styles (never `.system(size:)` outside the theme), `@ScaledMetric` for any custom sizing so it still follows Dynamic Type.

**Compose:** `data class AppSpacing(val xs: Dp = 4.dp, val sm: Dp = 8.dp, val md: Dp = 16.dp, val lg: Dp = 24.dp)`, `val LocalSpacing = staticCompositionLocalOf { AppSpacing() }`, `val MaterialTheme.spacing @Composable get() = LocalSpacing.current`; wrap the app in `MaterialTheme(colorScheme = …)` with dynamic colour on API 12+ and a static fallback below it.

## Folders and inventory

Create the atomic folders (`atoms/molecules/organisms`, or the repo's existing naming) plus `INVENTORY.md` — header line `One line per component: level · name · purpose · variants`, one bullet per component added in the same commit that creates it.

Base atoms to start from: `AppButton`, `AppTextField`, `AppIcon`, `StatusChip`, `Skeleton`, `EmptyState`, `ErrorView`.

## ui-guard as a test inside the normal suite

Wire a guard that runs inside the ordinary test command, so the build gate enforces it — not a separate CI step that can be skipped.

**Flutter (verified: flags 4 violations in a raw-styled atom, passes on the token version):**

```dart
// test/ui_guard_test.dart — runs inside `flutter test`, so the build gate enforces it
final banned = <String, RegExp>{
  'raw colour (use context.colors / ColorScheme)': RegExp(r'Color\(0x|Colors\.(?!transparent)'),
  'raw font size (use textTheme)': RegExp(r'fontSize:\s*\d'),
  'raw spacing (use context.space)': RegExp(r'EdgeInsets\.(all|symmetric|only|fromLTRB)\([^)]*\d'),
};
// walks lib/ui/**.dart except lib/ui/**/theme/**, collects "path:line reason", expect(offenders, isEmpty)
```

**Compose:** a JVM unit test scanning `src/main/**/ui/**` (except `ui/theme`) for `Color(0x` and a numeric literal inside `.padding(`.

**iOS:** SwiftLint `custom_rules` (`Color\(red:`, `\.font\(\.system\(size:`, `\.padding\(\d`) when the repo already uses SwiftLint; skip if it doesn't rather than introducing a new lint dependency for this alone.

## Worked Example

```
❌ Start writing a screen straight away: Color(0xFF2196F3) inline, EdgeInsets.all(16)
   scattered across three widgets, no INVENTORY.md.

✅ 1. Detect: no ThemeData/ThemeExtension found → this skill applies.
   2. Design direction (one paragraph) → 3. buildTheme() + AppSpacing extension
   → 4. lib/ui/core/{theme,atoms,...} folders + INVENTORY.md → 5. AppButton,
   StatusChip, Skeleton atoms + widget tests → 6. ui_guard_test.dart green
   → 7. first screen, built entirely from atoms already in INVENTORY.md.
```

## Common Mistakes

- Writing a screen before any tokens exist — every colour becomes a one-off hex.
- Skipping `INVENTORY.md` — the next task re-invents a Button.
- Wiring the ui-guard as a separate script instead of a test inside the normal suite, so a hand-off can skip it.
- Introducing SwiftLint to a repo that doesn't already use it, just for the guard.

## Red Flags

- A component file with `Color(0x…)`, `Colors.*`, or a numeric `EdgeInsets`/`.padding(` literal anywhere in the diff outside the theme files.
- `lib/ui/` (or the repo's component folder) with no `INVENTORY.md`.
- A primitive hand-rolled inline in a screen file instead of placed in the component library.
