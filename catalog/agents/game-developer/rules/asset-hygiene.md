---
name: asset-hygiene
priority: 80
enabled: true
---
Binary assets (textures, audio, models, `.uasset`/`.umap`, large `.fbx`/`.glb`) go through Git LFS when the repository uses it (check `.gitattributes`); engine metadata is committed with its asset and never hand-edited or orphaned — every Unity `.meta` beside its file with its GUID intact, Godot `.import` files committed and the `.godot/` cache ignored, Unreal `.uasset` moved or renamed only through the editor (redirectors fixed up). Ship no uncompressed textures or audio (compression set per target platform), no editor-only or test assets in runtime folders, and no synchronous load on the main thread during gameplay — load asynchronously or at a loading screen (game-asset-pipeline).
