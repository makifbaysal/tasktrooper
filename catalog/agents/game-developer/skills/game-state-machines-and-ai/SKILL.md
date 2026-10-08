---
name: game-state-machines-and-ai
category: gameplay
description: Use when adding character/enemy/game-flow states or NPC behaviour - explicit transition tables instead of flag soup, hierarchical and pushdown machines, behaviour trees per engine, navmesh pathfinding, AI tick budgets, and table-driven tests.
tech_stack: Game core
source: informed by Robert Nystrom's Game Programming Patterns (State chapter) and the Unity, Godot and Unreal AI/navigation docs; own wording
---
# Game State Machines and AI

## Overview

Character controllers, enemies, menus and match flow are all state machines whether or not the code admits it. When they are written as a pile of booleans (`isJumping`, `isAttacking`, `isStunned`), illegal combinations become reachable and every new state touches every branch. Make the states and the legal transitions explicit, and test the table.

**Core principle:** One current state, an explicit list of (state, event) → next state, and enter/exit hooks. Anything not in the table cannot happen.

## Flags vs a table

```ts
// ❌ flag soup: can be jumping AND stunned AND attacking; every rule checks every flag
if (input.attack && !this.isStunned && !this.isDead && (!this.isJumping || this.canAirAttack)) { ... }

// ✅ explicit states and transitions
type State = "idle" | "run" | "jump" | "attack" | "stunned" | "dead";
type Event = "move" | "stop" | "jump" | "land" | "attack" | "attackEnd" | "hit" | "recover" | "die";

const transitions: Record<State, Partial<Record<Event, State>>> = {
  idle:    { move: "run", jump: "jump", attack: "attack", hit: "stunned", die: "dead" },
  run:     { stop: "idle", jump: "jump", attack: "attack", hit: "stunned", die: "dead" },
  jump:    { land: "idle", hit: "stunned", die: "dead" },
  attack:  { attackEnd: "idle", hit: "stunned", die: "dead" },
  stunned: { recover: "idle", die: "dead" },
  dead:    {},
};

export class Fsm {
  constructor(public state: State, private hooks: Hooks) {}
  send(e: Event): boolean {
    const next = transitions[this.state][e];
    if (!next) return false;
    this.hooks.exit?.(this.state); this.state = next; this.hooks.enter?.(next);
    return true;
  }
}
```

The same shape in C# is an `enum` plus a `Dictionary<(State, Event), State>` built once; in GDScript a `Dictionary` of dictionaries or one node per state (godot-gdscript-patterns); in Unreal a `UENUM` with a switch, or StateTree for larger designs.

## Testing the table

```ts
it.each([
  ["idle", "jump", "jump"], ["jump", "attack", "jump"],   // no air attack in this design
  ["stunned", "move", "stunned"], ["dead", "recover", "dead"],
  ["attack", "hit", "stunned"],
])("%s + %s → %s", (from, ev, to) => {
  const fsm = new Fsm(from as State, {});
  fsm.send(ev as Event);
  expect(fsm.state).toBe(to);
});
```

Cover every row that matters to the design plus the rejections ("dead ignores everything"). A new state lands with its rows in this test.

## Bigger machines

- **Hierarchical:** shared transitions (hit → stunned, die → dead) live on a parent "alive" state instead of being copied into every child.
- **Pushdown (a stack):** pause and nested menus — push `paused`, pop back to exactly where play was.
- **Concurrent:** movement and weapon as two machines rather than the product of their states.
- **Timed states:** a state that ends after 0.4 s counts seconds from `enter`, on the simulation clock (game-loop-and-time).

## Behaviour trees and their alternatives

Reach for a behaviour tree when an NPC has many prioritised goals with conditions (patrol, investigate, chase, flee, heal). Use the one the repository already has:

| Engine | Built in / common |
|---|---|
| Unreal | Behavior Trees + Blackboard + EQS; StateTree for state-based logic |
| Unity | Behavior package (`com.unity.behavior`); third-party trees if already present |
| Godot | Addons such as LimboAI or Beehave; a node-based state machine for simple NPCs |
| Bevy | Crates such as `big-brain` (utility AI) or a hand-rolled tree |
| Web | A small hand-rolled tree, or the repository's library |

Rules either way: leaf actions are small and testable alone, the tree reads a blackboard instead of reaching into scene objects, conditions are cheap, and running actions can be aborted cleanly when a higher-priority branch wins. Utility AI (score each option, pick the best) suits needs-driven NPCs; keep its curves in data (data-driven-gameplay-design).

## Navigation

| Engine | Navmesh and agents |
|---|---|
| Unity | AI Navigation package: `NavMeshSurface` (bake), `NavMeshAgent.SetDestination` |
| Godot | `NavigationRegion2D/3D` + `NavigationAgent2D/3D`; query only after the navigation map has synced (wait one physics frame after load) |
| Unreal | `NavMeshBoundsVolume`, `AAIController::MoveToLocation/MoveToActor`, async path queries |
| Web | `recast-navigation` (WASM) or grid A* for 2D |
| Bevy | Crates such as `vleue_navigator` or `oxidized_navigation` |

- Do not re-path every frame: re-path when the target moved more than a threshold or on a timer, and cap path requests per frame (time-slicing).
- Grid A* for tile games: precompute the grid, reuse open/closed buffers, and early-out on unreachable targets.
- Avoidance (RVO) and path following are separate concerns from path finding.

## AI budget

- Perception (line-of-sight, hearing) at 5–10 Hz, staggered across agents so they do not all think on the same frame.
- Far or off-screen agents think less often (level of detail for AI).
- Measure the AI system's frame cost against its slice of the budget (game-performance-budget).
- Seed any randomness in decisions from the simulation's RNG, so a bug report with a seed reproduces.

## Common Mistakes

- Transition logic spread across `Update` branches instead of one table.
- State entry effects (play animation, start timer) duplicated at every place that changes the state — put them in `enter`.
- An animation state machine (Animator, AnimationTree, Anim Blueprint) used as the gameplay authority — it presents the gameplay state, it does not own it.
- Every agent calling the pathfinder every frame.

## Red Flags

- Three or more booleans describing what a character is "doing".
- A new state added with no test rows.
- An `if (state == X)` check outside the state machine that decides a transition.
