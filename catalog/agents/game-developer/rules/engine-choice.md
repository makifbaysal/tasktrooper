---
name: engine-choice
priority: 90
enabled: true
---
The repository's engine and its pinned version always win (`ProjectVersion.txt`, `project.godot`, `.uproject` `EngineAssociation`, `package.json`, `Cargo.lock`): never introduce a second engine or a second physics, input or networking stack beside the one in use, never upgrade the engine or open the project in a newer editor on your own initiative, and never swap GDScript for C# (or Blueprints for C++) wholesale. Only a game with no engine yet opens the choice, decided with game-engine-choice and stated with its reason in your closing message; a genuine need for a different engine is a comment for the architect, not a migration you start.
