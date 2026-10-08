---
name: web-game-performance-and-testing
category: performance
description: Use when working on any browser game (Phaser, PixiJS, three.js, Babylon.js, PlayCanvas, Excalibur, KAPLAY) - requestAnimationFrame with clamped delta and visibility pause, zero-allocation frame loops, measuring in DevTools, bundle and asset size budgets, Vitest with seeded RNG and injected time, and a seeded Playwright smoke test with console capture.
tech_stack: Web 2D
source: informed by the MDN, Chrome DevTools, Vite, Vitest and Playwright documentation; own wording
---
# Web Game Performance and Testing

## Overview

A browser game shares a thread with the page, a garbage collector with every closure, and a download budget with the player's patience. It is also the easiest kind of game for you to verify end to end on this machine: the build, the unit tests and a real browser run are all available. Use them.

**Core principle:** No allocations in the frame loop, a measured bundle, rules tested in Vitest with injected time and a seed, and one seeded Playwright smoke test that fails on console errors.

## The frame loop in the browser

- Drive it with `requestAnimationFrame` (or the engine's loop built on it); never `setInterval`.
- Browsers throttle or stop rAF in hidden tabs: clamp the delta (≤ 0.25 s), pause the simulation on `document.visibilitychange`, and resume without a catch-up burst.
- Fixed-step simulation with an accumulator and interpolated rendering (game-loop-and-time); engines with built-in physics steps (Phaser Arcade) already do this for bodies.
- Audio needs a user gesture: create or `resume()` the `AudioContext` on the first click/key/tap.
- On touch devices the canvas sets `touch-action: none` so gestures do not scroll or zoom the page.

## No garbage in the loop

```ts
// ❌ allocates every frame: new vectors, arrays, closures
function update(dt: number) {
  const dir = new THREE.Vector3().subVectors(target.position, enemy.position).normalize();
  enemies.filter((e) => e.alive).forEach((e) => e.step(dt));
  hud.setText(`Score: ${score}`);
}

// ✅ reuses module-level temporaries; plain loops; updates text only on change
const tmp = new THREE.Vector3();
let shownScore = -1;
function update(dt: number) {
  tmp.subVectors(target.position, enemy.position).normalize();
  for (let i = 0; i < enemies.length; i++) if (enemies[i].alive) enemies[i].step(dt);
  if (score !== shownScore) { shownScore = score; hud.setText(`Score: ${score}`); }
}
```

Pool bullets, particles and damage numbers; prefer typed arrays (`Float32Array`) for large particle or boid sets; avoid `Array.prototype.map/filter/forEach` with fresh closures in hot paths.

## Measure

- Chrome DevTools Performance: record 5–10 s of the changed scene, read frame times and the Bottom-up tab; look for GC events (`Minor GC`) in the frame track.
- Memory: heap snapshot before entering and after leaving a level — growth means a leak (undisposed textures, listeners left on a global emitter, pooled objects never released).
- Engine counters: `renderer.info` (three.js), Babylon Inspector statistics, PixiJS devtools, Phaser's debug FPS.
- In code: `performance.mark`/`performance.measure` around a system to get numbers into a test or log.
- Never take frame-rate numbers from headless Chromium — it renders WebGL in software. Report numbers from a real browser profile, or say you could not.

## Bundle and assets

- Read the build output: `npm run build` lists chunk sizes; add `rollup-plugin-visualizer` only if the repository already has it.
- Keep the engine and the first scene in the initial chunk; split later levels with dynamic `import()`.
- Textures: atlases and compressed formats (KTX2 for 3D, WebP/AVIF for 2D); audio: compressed (OGG/Opus plus an AAC/M4A fallback where Safari needs it).
- Respect the repository's size budget; without one, report the initial JS gzip size and the first-scene asset weight in the closing message when you changed either.

## Vitest for rules

```ts
// src/logic/rng.ts
export function mulberry32(seed: number) {
  return () => { seed |= 0; seed = (seed + 0x6d2b79f5) | 0; let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t; return ((t ^ (t >>> 14)) >>> 0) / 4294967296; };
}
```

```ts
// src/logic/spawner.test.ts
import { describe, it, expect } from "vitest";
import { Spawner } from "./spawner";
import { mulberry32 } from "./rng";

describe("Spawner", () => {
  it("spawns one wave every 5 seconds regardless of step size", () => {
    const a = new Spawner({ waveEverySeconds: 5 }, mulberry32(1));
    const b = new Spawner({ waveEverySeconds: 5 }, mulberry32(1));
    for (let i = 0; i < 600; i++) a.step(1 / 60);
    for (let i = 0; i < 300; i++) b.step(1 / 30);
    expect(a.wavesSpawned).toBe(2);
    expect(b.wavesSpawned).toBe(2);
  });

  it("is deterministic for a seed", () => {
    const run = () => { const s = new Spawner({ waveEverySeconds: 1 }, mulberry32(42)); for (let i = 0; i < 180; i++) s.step(1 / 60); return s.positions(); };
    expect(run()).toEqual(run());
  });
});
```

- Rules never import the engine, so tests run in Node without a canvas. Don't mock Phaser or three.js to test a scene — move the rule out.
- Time is a parameter; where a module genuinely uses timers, `vi.useFakeTimers()` and `vi.advanceTimersByTime(ms)`.
- Babylon scene logic can run under `NullEngine` in Vitest.

## Playwright smoke test (seeded)

```ts
// e2e/smoke.spec.ts
import { test, expect } from "@playwright/test";

test("boots, takes input, no console errors", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });

  await page.goto("/?seed=42&scene=Level1&debug=1");
  await expect(page.locator("canvas")).toBeVisible();
  await expect.poll(() => page.evaluate(() => (window as any).__GAME__?.ready)).toBe(true);

  await page.keyboard.press("Space");
  await expect.poll(() => page.evaluate(() => (window as any).__GAME__?.player.jumps)).toBe(1);

  await page.screenshot({ path: "test-results/level1-after-jump.png" });
  expect(errors).toEqual([]);
});
```

- A `window.__GAME__` read-only debug handle exposed only in debug/dev builds lets tests assert state instead of guessing from pixels.
- Fixed seed and a direct scene parameter make runs reproducible. Pixel comparisons (`toHaveScreenshot`) only with the loop paused on a deterministic frame and a tolerance; GPU differences make exact matches flaky.
- Run it the way the repository does (`npx playwright test`); if Playwright is not set up and not installed, use the browser tools and say what the smoke run would have covered (game-visual-self-review).

## Common Mistakes

- `setInterval` loops; unclamped delta after a background tab.
- Temporaries allocated per frame in vector math.
- Unit tests importing the engine and failing for lack of WebGL.
- Smoke tests without a seed, flaking on random spawns.
- Judging performance from a headless browser.

## Red Flags

- `new` inside the rAF callback or a system's `update`.
- A Playwright test with no `pageerror` listener.
- A bundle that grew by hundreds of KB for a small feature, unmentioned in the closing message.
