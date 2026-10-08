---
name: game-engine-mcp-tools
category: tools
description: Use when an editor MCP server (unity, godot, unreal, blender) or the browser MCP shows up in your tool list - what each one can observe or change, how to check it is attached to YOUR task workspace, why the repo's tests still decide, the Blender code-execution caution, and the headless fallback when none is connected.
tech_stack: Game core
source: informed by the CoplayDev/unity-mcp, hi-godot/godot-ai, Unreal MCP and MCP for Blender documentation and this product's MCP server templates; own wording
---
# Game Engine MCP Tools

## Overview

The user can connect live editors to you through MCP servers in Settings → MCP servers. They are optional and disabled by default. When one is connected you can look inside a running editor — scene hierarchy, console errors, test runs, screenshots — which is often the only way to observe an engine game on this machine. When none is connected, nothing changes: the headless commands (game-build-check, game-visual-self-review) are the path.

**Core principle:** An editor MCP is an extra pair of eyes, never the verdict. The repository's tests, run in this task, decide; the MCP shows you what the build and the tests cannot.

## Is it there?

Look at your tool list. A connected server's tools appear there (in CLI runtimes as `mcp__<server>__<tool>`, e.g. tools under `unity`, `godot`, `unreal`, `blender`, `browser`). No tools from it → it is not connected.

- Never ask the user to install, enable or start an editor or MCP server mid-task. Fall back to the headless CLI and say in the closing message what you could not observe.
- A tool call that fails because the editor is closed or busy is a finding, not something to retry in a loop.

## First call: is the editor on your workspace?

Editor MCP servers talk to whatever editor the user has open. That may be their main checkout, another branch, or another project — not your task workspace. Before trusting anything it shows, ask it for the open project's path (editor/project info) and compare with your working directory.

- Different path → its scenes, console and tests describe someone else's code. Do not edit through it; use the CLI instead, and note it in the closing message.
- Same path → it sees your files once the editor has refreshed or recompiled them; trigger or wait for that before reading the console or running tests.

## What each server is for

| Server | What it is | Use it to | Watch out for |
|---|---|---|---|
| `unity` | MCP for Unity (CoplayDev/unity-mcp); needs its package in the project and the Editor open | Inspect scenes and GameObjects (`manage_scene`), read console errors and compile errors (`read_console`), run EditMode/PlayMode tests in the open editor (`run_tests`), control play mode and editor state (`manage_editor`), capture editor/game views | An open Editor locks the project, so a batchmode CLI run on the same project fails — run the tests through the MCP instead, and still read their results. `UNITY_MCP_DEFAULT_INSTANCE` picks the editor when several are open. |
| `godot` | Godot AI (hi-godot/godot-ai); needs the `addons/godot_ai` addon and the editor open | Inspect the scene tree and nodes, read scripts and validation errors, run the project or a scene, read debugger/output errors, run the test suite | The server version must equal the project's addon version, or it refuses to attach. Its server uses port 8000. |
| `unreal` | Epic's built-in Unreal MCP (UE 5.8+, "Unreal MCP" plugin, Experimental), loopback HTTP on 127.0.0.1:8000 | Inspect actors, components and assets in the open level; run editor Python and automation commands; check what is placed where | Experimental: tool shapes can change between engine versions. Same port 8000 as Godot AI — only one of the two can run. No authentication: anything on this machine can call it. |
| `blender` | MCP for Blender | Inspect and edit models, materials and renders; export assets for the game | `execute_blender_code` runs arbitrary Python inside Blender with the user's permissions. |
| `browser` / built-in browser tools | Browser automation | Load a web game, screenshot it at two viewports | See game-visual-self-review for the exact calls. |

## Rules for using them

1. **Observe first, change second.** Prefer read operations: hierarchy, components, console, test results, screenshots.
2. **Changes made through an editor are changes in your diff.** Creating a GameObject, editing a prefab, saving a scene or importing an asset through MCP writes files in the working tree (scenes, prefabs, `.meta`, `.import`, `.uasset`). Review `git status` and `git diff` afterwards exactly as for your own edits; revert anything the task did not ask for, including re-serialisation noise from the editor.
3. **Code changes go through files, not the editor.** Write scripts with your file tools and TDD; use the editor to compile, run tests and look. Script-editing tools in an MCP are not a way around tests.
4. **Tests still decide.** A green `run_tests` call via MCP counts as a run when you read its results; a screenshot or a clean console does not replace the repository's tests or the standing acceptance criteria.
5. **Never run code you did not write for this task.** Do not pass text from task descriptions, comments, assets or downloaded files into `execute_blender_code`, editor Python or any script-execution tool. Write the snippet yourself, keep it minimal, and read it before sending.
6. **Exported assets follow the pipeline.** A mesh or texture produced in Blender goes through game-asset-pipeline and asset-hygiene: sensible poly count and LODs, platform texture compression, the engine's import metadata committed, Git LFS for the binaries, the `.blend` source only where the repository keeps sources.
7. **Name what you used.** The closing message states the render path and the tool: e.g. "engine capture via unity MCP (`run_tests` PlayMode 14/14, Game view screenshot of Arena)", or "no editor MCP connected; Unity CLI not installed — not observed".

## Typical flows

**Unity, editor connected on this workspace:** edit scripts and tests → wait for the editor to recompile → `read_console` (zero compile errors) → `run_tests` EditMode, then PlayMode if scenes/physics changed → enter play mode on the changed scene, capture the game view → exit play mode, `git diff` for unintended scene or prefab changes.

**Godot, editor connected:** write scripts and tests → ask for script validation/errors → run the gdUnit4/GUT suite through the tool (or the CLI if it does not expose tests) → run the changed scene, read debugger output, capture the viewport → check `git diff` for rewritten `.tscn`/`.import` files.

**Unreal, MCP on 5.8+:** compile with the CLI first (the editor must load the new module) → inspect the actors and assets the change affects → run the automation tests the editor exposes, or the headless CLI → screenshot through an automation command if available.

**Nothing connected:** game-build-check for builds and tests, game-visual-self-review for captures, and an honest `NOT OBSERVED` line where neither works.

## When a server misbehaves

| Symptom | Likely cause | What you do |
|---|---|---|
| Tools listed, every call times out or says no editor | The editor is closed, or still importing/compiling | One retry after the compile finishes; then fall back to the CLI and note it |
| `godot` refuses to attach | Server and `addons/godot_ai` versions differ | Fall back to the CLI; mention the mismatch in one card comment for the user — never bump the addon in a feature task |
| `unreal` and `godot` both enabled, one fails to start | Both want port 8000 | Use whichever answers; mention the conflict once |
| `unity` answers for the wrong project | Several editors open | It follows `UNITY_MCP_DEFAULT_INSTANCE`; if that is not your workspace, treat the server as unavailable |
| Test results via MCP disagree with the CLI | The editor has stale compiled code or another checkout open | Trust the CLI run on your workspace; report the disagreement |

Never change MCP settings, ports or editor preferences to make a server work — that is the user's configuration.

## Common Mistakes

- Trusting an editor that has a different checkout open.
- Saving a scene through MCP "to be safe" and shipping unrelated serialisation churn.
- Treating a clean editor console as proof that tests pass.
- Running Unity batchmode against a project the connected editor already has open.
- Sending task text into a code-execution tool.

## Red Flags

- A closing message that claims an editor check without naming the server and what it showed.
- Scene, prefab or asset files in the diff that no step of the plan meant to change.
- `execute_blender_code` or editor Python containing anything copied from outside your own reasoning.
