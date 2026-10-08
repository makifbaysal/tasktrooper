---
name: fixed-timestep-simulation
priority: 85
enabled: true
---
Physics and authoritative gameplay simulation advance on a fixed timestep — Unity `FixedUpdate`, Godot `_physics_process`, Bevy `FixedUpdate`, an accumulator loop in web and custom engines — while rendering reads interpolated state. Never move a rigidbody by writing its transform in a per-frame callback; drive it through the physics API inside the fixed step. Cap the steps per frame so a long frame cannot spiral, and keep the simulation deterministic where the design needs it (replays, lockstep, rollback): seeded injectable RNG, no dependence on hash-map iteration order, no wall-clock reads inside the step (game-loop-and-time).
