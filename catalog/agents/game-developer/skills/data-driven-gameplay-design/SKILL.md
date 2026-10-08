---
name: data-driven-gameplay-design
category: gameplay
description: Use when adding or changing tuning values, items, enemies, abilities or levels - put them in the engine's data assets (ScriptableObject, Resource, DataAsset/DataTable, JSON/RON) with validation and stable IDs, keep runtime state out of shared data, inject services instead of singletons, and test data invariants.
tech_stack: Game core
source: informed by Donchitos/Claude-Code-Game-Studios and the Unity, Godot, Unreal and Bevy data-asset docs; own wording
---
# Data-Driven Gameplay Design

## Overview

Designers balance games by changing numbers hundreds of times. If a number lives in code, each change is a programmer task, a rebuild and a review. If it lives in a data asset, it is an edit a designer can make and a test can check. The code defines what a sword *is*; the data says what *this* sword does.

**Core principle:** Behaviour in code, tuning in data. Data is typed, validated at load, referenced by a stable ID, and never mutated at runtime.

## The data asset per engine

```csharp
// Unity: ScriptableObject definition
[CreateAssetMenu(menuName = "Game/Weapon")]
public sealed class WeaponDefinition : ScriptableObject {
    [SerializeField] string id;
    [SerializeField, Min(0)] int damage = 10;
    [SerializeField, Min(0.05f)] float cooldownSeconds = 0.5f;
    [SerializeField] AnimationCurve falloff = AnimationCurve.Linear(0, 1, 1, 0);
    public string Id => id; public int Damage => damage; public float CooldownSeconds => cooldownSeconds;
    void OnValidate() { if (string.IsNullOrWhiteSpace(id)) Debug.LogError($"{name}: missing id", this); }
}
```

```gdscript
# Godot: a Resource saved as .tres
class_name WeaponDefinition extends Resource
@export var id: StringName
@export_range(0, 1000) var damage: int = 10
@export_range(0.05, 10.0, 0.05) var cooldown_seconds: float = 0.5
```

```cpp
// Unreal: a primary data asset (or an FTableRowBase row in a DataTable imported from CSV)
UCLASS(BlueprintType)
class UWeaponDefinition : public UPrimaryDataAsset {
    GENERATED_BODY()
public:
    UPROPERTY(EditDefaultsOnly, meta = (ClampMin = "0")) int32 Damage = 10;
    UPROPERTY(EditDefaultsOnly, meta = (ClampMin = "0.05")) float CooldownSeconds = 0.5f;
    virtual FPrimaryAssetId GetPrimaryAssetId() const override { return FPrimaryAssetId("Weapon", GetFName()); }
};
```

```ts
// Web: JSON validated once at load (zod)
export const Weapon = z.object({
  id: z.string().min(1),
  damage: z.number().int().nonnegative(),
  cooldownSeconds: z.number().min(0.05),
});
export const weapons = z.array(Weapon).parse(await (await fetch("data/weapons.json")).json());
```

```rust
// Bevy: a serde struct loaded from RON/JSON via an asset loader (or bevy_common_assets)
#[derive(Asset, TypePath, Deserialize)]
pub struct WeaponDefinition { pub id: String, pub damage: u32, pub cooldown_seconds: f32 }
```

## Rules

- **No tuning literals in gameplay code.** `damage = 25` in a script is a data bug. Physics constants and engine settings belong in project settings or a config asset too.
- **Validate at load or in the editor**, not on first use mid-game: `OnValidate` / `IsDataValid(FDataValidationContext&)` / `@export_range` / a schema parse.
- **Stable IDs, not indices or display names.** Saves, network messages and drop tables reference `"sword_iron"`, never "the third weapon" or "Iron Sword".
- **Data is read-only at runtime.** A ScriptableObject changed in Play mode stays changed in the editor; a shared Godot `.tres` mutated by one enemy changes every enemy. Copy into a runtime state object, or `duplicate()` the Resource (or set `resource_local_to_scene`).
- **No logic in data assets** beyond trivial derived getters — rules live in systems that read the data.
- **Curves and tables for progressions:** `AnimationCurve`, Godot `Curve`, `UCurveFloat`/`UCurveTable`, or a table in JSON. A level-1-to-50 XP formula hard-coded in a `switch` is not tunable.

## Services, not singletons

```csharp
// ❌ hidden global state; tests share it; order of initialisation decides behaviour
GameManager.Instance.Gold += reward;

// ✅ the dependency is explicit and replaceable in a test
public sealed class LootSystem {
    readonly IWallet _wallet; readonly IRandom _rng;
    public LootSystem(IWallet wallet, IRandom rng) { _wallet = wallet; _rng = rng; }
}
```

Each engine has a sanctioned place for services: Unity — a composition root or the repository's DI container (VContainer, Zenject); Godot — an autoload for a *service* (save, audio, scene loading), never for a level's state; Unreal — subsystems (`UGameInstanceSubsystem`, `UWorldSubsystem`); Bevy — `Resource`s inserted by a plugin. Use the one the repository already uses.

## Testing data

Two kinds of tests:

1. **Systems with hand-built data:** a test creates its own small definition (`ScriptableObject.CreateInstance<WeaponDefinition>()`, `WeaponDefinition.new()`, a literal object) so the test does not change when a designer rebalances.
2. **Invariants over all shipped data:** load every asset of the type and assert what must always hold.

```ts
describe("shipped data", () => {
  it("every drop table has positive total weight and only known item ids", () => {
    for (const t of dropTables) {
      expect(t.entries.reduce((s, e) => s + e.weight, 0)).toBeGreaterThan(0);
      for (const e of t.entries) expect(itemIds.has(e.itemId)).toBe(true);
    }
  });
});
```

In Unity this is an EditMode test using `AssetDatabase.FindAssets("t:WeaponDefinition")`; in Godot a test iterating `DirAccess` over the data folder; in Unreal an Automation test using the Asset Registry or the editor's Data Validation.

## Balancing changes

- A balance task changes data, not code — plus the invariant test if the change exposed a new rule.
- Never let a unit test assert a shipped tuning value (`Assert.AreEqual(25, sword.Damage)`) — that is a change detector that breaks on every rebalance.

## Common Mistakes

- Magic numbers in `Update` "just for now".
- Mutating a shared ScriptableObject or `.tres` at runtime.
- Referencing data by array index in save files.
- A static `Instance` holding gameplay state across scenes and tests.

## Red Flags

- A diff that changes a damage or speed number inside a `.cs`, `.gd`, `.cpp`, `.ts` or `.rs` file.
- A data type with no validation.
- Tests that fail when a designer edits a data asset.
