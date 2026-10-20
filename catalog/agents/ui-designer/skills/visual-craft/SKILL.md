---
name: visual-craft
category: design
description: Use on every screen design - what separates a finished, professional website or app screen from a wireframe: layout and grid, type scale, colour and depth, real imagery and illustration drawn in SVG/CSS, polished components, density and rhythm, microcopy, and a pre-attach polish checklist
---
# Visual Craft

## Overview

The human judges the mockup as if it were the shipped product. A grey-box layout with one border colour, system-font text and no imagery reads as a wireframe, and a wireframe invites "make it look real" in review — a wasted round. The bar is the quality of a well-funded product site or app: a designer at a good studio would put their name under it.

**Core principle:** Decide the look, then execute it with craft — hierarchy, rhythm, depth, real imagery, finished components — all from the design system's tokens, all inside what the sanitizer keeps (HTML, CSS, inline SVG, `data:` images and fonts).

You have more room than a wireframe suggests: CSS gradients and masks, layered `box-shadow`, `backdrop-filter`, `clip-path`, `color-mix()`, grid and `aspect-ratio`, inline SVG for any illustration, chart, icon or pattern, and `data:` images. Use them. The one limit is the design system: craft is expressed through its tokens and components; a flourish it does not hold is proposed (`propose_design_system`), not slipped in as a raw value.

## 1. Layout and grid

- Pick a grid per width and hold it: 12 columns with a 1200–1280 px content max at 1440, 8 columns at 768, one column with 16–20 px gutters at 375. Every block snaps to it.
- Whitespace is a material. Section padding of 80–128 px at 1440 (48–64 at 375) on marketing surfaces; 24–32 px page padding on dense app screens. Space between groups is at least twice the space inside a group.
- One reading path per screen: a focal point (the primary action or the headline), a secondary tier, the rest quiet. Squint at the screenshot — the eye must land in the right place.
- Vary the rhythm. Five identical stacked cards is a template, not a design: alternate full-bleed bands, split sections (text + visual, flipped on the next), a bento or asymmetric grid, a stat strip, a wide quote. Alignment stays strict while the composition varies.
- Wide frames use the width: 1440 is not the 375 layout stretched. Add the second column, the side panel, the filter rail, the preview pane the room earns.

## 2. Typography

- A real type system: one display face and one text face at most (one family in several weights is often best). Name the face and why in the notes; the design system's face wins when it has one.
- A modular scale (1.2–1.333): marketing display 56–88 px at 1440 (36–44 at 375), line height 1.0–1.1, tracking −1 to −3%; headings 28–40; body 16–18 at 1.5–1.65; captions 12–14. Body lines 55–75 characters.
- Hierarchy by size and weight, not colour alone. Numerals tabular (`font-variant-numeric: tabular-nums`) in tables and stats.
- Typographic detail: proper quotes and dashes, `text-wrap: balance` on headlines, letter-spaced uppercase only for small labels and not above every heading (avoid-ai-default-looks).

## 3. Colour, depth and surface

