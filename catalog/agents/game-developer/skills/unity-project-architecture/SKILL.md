---
name: unity-project-architecture
category: architecture
description: Use when writing or restructuring Unity C# code - assembly definitions and dependency direction, small components with cached references, ScriptableObjects for data and events, DI instead of singletons, Input System, Addressables, serialization safety and Unity 6 API changes.
tech_stack: Unity
source: informed by the Unity 6 Manual (assembly definitions, ScriptableObject, Input System, Addressables, serialization); own wording
---
# Unity Project Architecture

## Overview

Unity makes it easy to put everything in one MonoBehaviour that finds its collaborators with `FindObjectOfType` and a static `Instance`. That works for a jam and collapses after a few months: nothing is testable, compile times grow, and scene order decides behaviour. Structure the project as assemblies with a dependency direction, plain C# for rules, thin components for engine glue, and data in assets.

**Core principle:** Rules in plain C# assemblies, components as adapters, data in ScriptableObjects, dependencies passed in. The repository's existing layout and DI choice win.

## Assemblies and direction

```
Game.Core      (noEngineReferences: rules, math, state machines, save DTOs)
   ↑
Game.Runtime   (MonoBehaviours, ScriptableObjects, adapters; references Core)
   ↑
Game.UI        (views and presenters; references Runtime/Core, never the reverse)
Game.Editor    (includePlatforms: Editor; tools, validators)
Tests.EditMode / Tests.PlayMode (test assemblies; see unity-testing)
```

```json
{
  "name": "Game.Core",
  "rootNamespace": "Game.Core",
  "references": [],
  "noEngineReferences": true,
  "autoReferenced": false
}
```

- An assembly per layer keeps compile times down and makes a wrong-way reference a compile error instead of a review comment.
- `Game.Core` with `noEngineReferences: true` can be compiled and tested by plain `dotnet test` when the editor is missing (testable-game-logic).
- Gameplay never references UI; UI observes gameplay through events.

## Components

```csharp
// ❌ lookups every frame, public mutable fields, rules inside the component
public class Enemy : MonoBehaviour {
    public int hp = 100;
    void Update() {
        var player = GameObject.Find("Player");
        if (Vector3.Distance(transform.position, player.transform.position) < 5) GetComponent<Animator>().SetTrigger("Attack");
    }
}

// ✅ cached references, serialized private fields, rule delegated to plain C#
[RequireComponent(typeof(Animator))]
public sealed class EnemyView : MonoBehaviour {
    static readonly int AttackHash = Animator.StringToHash("Attack");
    [SerializeField] EnemyDefinition definition;
    Animator _animator; EnemyBrain _brain; ITargetProvider _targets;

    public void Construct(ITargetProvider targets) => _targets = targets;
    void Awake() { _animator = GetComponent<Animator>(); _brain = new EnemyBrain(definition.AttackRange); }
    void Update() {
        if (_brain.ShouldAttack(transform.position, _targets.PlayerPosition)) _animator.SetTrigger(AttackHash);
    }
}
```

- `[SerializeField] private` over public fields; public API through properties and methods.
- `Awake` initialises the object itself; `OnEnable`/`Start` wire up to others; `OnDisable` unsubscribes everything `OnEnable` subscribed.
- `[RequireComponent]` for hard dependencies on the same GameObject; `TryGetComponent` instead of `GetComponent` + null check.
- Physics in `FixedUpdate` through the Rigidbody API (`MovePosition`, `AddForce`, `linearVelocity`), never `transform.position` on a dynamic body.

## Data and events in ScriptableObjects

- Definitions (weapons, enemies, levels) are ScriptableObjects (data-driven-gameplay-design); runtime state is a separate object built from them. Changes made to a ScriptableObject in Play mode persist in the editor — never write gameplay state into one.
- Event channels (`GameEvent : ScriptableObject` with `Raise()` and listeners) decouple scenes, but keep them few and named for what happened (`PlayerDied`), not what should happen.

## Dependencies, not singletons

- Use the repository's container if it has one (VContainer, Zenject/Extenject): register services in a `LifetimeScope`/installer and inject via constructor (plain classes) or `[Inject]` method (components).
- Without one: a bootstrap scene or composition root creates services and passes them in (`Construct(...)` as above).
- No new `static Instance` for gameplay state. Engine-wide stateless helpers are fine as static classes.

## Input, assets, async

- **Input System**, not the legacy `Input` class — actions from the `.inputactions` asset, rebinding and device glyphs (game-input-and-accessibility). A project still on the legacy Input Manager stays on it unless the task migrates it.
- **Addressables** for content loaded at runtime (game-asset-pipeline); no new content in `Resources/`. Every `LoadAssetAsync` handle is released.
- **Async:** `Awaitable` (Unity 6) or UniTask where the repo uses it; cancel with `destroyCancellationToken` so a destroyed object's continuation does not run. `async void` only for event handlers, with try/catch.

## Serialization safety

- Renaming a serialized field loses its data in every scene and prefab unless you add `[FormerlySerializedAs("oldName")]`.
- Polymorphic serialized fields need `[SerializeReference]`; plain `[SerializeField]` slices to the base type.
- Keep Asset Serialization on Force Text so scenes and prefabs diff and merge (UnityYAMLMerge); prefer prefabs and nested prefabs over large scene edits so changes stay reviewable.

## Unity 6 API notes (follow the pinned version)

- `Rigidbody.velocity` → `linearVelocity`; `drag`/`angularDrag` → `linearDamping`/`angularDamping`.
- `FindObjectsOfType`/`FindObjectOfType` → `FindObjectsByType(FindObjectsSortMode.None)` / `FindAnyObjectByType` (and avoid both in gameplay paths).
- `Object.InstantiateAsync` for large prefabs; `Awaitable.NextFrameAsync`, `Awaitable.WaitForSecondsAsync`.
- Unity 6 LTS lines: 6000.0 LTS support ends in October 2026, 6000.3 LTS is supported to the end of 2027 — never upgrade the project on your own.

## Common Mistakes

- `GameObject.Find`, `FindAnyObjectByType` or `GetComponent` in `Update`.
- A MonoBehaviour holding the rule logic, making it testable only in PlayMode.
- `static Instance` managers that depend on scene load order.
- Renaming serialized fields without `FormerlySerializedAs`.
- Subscribing in `OnEnable` without unsubscribing in `OnDisable` — leaks and calls on destroyed objects.

## Red Flags

- A UI assembly referenced by gameplay code.
- A new `Resources.Load` call.
- `transform.position =` on an object with a non-kinematic Rigidbody.
- A component longer than a few hundred lines with rules, input and presentation mixed.
