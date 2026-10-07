---
name: mobile-visual-self-review
category: mobile
description: Use when you are about to hand off any change to a screen or component on a mobile repository - the size, text-scale and dark-mode matrix, the exact tool calls to see your own build, and the checklist that decides whether the screen is done.
source: ehmo/platform-design-skills (MIT), flutter/agent-plugins (BSD-3-Clause), android/skills (Apache-2.0), wshobson/agents (MIT), adapted
---
# Mobile Visual Self-Review

## Overview

A green build and a passing widget test say the code compiles and the tree is what the test asserted. Neither says the screen looks right on a 360 phone at 200% text, on a tablet, in landscape, or in dark mode. Default assumption: the change is NOT done until you've looked at it rendered — be the skeptic here, not the advocate.

**Core principle:** two layers, always both. A matrix test in the suite catches overflow mechanically on every future change; looking at the rendered screen catches what a test assertion can't (a placeholder glyph, a clipped icon, invisible text in dark mode).

## The matrix

360×640 (phone) · 430×932 (large phone) · 640×360 landscape · 768×1024 (tablet) — each at text scale 1.0 and 2.0, and in dark mode.

## Layer 1 — a matrix test in the repo's test suite

**Flutter** (verified, Flutter 3.41.6 — 11/11 green after the fix below):

```dart
const sizes = <String, Size>{
  'phone 360x640': Size(360, 640),
  'large phone 430x932': Size(430, 932),
  'landscape 640x360': Size(640, 360),
  'tablet 768x1024': Size(768, 1024),
};

for (final e in sizes.entries) {
  for (final scale in [1.0, 2.0]) {
    testWidgets('PriceRow at ${e.key}, text x$scale, dark', (tester) async {
      tester.view.physicalSize = e.value * 3;
      tester.view.devicePixelRatio = 3;
      tester.platformDispatcher.textScaleFactorTestValue = scale;
      addTearDown(tester.view.reset);
      addTearDown(tester.platformDispatcher.clearAllTestValues);
      final handle = tester.ensureSemantics();
      await tester.pumpWidget(host(PriceRow(title: longRealisticTitle, price: '1.299,00 TL', onAdd: () {}),
          brightness: Brightness.dark));
      expect(tester.takeException(), isNull);                        // overflow = FlutterError here
      await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
      await expectLater(tester, meetsGuideline(iOSTapTargetGuideline));
      await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
      handle.dispose();
    });
  }
}
```

Verified result: `Row[Expanded(title), price, IconButton]` passes every case at text 1.0. At text 2.0 it fails — *"A RenderFlex overflowed by 82 pixels on the right"* at 360 (122 at 320, 12 at 430). The fix that passes all cases: wrap in `LayoutBuilder`; if `constraints.maxWidth < 280 * MediaQuery.textScalerOf(context).scale(1)`, render `Column[title, Row[Expanded(price), IconButton]]` instead of the single `Row`. This is the ❌/✅ worked example for both this skill and `mobile-ui-ux`.

**Compose:** `DeviceConfigurationOverride(ForcedSize(DpSize(360.dp, 640.dp)) then FontScale(2f) then DarkMode(true)) { ... }` around the composable under test, repeated per matrix cell.

**SwiftUI:** `#Preview` with `.environment(\.dynamicTypeSize, .accessibility3)`, `.preferredColorScheme(.dark)` and `traits: .landscapeLeft` (typechecks against the iOS 18 simulator SDK — verified) — plus snapshot tests if the repo already has them.

## Layer 2 — look at the rendered screen

Pick the first path that applies:

**(a) Host-local emulator/simulator — your own build.**
1. `mobile_launch_app {repository_id}` — this takes the device lease and installs the build registered on the deploy target (the stage build), not your branch.
2. Put your own build on the device:
   - Android, local emulator:
     ```
     flutter build apk --debug
     adb devices
     adb -s <serial> uninstall <pkg>        # only needed on INSTALL_FAILED_UPDATE_INCOMPATIBLE
     adb -s <serial> install build/app/outputs/flutter-apk/app-debug.apk
     adb -s <serial> shell monkey -p <pkg> -c android.intent.category.LAUNCHER 1
     ```
     Native Android: `ANDROID_SERIAL=<serial> ./gradlew installDebug`.
   - iOS, local simulator:
     ```
     flutter build ios --simulator --debug
     xcrun simctl install <udid> build/ios/iphonesimulator/Runner.app
     xcrun simctl launch <udid> <bundle-id>
     ```
     Native iOS: `xcodebuild -scheme <S> -destination 'id=<udid>' -derivedDataPath build build`, then `simctl install` the `.app`.