- Neutrals carry the page (5–7 steps tinted toward the brand hue, never pure #000 on #FFF); the brand colour sits where action or identity lives; a status palette used consistently, always with a text or icon cue.
- Depth with restraint: a 3-level elevation scale (flat, raised, floating) built from layered shadows — a tight contact shadow plus a large soft one, low opacity, tinted by the surface — and 1 px borders at 6–10% opacity on dark surfaces. The same elevation for the same kind of thing.
- Surfaces with character: a soft radial or mesh glow behind a hero in the brand hues, a faint SVG grid or dot pattern, a gradient border on the one featured card, glass only where content scrolls under it. One of these per screen, as the bold move — not all.
- Dark theme is designed, not inverted: lower elevation contrast, desaturate accents, hold AA text contrast, turn shadows into borders and lighter surfaces.

## 4. Imagery and illustration

- Never a grey rectangle with "image". Draw it: product shots as a faithful mini-UI from the same components (a framed window or device with realistic content), brand and spot illustrations as inline SVG (geometric shapes, gradients, overlapping forms in the palette), avatars as SVG initials on tinted discs, customer logos as tidy wordmarks set in type, charts as real SVG with axes, gridlines, labels, a highlighted series and plausible data.
- Photography stand-ins: a layered gradient with an SVG silhouette; the notes say which real photo the team should source and its art direction (subject, crop, light).
- Icons: one set, one stroke width and corner style, on the 4/8 px grid (16/20/24), optically centred. Icons in tinted tiles on every feature is a reflex — use them where they carry meaning.
- Empty and error states get a small illustration or drawn glyph with the same care as the default.

## 5. Components that look finished

- Buttons: 40–48 px high, a clear primary / secondary / tertiary hierarchy, an icon only when it helps, a visible offset focus ring.
- Inputs: label above, helper or error text below, 44 px minimum height, a drawn value or caret, an adornment (icon, unit, button) where the field calls for it.
- Tables and lists: row height 48–56, a sticky header, right-aligned numerals, a muted secondary line, status as a chip with a dot, row actions drawn, a selected row and a hover row shown.
- Cards only when the thing is a unit that is moved, compared or acted on — otherwise a divider and spacing. Never a card inside a card.
- Navigation: a believable header (logo mark, links, account menu with avatar, a primary action), an active indicator, a footer with real link groups, breadcrumbs or back where depth exists.
- Feedback drawn over the screen it belongs to: toasts, banners, skeletons shaped like the content, progress, inline validation, confirm dialogs.

## 6. Content and microcopy

- Real, specific copy at real lengths (real-content-no-lorem): numbers with units and trends, names from the audience's locale, dates in its format, one plausible long string.
- Headlines state the benefit or the state; buttons name the action (`Send invoice`, not `Submit`); empty states say why and what next (ux-copy).
- Social proof, pricing, FAQ and testimonial blocks carry varied, believable items — different lengths and roles, never five identical ones.

## 7. Detail pass before you attach

Walk each screen once, zoomed in at 1440 and at 375. Anything unticked is fixed or named in the notes with the reason.

- [ ] One focal point; the eye lands on the primary action or headline first.
- [ ] Spacing follows one scale (4/8 px steps); groups clearly separated.
- [ ] Everything aligns to the grid; edges line up across sections.
- [ ] Type scale applied, line lengths sane, no orphans on headlines, tabular numerals where they stack.
- [ ] Neutrals tinted and layered, accent used with intent, AA contrast on every text pair (computed, not assumed), status never by colour alone.
- [ ] Elevation consistent; borders and shadows tuned for both themes.
- [ ] No grey placeholder anywhere — every image, avatar, chart and logo is drawn.
- [ ] Components show their finished details: icons, adornments, hover and selected rows, chips, helper text.
- [ ] Composition varies from section to section while alignment does not.
- [ ] 375 is its own design (targets ≥ 44 px, a thumb-reachable primary action, a sensible stack order), not a squeezed 1440.
- [ ] Dark theme looks intentional.
- [ ] The notes name the one bold move, what the look draws on (a field or genre, never a brand to copy), and which AI-default looks were checked.

## Common Mistakes

- Treating "no lorem, every state drawn" as done — a complete wireframe is still a wireframe.
- Flat single-weight type at one size.
- Every section the same centred block; every card the same shadow.
- Decoration without information: gradients and glass everywhere, none carrying hierarchy.
- Placeholder rectangles for images, avatars or charts.
- A colour, shadow or radius invented in a component instead of proposed as a token.
- Copying a known brand's look instead of deriving one from the audience and the design system.

## Red Flags

- The first screenshot looks like a grey-box wireframe.
- A hero with a headline, a subline, two buttons and nothing else.
- All icons in identical tinted tiles; all sections the same height.
- Text under 12 px, contrast not computed.
- A 375 frame that is the 1440 layout in a narrow box.
