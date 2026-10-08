---
name: no-alloc-in-hot-paths
priority: 85
enabled: true
---
Nothing that runs every frame or every physics step allocates: no `new` of reference types, LINQ, string concatenation or interpolation, capturing lambdas, boxing, `foreach` over interface enumerators, `GetComponent`/`Find`/`get_node`/`FindObjectOfType` lookups, or fresh arrays and objects in `Update`/`FixedUpdate`/`_process`/`_physics_process`/`Tick`/`requestAnimationFrame` callbacks or Bevy systems. Cache references at startup, pre-allocate and reuse buffers, use the engine's non-allocating queries, and pool anything spawned more than a few times a second. Prove a hot-path change with the profiler's allocation column or an allocation test (game-performance-budget), not by reading the code.
