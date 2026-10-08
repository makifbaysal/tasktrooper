---
name: godot-gdscript-patterns
category: architecture
description: Use when writing Godot 4 GDScript - static typing everywhere, scene composition with component nodes, call down/signal up, @onready caching, Resources as data duplicated per instance, autoloads only for services, state machines, pooling, physics in _physics_process, and threaded loading.
tech_stack: Godot
source: wshobson/agents godot-gdscript-patterns (MIT), adapted; Godot 4 documentation cited
---
# Godot GDScript Patterns

## Overview

Godot's building blocks are nodes composed into scenes, signals between them, and Resources for data. GDScript is dynamic by default, which hides typos until runtime and costs speed; typed GDScript catches them at parse time and runs faster. The common failure modes are untyped code, `get_node` chains reaching up the tree, logic in Resources, and autoloads holding everything.

**Core principle:** Type everything, compose scenes from small nodes, call down and signal up, keep data in Resources and rules in plain classes.

## Type everything

```gdscript
# ❌ untyped: typos and wrong types surface only when this line runs
var speed = 200
func take_damage(amount):
    health -= amount

# ✅ typed: checked when the script parses, faster at runtime
class_name Player extends CharacterBody2D

signal health_changed(current: int, maximum: int)
signal died

@export var definition: PlayerDefinition
@onready var _sprite: AnimatedSprite2D = $AnimatedSprite2D
@onready var _hurtbox: Area2D = %Hurtbox

var health: int

func _ready() -> void:
    health = definition.max_health

func take_damage(amount: int) -> void:
    health = maxi(0, health - amount)
    health_changed.emit(health, definition.max_health)
    if health == 0:
        died.emit()
```

Turn on Project Settings → Debug → GDScript → `untyped_declaration` (and `unsafe_*` warnings) as warnings or errors if the repository has not yet. Typed arrays and dictionaries (`Array[Enemy]`, `Dictionary[StringName, int]` in 4.4+) everywhere a collection has one element type.

## Composition with component nodes

Build behaviour from small reusable child scenes rather than deep inheritance:

```
Enemy (CharacterBody2D)          enemy.gd: wires components, no rules of its own
├── HealthComponent (Node)       health_component.gd: health, damage(), signal died
├── HitboxComponent (Area2D)     deals damage on overlap
├── HurtboxComponent (Area2D)    forwards hits to HealthComponent
└── StateMachine (Node)          child state nodes: Idle, Chase, Attack
```

Each component exports its dependencies (`@export var health: HealthComponent`) so the scene wires them in the inspector, and each is testable alone.

## Call down, signal up

```gdscript
# ❌ reaching up and across the tree: breaks when the scene is reused or rearranged
func _on_hit() -> void:
    get_parent().get_parent().get_node("UI/HUD").update_health(health)

# ✅ the child emits; whoever owns both connects them
# in level.gd:
func _ready() -> void:
    player.health_changed.connect(hud.show_health)
```

- Parents call methods on children they own; children announce events with signals.
- Connect in code with Callables (`button.pressed.connect(_on_start_pressed)`) or in the editor — follow the repository.
- `@onready` (or `%UniqueName` for unique-in-owner nodes) caches node references once; never `get_node()` or `$Path` inside `_process`.

## Resources: data, duplicated per instance

```gdscript
class_name PlayerDefinition extends Resource
@export var max_health: int = 100
@export var move_speed: float = 180.0
@export var dash_curve: Curve
```

- Resources hold data, not behaviour (no gameplay logic, no node references).
- A loaded `.tres` is shared by everyone who loads it. Runtime state goes on the node, or `definition.duplicate()` per instance, or `resource_local_to_scene` for sub-resources edited per scene.
- Never load user-provided `.tres`/`.res` files: they can carry scripts (game-save-and-persistence).

## Autoloads sparingly

Autoloads are global singletons. Acceptable for **services**: scene loading, save, audio, settings, an event bus for genuinely global events. Not for the current level's state, the player's health or anything a test needs to reset. A level's data belongs to the level scene.

## State machines

Small: an `enum` and a `match`. Larger: a `StateMachine` node with one child node per state (`enter()`, `exit()`, `physics_update(delta)`, `handle_input(event)`), transitions requested by name, and the legal transitions tested (game-state-machines-and-ai).

```gdscript
enum State { IDLE, CHASE, ATTACK }
var _state := State.IDLE

func _physics_process(delta: float) -> void:
    match _state:
        State.IDLE:
            if _target_in_range(definition.aggro_range): _change(State.CHASE)
        State.CHASE:
            _chase(delta)
            if _target_in_range(definition.attack_range): _change(State.ATTACK)
        State.ATTACK:
            pass
```

## Physics, processing and pooling

- `CharacterBody2D/3D`: set `velocity`, call `move_and_slide()` in `_physics_process(delta)`. `RigidBody`: forces/impulses, or `_integrate_forces(state)` for direct control — never set `position` on it every frame.
- Turn off what is idle: `set_process(false)` / `set_physics_process(false)`, `process_mode = PROCESS_MODE_DISABLED`, `VisibleOnScreenEnabler2D/3D` for off-screen enemies.
- Pool frequent spawns (bullets, hit effects): pre-instantiate, hide and disable processing on return, reset state on reuse — never `queue_free()` + `instantiate()` every shot.
- Groups (`add_to_group("enemies")`, `get_tree().get_nodes_in_group(...)`) for broadcast queries, not for per-frame lookups in hot loops.

## Loading and awaiting

- Big scenes: `ResourceLoader.load_threaded_request(path)` at a loading screen, poll `load_threaded_get_status`, then `load_threaded_get` (game-asset-pipeline).
- `await` on a signal of a node that may be freed hangs or errors; check `is_instance_valid(node)` after awaits that span frames.

## Project hygiene

- Commit `.import` files and, from 4.4, the `.uid` files beside scripts and shaders; ignore `.godot/`.
- Keep the editor version pinned in `config/features`; opening the project in a newer minor rewrites files.

## Common Mistakes

- Untyped variables and functions in new code.
- `get_node("../../UI/HUD")` or `get_parent()` chains for communication.
- Mutating a shared `.tres` at runtime.
- An autoload per feature, holding level state.
- Moving a `RigidBody` by setting its position.

## Red Flags

- `$Path` or `get_node` inside `_process` / `_physics_process`.
- A Resource script with methods that change game state.
- `queue_free()` and `instantiate()` for projectiles fired many times a second.
