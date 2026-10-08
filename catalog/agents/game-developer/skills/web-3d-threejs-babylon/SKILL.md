---
name: web-3d-threejs-babylon
category: engine
description: Use when building a 3D web game with three.js, Babylon.js or PlayCanvas - render loop and delta per library, fixed-step physics with Rapier or Havok, explicit dispose of GPU resources, instancing and draw-call budgets, glTF with Draco/meshopt/KTX2, pixel ratio and resize, WebGPU opt-in, React Three Fiber notes, and headless testing.
tech_stack: Web 3D
source: informed by the three.js, Babylon.js, PlayCanvas and Rapier documentation; own wording
---
# Web 3D: three.js, Babylon.js, PlayCanvas

## Overview

three.js is a rendering library with a huge ecosystem; Babylon.js is a full engine (physics plugin, GUI, inspector, a `NullEngine` for headless tests); PlayCanvas is an engine built around its hosted editor, also usable engine-only. All three hand you the GPU directly — and with it the job of freeing what you allocate, keeping draw calls low, and staying inside a mobile browser's memory.

**Core principle:** Simulate on a fixed step, render every frame, dispose every GPU resource you create, and count draw calls.

## The loop and delta

```ts
// three.js — Timer replaces the deprecated Clock (r183+); setAnimationLoop also drives WebXR
const timer = new THREE.Timer();
timer.connect(document);                         // ignores the huge delta after a hidden tab
renderer.setAnimationLoop((t) => {
  timer.update(t);
  const dt = Math.min(timer.getDelta(), 0.25);   // seconds
  stepFixed(dt);                                 // accumulator: physics + rules at 1/60 (game-loop-and-time)
  renderer.render(scene, camera);
});
```

```ts
// Babylon.js — getDeltaTime() is milliseconds
scene.onBeforeRenderObservable.add(() => stepFixed(Math.min(engine.getDeltaTime() / 1000, 0.25)));
engine.runRenderLoop(() => scene.render());
window.addEventListener("resize", () => engine.resize());
```

```ts
// PlayCanvas (engine API / ESM scripts) — dt in seconds
export class Spin extends Script {
  static scriptName = "spin";
  speed = 90;
  update(dt: number) { this.entity.rotate(0, this.speed * dt, 0); }
}
```

## Physics on a fixed step

- **Rapier** (WASM, three.js or Babylon): `await RAPIER.init()`, `world.timestep = 1 / 60`, call `world.step()` from the accumulator, copy body transforms to meshes with interpolation. The deterministic build exists for lockstep/rollback.
- **Babylon**: Havok via the physics V2 plugin (`new HavokPlugin(true, await HavokPhysics())`); keep its fixed timestep consistent across devices.
- **PlayCanvas**: its rigidbody components (ammo.js) step with the app.
- Never move a dynamic body by writing `mesh.position`; set velocities/impulses or use kinematic bodies.

## Dispose what you create

```ts
// ❌ removing from the scene does not free GPU memory
scene.remove(level);

// ✅ free geometry, materials and textures explicitly (unless shared with something still alive)
level.traverse((o) => {
  if (o instanceof THREE.Mesh) {
    o.geometry.dispose();
    for (const m of Array.isArray(o.material) ? o.material : [o.material]) {
      for (const v of Object.values(m)) if (v instanceof THREE.Texture) v.dispose();
      m.dispose();
    }
  }
});
scene.remove(level);
console.assert(renderer.info.memory.geometries <= baseline.geometries);
```

- three.js: also dispose render targets, `PMREMGenerator` outputs, and the renderer itself on teardown. `renderer.info.memory` before and after a level change is the leak test.
- Babylon: `mesh.dispose(false, true)` frees materials and textures too; load levels into an `AssetContainer` and `dispose()` it on exit; `scene.dispose()` / `engine.dispose()` on teardown.
- PlayCanvas: `entity.destroy()`, and `asset.unload()` for assets no longer used.

## Draw calls and instancing

- Many copies of one mesh: `THREE.InstancedMesh` (`setMatrixAt` + `instanceMatrix.needsUpdate = true`, `count` for the live number) or Babylon thin instances (`mesh.thinInstanceAdd(matrix)`).
- Merge static scenery (`BufferGeometryUtils.mergeGeometries`, Babylon `Mesh.MergeMeshes`) and share materials.
- Budget draw calls per frame — a few hundred for mobile browsers, more on desktop — and read the actual number (`renderer.info.render.calls`, Babylon Inspector's statistics) before and after.
- Shadows are expensive: one shadow-casting light, tight shadow camera bounds, `castShadow` only where it shows; bake lighting for static scenes.
- Cap the pixel ratio: `renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))`; resize from the canvas size, not the window, with a `ResizeObserver`.

## Assets

- glTF/GLB with Draco or meshopt geometry compression and KTX2 textures: three.js `GLTFLoader` + `DRACOLoader` / `MeshoptDecoder` + `KTX2Loader` (decoder files served from the app's own path); Babylon's glTF loader with its Draco/KTX2 support; `LoadAssetContainerAsync` to keep a level's assets disposable together.
- Optimise models offline (`npx @gltf-transform/cli optimize`) and keep originals out of the shipped bundle (game-asset-pipeline).

## WebGPU

three.js `WebGPURenderer` (from `three/webgpu`, falling back to WebGL2) and Babylon `WebGPUEngine` are usable; adopt them only when the repository or task opts in — shader code, post-processing and some materials differ between the paths.

## React Three Fiber (if the repo uses it)

`useFrame((state, delta) => ...)` mutates refs; never `setState` per frame. Dispose is automatic for objects R3F created declaratively, not for ones you created imperatively. `<Instances>` / `<instancedMesh>` for repetition.

## Testing

- Rules and simulation: plain TS with Vitest, no renderer import.
- Babylon: `new NullEngine()` + `new Scene(engine)` runs scene logic headless in Vitest (no pixels).
- three.js: scene-graph logic can be tested without a renderer; rendering is checked with the seeded Playwright smoke run and screenshots (game-visual-self-review). Headless Chromium uses a software GL — correct images, meaningless frame rates.

## Common Mistakes

- `scene.remove` without `dispose` — memory grows every level.
- Allocating `new THREE.Vector3()` in the frame loop instead of reusing module-level temporaries.
- Full device pixel ratio on 3× phones.
- One mesh per bullet or tree instead of instancing.
- `THREE.Clock` in new code on a revision that deprecated it.

## Red Flags

- No `dispose()` call anywhere in a project that loads more than one level.
- `renderer.info.render.calls` in the thousands on a mobile target.
- Physics stepped with the raw render delta.
