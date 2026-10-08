---
name: unity-performance-and-gc
category: performance
description: Use when touching Unity per-frame code, spawning, physics queries, UI updates or rendering setup - profiling with numbers, zero-GC hot paths (caching, NonAlloc queries, no LINQ/closures/boxing, TMP SetText), ObjectPool, allocation tests, Burst/Jobs for bulk math, and rendering/physics settings that cost frames.
tech_stack: Unity
source: informed by the Unity 6 Manual (Profiler, memory and GC, Jobs, Burst, ObjectPool, physics queries) and Unity's optimisation guides; own wording
---
# Unity Performance and GC

## Overview

Unity's managed heap is garbage-collected; every allocation in `Update` is a future GC spike, and the incremental collector only spreads the pain. Most frame drops in Unity projects come from a short list: allocations per frame, per-frame lookups, unpooled spawning, physics queries that allocate, UI rebuilds, and draw calls. Find which one with the Profiler, fix it, and prove the fix with numbers (game-performance-budget).

**Core principle:** Zero managed allocations per frame in gameplay, measured — not assumed — with the Profiler's GC Alloc column or an allocation test.

## Profile first

- Profiler: CPU Usage (Hierarchy, sort by Time ms and GC Alloc), Rendering, Physics, Memory modules. Profile a Development Build with Autoconnect; the Editor adds overhead.
- Deep Profile only to locate a call, never for timing.
- Profile Analyzer compares a before and an after capture; Memory Profiler finds leaks across scene loads.
- Mark your own code: `static readonly ProfilerMarker s_Marker = new("Enemy.Think"); using (s_Marker.Auto()) { ... }`.

## The allocation list

```csharp
// ❌ allocates or searches every frame
void Update() {
    var enemies = FindObjectsByType<Enemy>(FindObjectsSortMode.None);       // array + scene search
    var close = enemies.Where(e => (e.transform.position - transform.position).sqrMagnitude < r2).ToList(); // LINQ + closure
    hpText.text = "HP: " + hp;                                              // new string every frame
    if (other.tag == "Enemy") { }                                           // allocates the tag string
    var hits = Physics.OverlapSphere(transform.position, 5f);              // new array per call
    StartCoroutine(Flash(new WaitForSeconds(0.1f)));                        // allocation per call
}

// ✅ cached, reused, non-allocating
readonly Collider[] _hits = new Collider[32];
static readonly WaitForSeconds FlashDelay = new(0.1f);
int _shownHp = -1;

void Update() {
    int count = Physics.OverlapSphereNonAlloc(transform.position, 5f, _hits, enemyMask);
    for (int i = 0; i < count; i++) { /* use _hits[i] */ }
    if (hp != _shownHp) { _shownHp = hp; hpText.SetText("HP: {0}", hp); }   // TextMeshPro, no string alloc
    if (other.CompareTag("Enemy")) { }
}
```

More of the same:
- 3D queries: `RaycastNonAlloc`, `SphereCastNonAlloc`, `OverlapBoxNonAlloc`. 2D: the overloads taking a `ContactFilter2D` and a reused `List<T>`/array (the 2D `*NonAlloc` methods are obsolete).
- `GetComponentsInChildren(list)` overloads that fill a reused list.
- `Animator.StringToHash`, `Shader.PropertyToID`, cached `WaitForSeconds`/`WaitForFixedUpdate`.
- `foreach` over `List<T>` and arrays is fine; over an `IEnumerable<T>` interface it boxes the enumerator.
- No capturing lambdas, `string.Format`/interpolation, `params` arrays or boxing (struct → `object`/interface) in hot paths.
- `Debug.Log` in a hot path allocates and stalls — strip it from builds (`[Conditional("UNITY_EDITOR")]` wrapper) or remove it.
- `Camera.main` is cheaper than it was but is still a lookup — cache it.

## Pool what you spawn

```csharp
using UnityEngine.Pool;

public sealed class BulletSpawner : MonoBehaviour {
    [SerializeField] Bullet prefab;
    ObjectPool<Bullet> _pool;

    void Awake() => _pool = new ObjectPool<Bullet>(
        createFunc: () => { var b = Instantiate(prefab); b.Init(release: x => _pool.Release(x)); return b; },
        actionOnGet: b => b.gameObject.SetActive(true),
        actionOnRelease: b => b.gameObject.SetActive(false),
        actionOnDestroy: b => Destroy(b.gameObject),
        collectionCheck: false, defaultCapacity: 64, maxSize: 256);

    public void Fire(Vector3 pos, Vector3 dir) { var b = _pool.Get(); b.Launch(pos, dir); }
}
```

Pre-warm at load (get and release `defaultCapacity` instances) so the first fight does not instantiate. A pooled object resets all its state in `actionOnGet`/`Launch`, not in `Start`.

## Prove zero allocations

```csharp
using Is = UnityEngine.TestTools.Constraints.Is;

[Test] public void Tick_SteadyState_DoesNotAllocate() {
    var sim = TestSim.WithEnemies(100);
    sim.Tick(0.016f);                                    // warm-up: first call may JIT/initialise
    Assert.That(() => sim.Tick(0.016f), Is.Not.AllocatingGCMemory());
}
```

## Burst and Jobs for bulk work

For the same math over hundreds or thousands of items (flocking, projectiles, terrain), a Burst-compiled job on native containers:

```csharp
[BurstCompile]
struct MoveJob : IJobParallelFor {
    public NativeArray<float3> Positions;
    [ReadOnly] public NativeArray<float3> Velocities;
    public float Dt;
    public void Execute(int i) => Positions[i] += Velocities[i] * Dt;
}
// schedule: var h = new MoveJob { ... }.Schedule(count, 64); later h.Complete();
```

- Native containers are disposed (`Allocator.Persistent` in `OnDestroy`; `TempJob` within four frames; `Temp` within the frame).
- Complete handles before reading results; schedule early, complete late.
- At entity scale, consider DOTS (unity-dots-ecs) — only if the project already uses it or the task decides it.

## Rendering and physics settings that cost frames

- URP/HDRP: keep the SRP Batcher compatible (shaders with CBUFFERs; avoid per-renderer `MaterialPropertyBlock` where it breaks batching); GPU instancing for repeated meshes; static batching for static scenery; Sprite Atlases for 2D.
- UI: split static and frequently changing elements into separate Canvases; turn off Raycast Target on non-interactive graphics; never animate layout groups every frame.
- Overdraw: transparent particles and full-screen effects, especially on mobile; LODs and occlusion culling for 3D.
- Physics: the layer collision matrix trims pair checks; primitive colliders over mesh colliders on moving bodies; `Physics.autoSyncTransforms` off; tune `Time.fixedDeltaTime` deliberately.
- Mobile: set `Application.targetFrameRate` explicitly; watch thermal throttling over minutes, not seconds.

## Common Mistakes

- Optimising from intuition without a Profiler capture.
- `Instantiate`/`Destroy` for bullets, hit effects and damage numbers.
- Updating TextMeshPro text every frame with string concatenation.
- Allocation test without a warm-up call, failing on first-call initialisation.
- Leaking `NativeArray`s (the leak detection warning in the console is a real bug).

## Red Flags

- `GC.Alloc` above zero in a gameplay `Update` in the Profiler.
- LINQ, `FindObjectsByType`, `GetComponent` or `new` inside `Update`/`FixedUpdate`.
- A performance claim in the closing message without before/after numbers.
