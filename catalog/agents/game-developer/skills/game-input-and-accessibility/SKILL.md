---
name: game-input-and-accessibility
category: gameplay
description: Use when adding controls, menus, HUD, subtitles or effects - input through named actions with rebinding and controller support per engine, input buffering, and the accessibility floor (remapping, colour-blind safe cues, subtitles, text scale, reduced motion and flashes).
tech_stack: Game core
source: informed by the Game Accessibility Guidelines (gameaccessibilityguidelines.com), the Xbox Accessibility Guidelines, and the Unity Input System, Godot InputMap and Unreal Enhanced Input docs; own wording
---
# Game Input and Accessibility

## Overview

Players use keyboards, mice, gamepads, touch screens and adaptive controllers, with different hands, eyes and ears. Code that reads `KeyCode.Space` directly cannot be rebound, cannot be driven by a gamepad without a second branch, and cannot be tested without a keyboard. Gameplay reads named actions; devices map to actions in one place.

**Core principle:** Gameplay never sees a key code. Every action is rebindable, every essential cue reaches the player through more than one sense, and motion and flashing can be turned down.

## Actions, per engine

```csharp
// ❌ Unity legacy, device-bound, unrebindable
if (Input.GetKeyDown(KeyCode.Space)) Jump();

// ✅ Unity Input System action (from the .inputactions asset or its generated class)
[SerializeField] InputActionReference jump;
void OnEnable()  { jump.action.performed += OnJump; jump.action.Enable(); }
void OnDisable() { jump.action.performed -= OnJump; }
void OnJump(InputAction.CallbackContext _) => _intents.Jump();
```

```gdscript
# Godot: actions from the InputMap, never physical keys in gameplay
func _unhandled_input(event: InputEvent) -> void:
    if event.is_action_pressed("jump"):
        intents.jump()
```

```cpp
// Unreal Enhanced Input
void AHeroCharacter::SetupPlayerInputComponent(UInputComponent* Input) {
    auto* EIC = CastChecked<UEnhancedInputComponent>(Input);
    EIC->BindAction(JumpAction, ETriggerEvent::Started, this, &AHeroCharacter::OnJump);
    EIC->BindAction(MoveAction, ETriggerEvent::Triggered, this, &AHeroCharacter::OnMove);
}
// The mapping context is added through UEnhancedInputLocalPlayerSubsystem::AddMappingContext.
```

```ts
// Web: map KeyboardEvent.code (layout-independent position) to actions; poll gamepads each frame
const bindings: Record<string, Action> = { Space: "jump", KeyW: "up", ArrowUp: "up" };
window.addEventListener("keydown", (e) => { const a = bindings[e.code]; if (a) input.press(a); });
// in the loop: for (const pad of navigator.getGamepads()) if (pad?.buttons[0].pressed) input.press("jump");
```

Bevy: `ButtonInput<KeyCode>` / `ButtonInput<GamepadButton>` behind an action layer (or the `leafwing-input-manager` crate if the repo uses it).

## Rebinding and persistence

- Unity: `action.PerformInteractiveRebinding()`, then `actions.SaveBindingOverridesAsJson()` to settings and `LoadBindingOverridesFromJson` at boot.
- Godot: `InputMap.action_erase_events` + `action_add_event`, saved to a `ConfigFile` under `user://`.
- Unreal: player-mappable keys through `UEnhancedInputUserSettings` (UE 5.2+) or the project's existing settings system.
- Web: the `bindings` map in settings storage.
- Conflicts are detected and shown; a reset-to-default exists; UI prompts show the glyph of the currently bound key or button.

## Controllers and touch

- Radial dead zones on sticks (not per-axis squares); sensitivity and invert-Y settings.
- Switch prompt glyphs to the last-used device; support hot-plugging.
- Every menu is fully navigable with a gamepad and with a keyboard alone — focus is visible and never lost.
- Touch: on-screen controls sized for thumbs and placed in the safe area; the web canvas sets `touch-action: none`.

## Feel: buffering and grace windows

Input buffering (a jump pressed 0.1 s before landing still jumps) and coyote time (a jump 0.1 s after walking off a ledge still counts) are measured in **seconds** on the simulation clock, and they belong to the rule class, so they get tests:

```ts
it("buffers a jump pressed 80 ms before landing", () => {
  const c = new JumpRules({ bufferSeconds: 0.1 });
  c.pressJump(); c.step(0.08); c.land(); c.step(1 / 60);
  expect(c.isJumping).toBe(true);
});
```

## Accessibility floor

Ship these unless the task explicitly scopes them out:

- **Full remapping** of every action, on every supported device; hold-to-press alternatives as toggles.
- **No information by colour alone:** team, rarity, danger and status also carry a shape, icon, pattern or label. Colour-blind presets swap the palette of gameplay-critical cues rather than only tinting the screen.
- **Subtitles and captions:** on by default or offered at first launch; adjustable size, background opacity, speaker names; important sound cues (footsteps behind you, an alarm) also shown visually.
- **Readable UI:** text scales (at least 1.5× without overlap), minimum size at the smallest supported resolution, high-contrast option for HUD text.
- **Motion and flashes:** toggles for camera shake, head bob, motion blur and screen flashes; no content that flashes more than three times per second in a large area of the screen.
- **Difficulty and assists:** where the design allows — slower game speed, aim assist, skip puzzle, extended timing windows.
- **Pause anywhere** in single-player, including during cutscenes.

Settings live in data and persist (game-save-and-persistence); each option has a test that the setting changes the behaviour it promises (e.g. reduced motion → shake amplitude is zero).

## Common Mistakes

- Reading device keys in gameplay code; a second `if` branch per device.
- `KeyboardEvent.key` for movement (breaks on AZERTY) instead of `code`.
- Coyote time or buffering counted in frames.
- Colour-only health bars, team markers or loot rarity.
- Subtitles without size or background options; no visual cue for critical audio.

## Red Flags

- `Input.GetKey`, `Input.is_key_pressed`, `IsInputKeyDown` or raw `keydown` handling in a gameplay class.
- A new action with no default binding for the gamepad the game supports.
- A new screen-flash or shake effect with no setting that disables it.
