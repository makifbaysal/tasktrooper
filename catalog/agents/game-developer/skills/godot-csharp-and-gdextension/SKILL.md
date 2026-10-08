---
name: godot-csharp-and-gdextension
category: architecture
description: Use when a Godot project uses or considers C# (.NET) or native code - when C# is the right call and its export limits (no web export), C# node patterns with generated SignalName/StringName caching, plain C# rule libraries tested with dotnet test, and GDExtension (godot-cpp, godot-rust) pinning and build hygiene.
tech_stack: Godot
source: informed by the Godot 4 C#/.NET and GDExtension documentation; own wording
---
# Godot C# and GDExtension

## Overview

Godot runs GDScript, C# (.NET) and native code through GDExtension (C++ via godot-cpp, Rust via godot-rust, and others). Each has a cost: C# needs the .NET build of the editor and cannot export to the web; GDExtension needs a native build per target platform. The repository's existing choice wins; adding a second language is a decision, not a convenience.

**Core principle:** GDScript by default, C# where the project already is .NET or the team needs it and the web is not a target, GDExtension only for measured hot spots or native libraries.

## When which

| Need | Choice |
|---|---|
| Web export required | GDScript (C# web export is still unsupported in Godot 4.7) |
| Team is C#, heavy algorithms, .NET libraries | C# — on desktop and mobile targets |
| A measured hot loop GDScript cannot meet the budget with | GDExtension (C++ or Rust), or move it to C# if the project is .NET |
| Binding an existing native SDK | GDExtension |

Mixing GDScript and C# in one project works (cross-language calls go through `Call`, `Get`, signals), but keep one language per feature and a clear boundary.

## C# nodes

```csharp
using Godot;

public partial class Player : CharacterBody2D
{
    [Signal] public delegate void HealthChangedEventHandler(int current, int maximum);

    [Export] public PlayerDefinition Definition { get; set; }

    static readonly StringName JumpAction = "jump";
    static readonly StringName MoveLeft = "move_left";
    static readonly StringName MoveRight = "move_right";

    AnimatedSprite2D _sprite;
    int _health;

    public override void _Ready()
    {
        _sprite = GetNode<AnimatedSprite2D>("AnimatedSprite2D");
        _health = Definition.MaxHealth;
    }

    public override void _PhysicsProcess(double delta)
    {
        var v = Velocity;
        v.X = Input.GetAxis(MoveLeft, MoveRight) * Definition.MoveSpeed;
        if (IsOnFloor() && Input.IsActionJustPressed(JumpAction)) v.Y = -Definition.JumpSpeed;
        v.Y += Definition.Gravity * (float)delta;
        Velocity = v;
        MoveAndSlide();
    }

    public void TakeDamage(int amount)
    {
        _health = Mathf.Max(0, _health - amount);
        EmitSignal(SignalName.HealthChanged, _health, Definition.MaxHealth);
    }
}
```

- Classes deriving from Godot types are `partial` (source generators fill in the rest); the file name matches the class name.
- Use the generated `SignalName`, `PropertyName` and `MethodName` members instead of string literals.
- **Cache `StringName`s.** Passing a C# `string` to `Input.IsActionPressed("jump")` converts it to a `StringName` on every call — an allocation per frame per call.
- Prefer .NET collections inside C# code; `Godot.Collections.Array`/`Dictionary` marshal on every access and belong only at the engine boundary (exports, signals).
- Cache node references in `_Ready`; never `GetNode` inside `_Process`.
- `delta` is a `double` in Godot 4 C#.

## Rules in a plain library

```
Game.Core/          net8.0 class library — no Godot reference; rules, math, state machines
Game.Core.Tests/    xUnit or NUnit, references Game.Core
Game/               the Godot project (.csproj references Game.Core)
```

`dotnet test Game.Core.Tests` runs anywhere, with or without Godot installed (testable-game-logic). Engine-level C# tests go through gdUnit4Net (godot-testing).

## GDExtension

```ini
; res://bin/pathfinding.gdextension
[configuration]
entry_symbol = "pathfinding_library_init"
compatibility_minimum = "4.7"

[libraries]
macos.debug = "res://bin/libpathfinding.macos.template_debug.framework"
macos.release = "res://bin/libpathfinding.macos.template_release.framework"
linux.debug.x86_64 = "res://bin/libpathfinding.linux.template_debug.x86_64.so"
windows.debug.x86_64 = "res://bin/pathfinding.windows.template_debug.x86_64.dll"
```

- **C++ (godot-cpp):** pin the godot-cpp branch/tag to the project's Godot minor; build with SCons (`scons platform=macos target=template_debug`) or the CMake setup the repository uses; register classes in the init function.
- **Rust (godot-rust / gdext):** pin the `godot` crate version and its API feature to the project's Godot minor; `cargo build`; `#[derive(GodotClass)]` types appear as nodes.
- Commit the `.gdextension` file and the source; commit built binaries only if the repository already does (otherwise CI builds them).
- Keep the native API small and coarse-grained: one call that processes a batch beats a thousand calls from GDScript per frame — crossing the boundary has a cost.
- Unit-test the native core in its own language (C++ test runner, `cargo test`) without the engine; test the binding from GDScript with gdUnit4.

## Export limits to state up front

- C#: no web export in Godot 4.7; mobile exports are supported but check the pinned version's notes; the export template must be the .NET one.
- GDExtension: one binary per platform and architecture; a missing entry in `[libraries]` means the feature silently fails on that platform — test the export for every target the task names, or say which you could not build.

## Common Mistakes

- Choosing C# for a game whose task lists the browser as a platform.
- String literals for action names and signal names in hot C# paths.
- Godot collections used for internal C# state.
- A GDExtension built against a different Godot minor than the project, failing to load at runtime.
- Calling into native code per entity per frame instead of per batch.

## Red Flags

- A new `.csproj` appearing in a GDScript-only project, or GDScript files appearing in a C#-only one, for a task that did not ask for it.
- `GetNode` inside `_Process` / `_PhysicsProcess`.
- A `.gdextension` file with platforms listed whose binaries are not built anywhere.
