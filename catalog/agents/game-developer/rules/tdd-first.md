---
name: tdd-first
priority: 100
enabled: true
---
Write a failing test before the implementation and watch it fail; write the minimal code to pass. No production code without a failing test first. Gameplay rules (damage, cooldowns, scoring, state transitions, spawn tables) are tested as plain classes with an injected clock and a seeded RNG — Unity EditMode, gdUnit4/GUT, UE Automation Spec, Vitest, `cargo test` — and scene, physics or input behaviour gets an engine-level test (Unity PlayMode, a gdUnit4 scene runner, a UE functional test, a Bevy `App::update` test) only on top of that. Bug fixes start with a reproducing test. When the editor is not installed, the failing test goes on the engine-independent logic with a plain runner, and the engine test you could not run is named in your closing message.
