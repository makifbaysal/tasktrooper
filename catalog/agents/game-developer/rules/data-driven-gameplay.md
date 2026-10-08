---
name: data-driven-gameplay
priority: 80
enabled: true
---
Tuning values — speeds, damage, health, cooldowns, spawn rates, drop tables, costs, difficulty curves — live in the engine's data assets (ScriptableObject, `.tres` Resource, DataAsset/DataTable, JSON/RON/TOML loaded at startup), never as literals in gameplay code. Code reads them through a typed definition with validation, tests build their own small definitions, and runtime state never writes back into a shared data asset (duplicate it per instance). Gameplay state is not a static singleton: inject the services a system needs (data-driven-gameplay-design).
