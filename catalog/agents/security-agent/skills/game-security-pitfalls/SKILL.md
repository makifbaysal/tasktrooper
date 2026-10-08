---
name: game-security-pitfalls
category: security
description: Use when reviewing game code in C#, GDScript or C++ (Unity, Godot, Unreal, custom engines) - client vs server authority, RPC and packet validation, in-app purchases and economy, save and replay files, mod and script loading, secrets in client builds
tech_stack: Game code
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording) and Glenn Fiedler's networking articles (gafferongames.com, cited); Unity, Godot and Unreal networking and serialization documentation cited, not reproduced
---
# Game Security Pitfalls

## Overview

Everything in the client — memory, files, packets — belongs to the player. That makes most single-player cheating a non-finding (attacker and victim are the same person) and makes everything that affects **other players or real money** a real one: multiplayer outcomes, leaderboards, trading, purchases, shared content.

**Core principle:** clients send intent ("move north", "fire toward x"), the server decides outcomes ("hit for 37"). Any message that carries an outcome is a finding when other players depend on it.

## 1. Server Authority (CWE-602)

```csharp
// ❌ Unity Netcode for GameObjects: any client can call it, and it reports the result
[ServerRpc(RequireOwnership = false)]
void ApplyDamageServerRpc(ulong targetId, int damage) { players[targetId].Health -= damage; }

// ✅ the client sends intent; the server checks the sender and computes the outcome
[Rpc(SendTo.Server)]
void FireRpc(Vector3 direction, RpcParams rpcParams = default) {
    var shooter = PlayerFor(rpcParams.Receive.SenderClientId);
    if (!shooter.CanFire()) return;
    weapons.ResolveShot(shooter, direction.normalized);
}
```

```gdscript
# ❌ Godot: any peer may call it and claim any score
@rpc("any_peer")
func submit_score(player_id: int, score: int) -> void:
    scores[player_id] = score

# ✅ identity from the transport, outcome from server state
@rpc("any_peer", "reliable")
func request_finish_level() -> void:
    var id := multiplayer.get_remote_sender_id()
    scores[id] = compute_score(id)
```

```cpp
// ❌ Unreal: the validation function accepts everything
bool AMyCharacter::ServerSetHealth_Validate(float NewHealth) { return true; }
// ✅ no client-settable health; server RPCs carry inputs and validate ranges
bool AMyCharacter::ServerUseItem_Validate(int32 Slot) { return Slot >= 0 && Slot < Inventory.Num(); }
```

Also: Mirror `[Command(requiresAuthority = false)]` acting on objects the caller does not own; a client-authoritative transform with no server-side speed or teleport check in a competitive mode.

## 2. Network Message Validation

Every field in a packet is attacker-chosen.

- **Sizes and indices:** a length or count read from the packet used to allocate, copy or index. In C/C++ this is memory corruption (CWE-120, CWE-787) — in scope, since these are not memory-safe languages.
- **Ranges:** item slots, entity ids, enum values, positions; `NaN` and `Infinity` floats passing `<`/`>` checks.
- **Ownership:** the entity id in the message belongs to the sender.
- **Versioning:** an unknown message type or version is rejected, not interpreted with the newest layout.

```cpp
// ❌ count comes from the wire
uint16_t count = reader.ReadU16();
for (int i = 0; i < count; ++i) items[i] = reader.ReadU32();   // items holds 32
// ✅
uint16_t count = reader.ReadU16();
if (count > std::size(items) || reader.Remaining() < count * 4) return Disconnect(peer);
```

## 3. Economy and Purchases

- Currency, items or entitlements granted from a client message, or from an in-app purchase receipt validated only on the device → free items for everyone with a proxy. Receipts are verified server-side with the store, once, and recorded to stop replay.
- Trades and crafting resolved client-side; duplicate-item races on the server where the same item can be spent twice (this is integrity, not a theoretical race — report it when two requests can both pass the check).

## 4. Hidden Information

Sending state the player should not see — other players' hands, fog-of-war positions, upcoming loot — to every client lets any modified client reveal it. In competitive modes it is a finding (MEDIUM to HIGH); filter what the server replicates per player.

## 5. Save Files, Replays and Shared Content (CWE-502)

Tampering with a local single-player save is not a finding. Deserializing a file that **someone else** can supply is:

- C# `BinaryFormatter` (also `NetDataContractSerializer`, `LosFormatter`) on save files, replays or downloaded content → code execution; use `System.Text.Json`, MessagePack with known types, or a custom schema.
- Godot: `FileAccess.get_var(true)`, `bytes_to_var_with_objects`, and loading `.tres`/`.res`/`.tscn` from untrusted sources can instantiate objects with embedded scripts; use `get_var()` with objects disallowed, or JSON.
- Saves uploaded to cloud slots, leaderboards or PvP snapshots are server input — validate them like any request.

## 6. Mods, Scripts and User-Generated Content

- Loading assemblies or native libraries from a mods directory (`Assembly.LoadFrom`, `dlopen`), Lua with `os`/`io`/`load` available, GDScript or other engine scripts from downloaded content: full code execution on the player's machine. A finding when the game fetches or auto-installs that content from untrusted sources, or advertises it as sandboxed when it is not.
- A **server** that loads player-uploaded scripts, maps or replays without sandboxing → CRITICAL.
- Downloaded content paths (zip-slip in mod archives — path-traversal-and-file-handling).

## 7. Secrets in Client Builds

Back-end admin or title secret keys (live-ops platforms, publisher web API keys, analytics write secrets with admin scope, dedicated-server credentials) compiled into the client are leaked to every player. Public client ids are fine.

## Not Findings

Single-player cheats, memory editing of the player's own game, speed-ups in offline modes, missing anti-cheat or obfuscation, a client that can see data it legitimately needs to render.

## Common Mistakes

- Reporting a single-player save edit as a vulnerability.
- Reporting C# or GDScript memory safety — memory-safe; C++ packet parsing is the exception.
- Accepting a `_Validate` function or a `RequireOwnership` flag as proof the server checks the outcome.

## Red Flags

- RPCs or messages named `Set…`, `Add…`, `Grant…`, `SubmitScore…` callable by clients.
- `RequireOwnership = false`, `requiresAuthority = false`, `@rpc("any_peer")` without a sender check.
- `_Validate` returning `true` unconditionally.
- `BinaryFormatter`, `get_var(true)`, `Assembly.LoadFrom` on files other people supply.
- Purchase grants with no server-side receipt verification.

## References (names and links only)

[Unity Netcode for GameObjects](https://docs-multiplayer.unity3d.com/) · [Godot high-level multiplayer](https://docs.godotengine.org/en/stable/tutorials/networking/high_level_multiplayer.html) · [Unreal RPCs](https://dev.epicgames.com/documentation/en-us/unreal-engine/remote-procedure-calls-in-unreal-engine) · [gafferongames networking](https://gafferongames.com/) · [BinaryFormatter security guide](https://learn.microsoft.com/en-us/dotnet/standard/serialization/binaryformatter-security-guide) · CWE-120, 502, 602, 787
