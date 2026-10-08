---
name: prototype-quarantine
priority: 75
enabled: true
---
Throwaway prototype or spike code lives in its own clearly named directory (`Prototypes/`, `prototype/`, a `proto_` scene) and is never referenced from production scenes, assemblies, autoloads or build settings. Promoting a prototype means rewriting it test-first into the production layout, not moving the file; a task asking for a feature never ships the prototype that explored it.
