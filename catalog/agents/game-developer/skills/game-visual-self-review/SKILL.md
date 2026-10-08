---
name: game-visual-self-review
category: testing
description: Use when you are about to hand off a change a player can see - the exact steps to look at it running (web game in the browser at two viewports plus a seeded Playwright smoke run for input and console errors; engine games through a headless frame capture), the checklist, and how to report what could not be observed.
tech_stack: Game core
source: informed by the frontend and mobile visual self-review skills in this catalog and the Godot, Unity, Unreal, Bevy and Playwright docs; own wording
---
# Game Visual Self-Review

## Overview

A green test suite says the rules are right in isolation. It does not say the sprite loaded, the camera points at the action, the HUD shows the new value, or the scene is not black. Look at the change running before you hand it off — and when this machine cannot render the game, say exactly that instead of implying you saw it.

**Core principle:** Default assumption: the change is NOT done until you have seen it rendered. A still frame proves presence and layout; timing and feel are proven by tests at fixed deltas and frame-time numbers.

## The render paths

Name the one you used in the closing message:

- **browser** — a web game in the shared browser.
- **engine capture** — a frame written to PNG by the engine (headless or offscreen), opened over loopback.
- **not observable here** — no editor, GPU, display or licence; say which, and comment it on the card.

## Web games — the matrix and the tool calls

Matrix: desktop `{device: "desktop"}` (1440×900) and phone `{device: "mobile", width: 360}`; add landscape `{device: "mobile", width: 844, height: 390}` when the game targets phones.

1. Dev server detached, as the main prompt describes; read the port from the log.
2. `browser_set_viewport` → `browser_navigate` to the game with the repository's debug parameters (`?seed=42&scene=Level2`) so it opens on the changed content deterministically.
3. `browser_wait_for` the `canvas` (or the DOM overlay that proves boot finished), `browser_click` through DOM menus.
4. `browser_screenshot` with NO `width` — passing `width` re-emulates and discards the viewport you set.
5. Read the `browser_set_viewport` report: the canvas must not overflow; it fits or letterboxes.

The browser tools cannot press keys or read the console. For both, run a seeded Playwright smoke script with run_terminal — use the repository's Playwright setup if it has one (web-game-performance-and-testing), otherwise a throwaway script in `/tmp/tt-<task key>/` when `playwright` is already installed:

```js
// /tmp/tt-<task key>/smoke.mjs — node smoke.mjs <url> <outDir>
import { chromium } from "playwright";
const [url, out] = process.argv.slice(2);
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
const problems = [];
page.on("pageerror", (e) => problems.push(`pageerror: ${e.message}`));
page.on("console", (m) => { if (m.type() === "error" || m.type() === "warning") problems.push(`${m.type()}: ${m.text()}`); });
await page.goto(url);
await page.waitForSelector("canvas");
await page.waitForTimeout(1000);
await page.screenshot({ path: `${out}/boot.png` });
await page.keyboard.down("ArrowRight"); await page.waitForTimeout(500); await page.keyboard.up("ArrowRight");
await page.keyboard.press("Space"); await page.waitForTimeout(300);
await page.screenshot({ path: `${out}/after-input.png` });
console.log(JSON.stringify({ problems }, null, 2));
await browser.close();
process.exit(problems.some((p) => !p.startsWith("warning")) ? 1 : 0);
```

Then serve the PNGs and look at them: `python3 -m http.server 8799 --bind 127.0.0.1 -d /tmp/tt-<task key>/shots > /tmp/tt-<task key>/shots.log 2>&1 &`, `browser_navigate http://127.0.0.1:8799/after-input.png`, `browser_screenshot`. Never install Playwright or its browsers on your own initiative; if it is missing, the browser-tool screenshots are what you have — say so.

Headless Chromium renders WebGL in software: correct pixels, slow frames. Never quote its FPS.

## Engine games — capture a frame

**Godot** — Movie Maker writes frames (needs a real renderer, so not `--headless`; on Linux without a display wrap in `xvfb-run`):

