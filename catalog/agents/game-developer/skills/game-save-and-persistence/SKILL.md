---
name: game-save-and-persistence
category: gameplay
description: Use when adding or changing save games, settings or progression storage - a versioned save format with a tested migration chain, atomic writes with backup and checksum, per-engine storage locations, safe serializers, and the round-trip/migration/corruption tests.
tech_stack: Game core
source: informed by the Unity, Godot, Unreal and MDN storage docs and the .NET BinaryFormatter security guidance; own wording
---
# Game Save and Persistence

## Overview

A save file outlives the code that wrote it. Players carry saves across updates, crashes happen mid-write, and a lost 40-hour save is the bug players remember. Every save has a version, every older version has a tested migration, and a write never destroys the last good save.

**Core principle:** Versioned, migrated, written atomically, checked on load — and never deserialized with a format that can execute code.

## The format

```json
{
  "version": 3,
  "savedAtUtc": "2026-10-08T12:00:00Z",
  "player": { "levelId": "forest_02", "position": [12.5, 0, -4.0], "hp": 72 },
  "inventory": [{ "itemId": "sword_iron", "count": 1 }],
  "flags": ["met_blacksmith", "boss_forest_defeated"]
}
```

- A plain DTO, separate from engine objects: no `GameObject`, `Node` or `AActor` references, no array indices into data tables — stable IDs only (data-driven-gameplay-design).
- Settings (volume, bindings, accessibility) in a separate file from progress, so resetting one never touches the other.
- JSON is readable and diff-able; a compact binary format (MessagePack, protobuf, Godot's `store_var` without objects) is fine when size matters — the version field stays first.

## Migrations

```ts
type Migration = (save: any) => any;
const migrations: Record<number, Migration> = {
  1: (s) => ({ ...s, version: 2, inventory: s.items.map((id: string) => ({ itemId: id, count: 1 })) }),
  2: (s) => ({ ...s, version: 3, flags: Object.keys(s.questState ?? {}).filter((k) => s.questState[k] === "done") }),
};

export function upgrade(raw: any): SaveV3 {
  if (typeof raw?.version !== "number") throw new SaveError("unversioned");
  if (raw.version > CURRENT) throw new SaveError("from a newer game version");  // refuse, never guess
  let s = raw;
  while (s.version < CURRENT) s = migrations[s.version](s);
  return SaveV3Schema.parse(s);
}
```

- One migration per version step, never edited after release; each tested with a fixture file captured from that version (`tests/fixtures/saves/v1.json`, `v2.json`).
- A save from a newer version is refused with a clear message, not loaded and overwritten.
- Changing a saved field's meaning without bumping the version is the bug this exists to prevent.

## Writing safely

1. Serialize to bytes in memory.
2. Write to `save.json.tmp`, flush to disk.
3. Keep the previous file as `save.json.bak`.
4. Rename the temp file over `save.json` (atomic on the same volume).
5. Store a checksum (CRC32 or SHA-256 of the payload) in a header or a sidecar.

On load: verify the checksum and parse; on failure fall back to `.bak`, tell the player, and never crash or silently start a new game over the old one.

Serialize on the main thread if the engine requires it, but do file I/O off it (or between frames at a checkpoint) — a save must not hitch gameplay. Autosave at checkpoints, not every frame.

## Where saves live

| Engine | Location / API |
|---|---|
| Unity | `Application.persistentDataPath` + `System.IO`; `PlayerPrefs` only for tiny settings |
| Godot | `user://` with `FileAccess`; `ConfigFile` for settings |
| Unreal | `USaveGame` subclass + `UGameplayStatics::AsyncSaveGameToSlot` / `LoadGameFromSlot`; keep a version `UPROPERTY` in it |
| Web | IndexedDB for saves (async, larger; e.g. `idb-keyval`), `localStorage` only for small settings — it is synchronous, ~5 MB and can throw in private mode or when full |
| Bevy / Rust | `serde` to the platform data dir (`directories` crate) |
| pygame / MonoGame | the OS user data directory, never the install folder |

Consoles and some stores impose their own save APIs and size limits — follow the platform layer the repository already has.

## Serializers that execute code — never for save data

- .NET `BinaryFormatter`: obsolete and insecure; do not use it in Unity, MonoGame or Godot C#.
- Godot: `bytes_to_var_with_objects` / `str_to_var` on untrusted data, and loading a user-supplied `.tres`/`.res`, can instantiate scripts — use `JSON`, `ConfigFile`, or `FileAccess.get_var(false)` (objects disallowed).
- Python `pickle` for pygame saves: a shared or downloaded save becomes code execution — use JSON.
- Treat every save file as untrusted input: players edit them, and mods share them.

## Tests

- **Round trip:** state → save → load → equal state.
- **Migration:** each fixture `vN.json` → `upgrade` → matches the expected current-version object.
- **Corruption:** truncated file, bad checksum, garbage bytes → falls back to `.bak` and reports; no exception escapes.
- **Newer version:** refused, original file untouched.
- **Atomicity:** the write path is tested with a fake file system that fails between steps — the previous save is still loadable.

## Common Mistakes

- Serializing engine objects or private implementation classes directly, so every refactor breaks old saves.
- No version field; "we'll add one when we need it".
- Writing in place over the only copy.
- Saving inventory by data-table index; reordering the table corrupts every save.
- Blocking the main thread with file I/O mid-gameplay.

## Red Flags

- A change to a saved type with no version bump and no migration test.
- `BinaryFormatter`, `pickle`, or object-enabled Godot variant decoding in a save path.
- A load path with no fallback when parsing fails.
