---
name: other-engines-quickref
category: engine
description: Use when the repository is Defold, MonoGame, pygame/pygame-ce, Excalibur or KAPLAY (or melonJS, Stride, Python arcade, macroquad/ggez) - each engine's lifecycle and delta, where logic goes, the test runner, the headless build/run command, and how to capture a frame.
tech_stack: Other engines
source: informed by the Defold, MonoGame, pygame-ce, Excalibur and KAPLAY documentation; own wording
---
# Other Engines Quick Reference

## Overview

Smaller engines follow the same rules as the big ones — fixed-step simulation, delta-scaled time, rules in testable plain modules, no allocations per frame, data out of code — with their own lifecycle names and tools. Find the engine's conventions in the repository first; this page is the map when they are thin.

**Core principle:** Same discipline, local vocabulary. Identify the loop callback and its delta unit, put rules in plain modules the language's test runner can import, and find the command that builds and runs headless.

## At a glance

| Engine | Language | Per-frame callback, delta unit | Fixed step | Rules tested with | Headless build / run | Frame capture |
|---|---|---|---|---|---|---|
| Defold | Lua | `update(self, dt)` — seconds | `fixed_update(self, dt)` when enabled in `game.project` | plain Lua modules + the repo's runner (busted, or an in-engine runner such as deftest/telescope) | `java -jar bob.jar --platform <platform> resolve build`; `dmengine_headless` for in-engine tests | `screenshot` extension, or none headless |
| MonoGame | C# | `Update(GameTime)` — `gameTime.ElapsedGameTime.TotalSeconds` | `IsFixedTimeStep = true` (default), `TargetElapsedTime` | class library + xUnit/NUnit, `dotnet test` | `dotnet build`; content via `dotnet mgcb` / the `.mgcb` project | `RenderTarget2D` → `SaveAsPng` (needs a graphics device) |
| pygame / pygame-ce | Python | your loop: `dt = clock.tick(60) / 1000` — seconds | your accumulator | pytest | `SDL_VIDEODRIVER=dummy SDL_AUDIODRIVER=dummy python -m game --frames 120` | `pygame.image.save(screen, path)` — works headless |
| Excalibur | TypeScript | `onPreUpdate(engine, elapsed)` — milliseconds | the engine's fixed-update option (`fixedUpdateFps` in recent versions) | Vitest | `npm run build`; browser for running | browser tools / Playwright |
| KAPLAY | JS/TS | `onUpdate(() => ...)` with `dt()` — seconds | the engine's fixed update for `body()` physics | Vitest on modules outside the `kaplay()` context | `npm run build`; browser for running | browser tools / Playwright |

## Defold

- Game objects with components (sprite, collision object, script), composed into collections; communication by messages (`msg.post(url, "damage", { amount = 10 })`) handled in `on_message`, input in `on_input` after `msg.post(".", "acquire_input_focus")`.
- Script properties (`go.property("speed", 200)`) for per-instance tuning; shared tuning in Lua data modules or JSON resources.
- Rules in plain Lua modules (`require "game.rules.combat"`) with no `go.`/`msg.` calls, so they run under a plain Lua test runner; engine glue in `.script` files.
- No per-frame table creation in `update`; reuse `vmath.vector3` values where the hot path allows.
- Atlases and texture profiles (`.texture_profiles`) for per-platform compression; HTML5 is a first-class target.

## MonoGame

```csharp
public sealed class ArenaGame : Game {
    readonly GraphicsDeviceManager _graphics;
    SpriteBatch _batch; World _world;                         // World: plain C# rules from Game.Core
    public ArenaGame() { _graphics = new GraphicsDeviceManager(this); IsFixedTimeStep = true; TargetElapsedTime = TimeSpan.FromSeconds(1.0 / 60); }
    protected override void LoadContent() { _batch = new SpriteBatch(GraphicsDevice); _world = new World(Content.Load<WorldData>("arena")); }
    protected override void Update(GameTime t) { _world.Step((float)t.ElapsedGameTime.TotalSeconds, InputMapper.Read()); base.Update(t); }
    protected override void Draw(GameTime t) { GraphicsDevice.Clear(Color.Black); _batch.Begin(samplerState: SamplerState.PointClamp); WorldRenderer.Draw(_batch, _world); _batch.End(); base.Draw(t); }
}
```

