---
name: game-performance-budget
category: performance
description: Use when touching anything that runs per frame, adding entities/effects at scale, or answering a stutter/frame-drop report - frame budgets per target FPS, per-system split, the profiler per engine, before/after numbers, and threshold tests.
tech_stack: Game core
source: informed by Donchitos/Claude-Code-Game-Studios and the Unity, Godot, Unreal, Chrome DevTools and Bevy profiling docs; own wording
---
# Game Performance Budget

## Overview

A game has a hard deadline every frame. Miss it and the player sees a hitch, whatever the average says. Performance work is therefore budgeting and measuring, not guessing: know the budget, measure before, change one thing, measure after, write both numbers down.

**Core principle:** No optimisation without a before and after measurement on the same scene, build type and machine. No hot-path change without proof it did not get slower.

## The budget

| Target | Frame budget | Typical for |
|---|---|---|
| 30 FPS | 33.3 ms | low-end mobile, some console cinematics |
| 60 FPS | **16.67 ms** | default for this agent unless the task or repo says otherwise |
| 90 FPS | 11.1 ms | VR minimum |
| 120 FPS | 8.33 ms | high-refresh displays, competitive games |
| 144 FPS | 6.94 ms | PC competitive |

Split it per system and leave headroom. An example 60 FPS CPU split (replace with the repository's own if it has one):

| System | Budget |
|---|---|
| Gameplay scripts | 3.0 ms |
| Physics | 2.0 ms |
| AI and pathfinding | 2.0 ms |
| Animation | 1.5 ms |
| Rendering (CPU submit) | 4.5 ms |
| UI | 1.0 ms |
| Audio | 0.5 ms |
| Headroom (OS, spikes, GC) | 2.17 ms |

GPU time is a separate 16.67 ms; a frame is bound by whichever of CPU or GPU is slower. Memory gets a per-platform ceiling, and mobile adds thermal throttling: a game that holds 60 for two minutes and then drops is over budget.

## Measure with the engine's tools

| Engine | Frame/CPU | Allocations / memory | Instrument your code |
|---|---|---|---|
| Unity | Profiler (CPU, Rendering, Physics modules), Profile Analyzer for before/after | Profiler GC Alloc column, Memory Profiler | `ProfilerMarker` / `using (marker.Auto())`; Performance Testing package `Measure.Method` |
| Godot | Debugger → Profiler, Visual Profiler, Monitors | Monitors: object/resource/node counts, static memory | `Performance.get_monitor(...)`, `Time.get_ticks_usec()` around a block |
| Unreal | `stat unit`, `stat game`, `stat gpu`, Unreal Insights (`-trace=cpu,frame,gpu`) | `memreport -full`, LLM (`-llm`) | `TRACE_CPUPROFILER_EVENT_SCOPE(Name)`, `SCOPE_CYCLE_COUNTER` |
| Web | Chrome Performance panel (Bottom-up), `renderer.info` (three.js), Babylon Inspector | Memory panel heap snapshots before/after a scene change | `performance.mark` / `performance.measure` |
| Bevy | `FrameTimeDiagnosticsPlugin` + `LogDiagnosticsPlugin`, Tracy via the `trace_tracy` feature | Tracy memory | `info_span!("name").entered()` |

Profile a development build on the target class of device where you can; an editor profile carries editor overhead and a desktop GPU hides mobile limits. Headless Chromium renders WebGL in software — never judge frame time from it.

## The before/after record

Put this in the closing message whenever a hot path changed:

```
Scene: Arena_Stress (200 enemies), dev build, M2 MacBook, 60 FPS target
                   before     after
frame median       14.1 ms    11.8 ms
frame p99          22.7 ms    13.9 ms
GC alloc / frame   3.2 KB     0 B
AI.Update          4.6 ms     2.1 ms   (path requests time-sliced, 8 per frame)
```

Median hides hitches — always report a high percentile (p95/p99) or the worst frame too. If you could not profile on this machine, say so and give the test-level number you did get.

## Threshold tests

A performance test fails when the budget is exceeded, otherwise it is a log line nobody reads.

```csharp
[Test]
public void StepAll_200Agents_StaysUnderTwoMs() {
    var grid = TestGrids.Arena64();
    var agents = TestAgents.Spawn(200, seed: 7);
    for (int i = 0; i < 5; i++) Pathfinder.StepAll(grid, agents, maxRequests: 8);
    var samples = new double[31];
    var sw = new System.Diagnostics.Stopwatch();
    for (int i = 0; i < samples.Length; i++) {
        sw.Restart(); Pathfinder.StepAll(grid, agents, maxRequests: 8); sw.Stop();
        samples[i] = sw.Elapsed.TotalMilliseconds;
    }
    Array.Sort(samples);
    Assert.That(samples[samples.Length / 2], Is.LessThan(2.0), "AI budget is 2 ms at 60 FPS");
}
```

The same shape works in every runner: warm up, take N samples, assert on the median (and a looser bound on the max). Where the repository uses a benchmark harness — Unity's Performance Testing package (`Measure.Method`), `criterion` in Rust, Vitest `bench` — record with it and keep the assertion. Keep thresholds generous enough for CI noise and tight enough to catch a 2× regression. Allocation tests are exact: zero means zero (unity-performance-and-gc).

## Where frames usually go

- **Allocations and GC** in per-frame code (rule no-alloc-in-hot-paths).
- **Draw calls / batches:** too many materials, no atlas, no instancing, UI canvases rebuilt every frame.
- **Overdraw:** stacked transparent particles and full-screen effects, worst on mobile.
- **Physics:** mesh colliders on moving bodies, everything on one layer colliding with everything, continuous collision on objects that don't need it.
- **AI:** pathfinding or perception for every agent every frame — time-slice it and run perception at 5–10 Hz.
- **Lookups:** `GetComponent`, `Find`, `get_node`, `TActorIterator` per frame.
- **Hitches:** shader compilation on first use (warm up / PSO caching), synchronous asset loads, a GC spike, a save on the main thread.
- **Idle work:** off-screen or sleeping objects still ticking — Godot `set_process(false)` / `VisibleOnScreenEnabler2D`, Unity disabling components or `CullingGroup`, Unreal `PrimaryActorTick.bCanEverTick = false` and tick intervals, Bevy run conditions.

## Procedure

1. Name the budget and the scene that exercises the change.
2. Measure before (median + p99 + allocations) and save the numbers.
3. Change one thing.
4. Measure after on the same scene, build type and machine.
5. Keep the change only if it moved the number; add or update the threshold test.

## Common Mistakes

- Optimising what looks slow instead of what the profiler shows.
- Reporting an average FPS instead of frame-time percentiles.
- Profiling in the editor and generalising to a mobile build.
- Micro-optimising a 0.05 ms function while a 6 ms one sits above it.

## Red Flags

- "Should be faster now" with no numbers.
- A performance test with no assertion.
- A hot-path diff with no before/after record in the closing message.