```sh
godot --path . --write-movie /tmp/tt-<task key>/shots/frame.png --fixed-fps 60 --quit-after 120 res://scenes/level_02.tscn
```

or from a test/script after the change is on screen:

```gdscript
await RenderingServer.frame_post_draw
get_viewport().get_texture().get_image().save_png("/tmp/tt-T-42/shots/hud.png")
```

**Unity** — a PlayMode test that renders a camera into a RenderTexture, run with `-batchmode` but WITHOUT `-nographics` (no graphics device, no pixels):

```csharp
[UnityTest] public IEnumerator Hud_AfterPickup_ShowsAmmo() {
    yield return SceneManager.LoadSceneAsync("Arena");
    Object.FindAnyObjectByType<PickupSpawner>().SpawnAt(Vector3.zero, "ammo_small");
    yield return new WaitForSeconds(0.5f);
    var cam = Camera.main; var rt = new RenderTexture(1280, 720, 24);
    cam.targetTexture = rt; cam.Render(); RenderTexture.active = rt;
    var tex = new Texture2D(1280, 720, TextureFormat.RGB24, false);
    tex.ReadPixels(new Rect(0, 0, 1280, 720), 0, 0); tex.Apply();
    File.WriteAllBytes(Path.Combine(Environment.GetEnvironmentVariable("TT_SHOTS") ?? Application.temporaryCachePath, "hud.png"), tex.EncodeToPNG());
    cam.targetTexture = null; RenderTexture.active = null; Object.Destroy(rt);
}
```

Screen-space overlay UI is not drawn by `Camera.Render`; for HUD checks use a Screen Space – Camera canvas in the test scene or assert the UI values in the test as well.

**Unreal** — an automation screenshot (functional test screenshot actor, or `HighResShot 1280x720` issued during an automation run) with `-RenderOffscreen` instead of `-nullrhi`; files land under `Saved/Screenshots/` or `Saved/Automation/`.

**Bevy** — spawn a screenshot entity and save it to disk (`Screenshot::primary_window()` with the `save_to_disk` observer in recent versions; follow the pinned version's API); needs a GPU and a window.

**pygame** — works fully headless: `SDL_VIDEODRIVER=dummy`, run N frames, `pygame.image.save(screen, path)`.

**MonoGame** — draw into a `RenderTarget2D` and `SaveAsPng`; needs a graphics device.

Open every PNG over loopback with `browser_navigate` + `browser_screenshot` (`file://` is refused).

## Checklist

- The thing the task asked for is visible, in the right place, at every size of the matrix.
- No missing-asset placeholder: magenta/pink material, Unreal's grey checker (WorldGridMaterial), Godot's missing-texture icon, a broken-image box in Phaser, a black or transparent canvas.
- The camera frames the action; nothing important is cut by the letterbox, the safe area or a notch.
- HUD text is legible at 360 px wide, does not overlap, and shows the value the state says it should.
- No z-fighting, flicker, or sprites on the wrong layer.
- The smoke run reports zero `pageerror`s and zero console errors (warnings listed in the closing message).
- Animation and timing: covered by a fixed-delta test, not by squinting at two frames.

## Batch, then fix, then one re-check

Collect every finding across the matrix first, fix them in one pass, re-run the matrix once. Two rounds at most; anything still open goes into the closing message.

## Closing message

Render path; each viewport or capture with what it showed; problems from the smoke run; and, when nothing could render, exactly why (`NOT OBSERVED: Unity 6000.3.2f1 not installed; PlayMode capture test written, not run`).

## Common Mistakes

- Screenshotting the title screen and calling the level change verified — jump to the changed content with the debug seed/scene.
- Passing `width` to `browser_screenshot` after `browser_set_viewport`.
- Running a capture with `--headless` or `-nographics` and getting an empty image.
- Treating "no console errors in the dev-server log" as "no runtime errors" — the server log never sees the page.

## Red Flags

- A closing message on a visual change that names no render path.
- "Looks fine" on a game repository where no screenshot or capture was taken in this run.
- More than two rounds of capture → fix → capture on the same change.