3. `mobile_wait_for` the new element → `mobile_screenshot` → `mobile_rotate {orientation: landscape}` → `mobile_screenshot` → `mobile_rotate {orientation: portrait}`.
4. Large text and dark mode, local only:
   - Android: `adb shell settings put system font_scale 2.0` and `adb shell cmd uimode night yes`; reset with `font_scale 1.0` and `night no`.
   - iOS: `xcrun simctl ui <udid> content_size accessibility-extra-extra-extra-large` and `xcrun simctl ui <udid> appearance dark` (simctl options verified). Reset with `content_size large` and `appearance light`.
5. TalkBack proxy: `mobile_read_ui {filter: "clickable=\"true\""}` — see below.
6. `mobile_release_device` as your last mobile step.

**(b) Remote/shared phone with no local install path.** It shows the stage build only — use (c) or (d) to see your own change; say so in the closing message.

**(c) Flutter widget previewer (no device needed, verified).** Add `@Preview(name: 'phone dark 2x', size: Size(360, 640), brightness: Brightness.dark, textScaleFactor: 2.0)` beside the widget (`import 'package:flutter/widget_previews.dart'`). Run `mkdir -p /tmp/tt-<task key> && flutter widget-preview start --web-server > /tmp/tt-<task key>/preview.log 2>&1 &`. The log prints `lib/main.dart is being served at http://localhost:<port>`; then `browser_navigate` → `browser_screenshot`. The previewer runs on the web: a widget touching `dart:io` or a plugin needs a fake in the preview.

**(d) Native screenshots over loopback.** Start a loopback server over the screenshot directory, e.g. `mkdir -p /tmp/tt-<task key> && python3 -m http.server 8799 --bind 127.0.0.1 -d <dir> > /tmp/tt-<task key>/shots.log 2>&1 &` (`py -3` instead of `python3` on Windows). Then `browser_navigate http://127.0.0.1:8799/<file>.png` → `browser_screenshot`. `file://` is refused by the browser guard; loopback http is allowed by default.

If a device call reports the device busy, stop: the task is parked and resumes by itself — do not retry in a loop.

## TalkBack/VoiceOver proxy on device

`mobile_read_ui {filter: "clickable=\"true\""}`. Every line needs a non-empty `text` or `content-desc`; bounds ≥48dp × (dpi/160) px on Android, ≥44pt on iOS. Verify the line-per-node shape on first use — the exact field names can differ by platform.

## Checklist (yes/no, each with a threshold)

- **Overflow / clipping:** none in any cell of the matrix.
- **Insets:** essential content clear of the status bar, notch, home indicator and keyboard.
- **Targets:** touch targets ≥48dp / 44pt.
- **Text:** essential text not truncated at 2.0; scales with the system setting (no hard-coded sizes).
- **States:** loading, empty and error all reachable and designed.
- **Dark mode:** intentional (no black-on-dark, no invisible icons); status-bar icons legible.
- **Consistency:** one primary action; the same control looks identical everywhere (built from the same atom).
- **Copy:** in the locale; no placeholder text.
- **Navigation:** landscape keeps the primary action reachable; tablet doesn't stretch text past ~600–840dp (constrain or use two panes).

## Batch, then fix, then one re-check

Collect every finding from every matrix cell before touching code. Fix everything from the batch in one pass, then re-run the full matrix once. At most two rounds total — whatever is still open after round two goes into the closing message, honestly, rather than triggering a third silent pass.

## Closing message

Name the render path used (device-own-build / device-stage-only / previewer / simulator) and, for each matrix size checked, what you saw. A known gap reported honestly is not a failure; an unverified claim of "done" is.

## Common Mistakes

- Treating a screenshot taken right after `mobile_launch_app` (with no own-build install) as proof of your change — it's the stage build.
- Skipping the 2.0 text-scale cell because 1.0 looked fine — the overflow in the worked example only appears at 2.0.
- `--update-goldens` or regenerating a snapshot to make a red matrix test green.
- More than two rounds of screenshot → fix → screenshot on the same screen.

## Red Flags

- A closing message that names no render path.
- "Looks fine" without ever having set text scale to 2.0 or dark mode.
- A device-busy result retried instead of treated as parked.