- Rules in a `Game.Core` class library with no MonoGame reference; `dotnet test` runs everywhere.
- One `SpriteBatch.Begin/End` per layer, sprites from atlases; `DrawString` with a cached `StringBuilder` for changing numbers; no LINQ or `new` in `Update`/`Draw`.
- Assets go through the content pipeline (`Content.mgcb`); commit the `.mgcb` entries with the source assets.

## pygame / pygame-ce

```python
def run(frames: int | None = None) -> None:
    pygame.init()
    screen = pygame.display.set_mode((1280, 720))
    clock, world = pygame.time.Clock(), World(load_tuning("data/tuning.json"), random.Random(SEED))
    hero = pygame.image.load("art/hero.png").convert_alpha()      # convert once, never per frame
    acc, step, n = 0.0, 1 / 60, 0
    while frames is None or n < frames:
        acc += min(clock.tick(120) / 1000, 0.25)
        for event in pygame.event.get():
            if event.type == pygame.QUIT: return
        while acc >= step:
            world.step(step, read_intents()); acc -= step
        draw(screen, world, hero); pygame.display.flip(); n += 1
```

- `World` and its rules import nothing from pygame; pytest tests them directly.
- `convert()`/`convert_alpha()` every loaded surface (unconverted blits are several times slower); `pygame.sprite.Group` for batches.
- Know which package the repo uses (`pygame` or the community fork `pygame-ce`); they are API-compatible for most code but are installed separately.
- Web builds go through pygbag, which needs an `async` main loop with `await asyncio.sleep(0)` each frame.
- Never `pickle` saves (game-save-and-persistence).

## Excalibur

- `new Engine({ width, height, displayMode: DisplayMode.FitScreen, fixedUpdateFps: 60 })`, `Scene`s, `Actor`s with components; `Loader` with `ImageSource`/`Sound` resources.
- `onPreUpdate(engine, elapsedMs)` — milliseconds; convert once.
- Rules in plain TS modules for Vitest; actors only translate rule state into transforms and graphics.

## KAPLAY (the maintained successor of Kaboom)

- `const k = kaplay({ width: 1280, height: 720, letterbox: true })`; objects are component lists: `k.add([k.sprite("hero"), k.pos(80, 40), k.area(), k.body(), "player"])`.
- Scenes with `k.scene("game", () => { ... })` and `k.go("game")`; input with `k.onKeyPress("space", jump)`; movement scaled by `k.dt()`.
- The `kaplay()` context is global and needs a canvas — keep rules in modules that never call it so Vitest can import them.

## Also detected as games

- **melonJS** (TS/JS): `me.game`, `me.Renderable`; same web testing approach.
- **Stride** (C#): component scripts (`SyncScript.Update`), `Game.UpdateTime.Elapsed`; rules in a class library.
- **Python arcade**: `on_update(delta_time)` seconds; pytest on plain modules.
- **macroquad / ggez** (Rust): `get_frame_time()` / `ctx.time.delta()`; `cargo test` on plain modules.

## Common Mistakes

- Treating Excalibur's elapsed milliseconds as seconds.
- pygame surfaces never converted; loading images inside the loop.
- Defold game logic spread through `on_message` handlers with no testable module.
- MonoGame content added to the folder but not to the `.mgcb` project.

## Red Flags

- An engine import inside a module the tests are supposed to run without the engine.
- A loop with no delta at all ("one step per frame").
- A run reported as tested where only the build ran.
