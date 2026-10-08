---
name: unreal-automation-testing
category: testing
description: Use when writing or running Unreal Engine tests - which test type fits (simple/complex automation tests, Automation Specs, functional tests, CQTest, Low-Level Tests, Gauntlet), naming and filters, latent tests, and the headless UnrealEditor-Cmd run with report export, exit handling and screenshot captures.
tech_stack: Unreal Engine
source: informed by the Unreal Engine 5 Automation System, Automation Spec, Functional Testing and Gauntlet documentation; own wording
---
# Unreal Automation Testing

## Overview

Unreal's Automation System runs C++ tests inside the editor or a game process, from the Session Frontend or the command line. The fast, stable tests are plain C++ checks on rules and data; tests that need a world, actors and frames are slower and belong in functional test maps. Either way the run must be headless and its report read, because the editor's exit code alone is not a reliable verdict.

**Core principle:** Rules tested as plain C++ in Automation Specs; world behaviour in functional tests; every run headless with a report you read.

## Which test type

| Question | Type |
|---|---|
| Does the damage formula / inventory rule compute correctly? | Automation Spec or simple automation test |
| Same check over many inputs | Complex automation test (`GetTests` + parameters) or a spec loop |
| Does the door open when the player overlaps the trigger? | Functional test (`AFunctionalTest` in a test map) |
| Readable fixtures and assertions in a newer codebase | CQTest (`TEST_CLASS`), if the project enabled the plugin |
| Engine-free module code, fast native tests | Low-Level Tests (Catch2, `-Test` targets) |
| Client/server sessions, performance captures on devices | Gauntlet (via RunUAT) |

Follow what the project already has. Without a convention, test names are dotted filters `<Project>.<System>.<Scenario>` and classes `F<System><Scenario>Test` / `F<System>Spec`.

## Automation Spec

```cpp
#include "Misc/AutomationTest.h"
#include "Combat/DamageMath.h"

BEGIN_DEFINE_SPEC(FDamageMathSpec, "Hero.Combat.DamageMath",
    EAutomationTestFlags::EditorContext | EAutomationTestFlags::ProductFilter)
    FDamageInput Input;
END_DEFINE_SPEC(FDamageMathSpec)

void FDamageMathSpec::Define() {
    BeforeEach([this]() { Input = FDamageInput{ /*Base*/ 100.f, /*Armor*/ 0.f, /*bCritical*/ false }; });

    Describe("Apply", [this]() {
        It("returns the base damage with no armor", [this]() {
            TestEqual(TEXT("damage"), FDamageMath::Apply(Input), 100.f);
        });
        It("halves damage at 50% armor", [this]() {
            Input.Armor = 0.5f;
            TestNearlyEqual(TEXT("damage"), FDamageMath::Apply(Input), 50.f, 0.001f);
        });
        It("never goes below zero", [this]() {
            Input.Armor = 2.f;
            TestTrue(TEXT("non-negative"), FDamageMath::Apply(Input) >= 0.f);
        });
    });
}
```

Simple form for a single check:

```cpp
IMPLEMENT_SIMPLE_AUTOMATION_TEST(FInventoryStackOverflowTest, "Hero.Inventory.AddBeyondStack_OverflowsToNextSlot",
    EAutomationTestFlags::EditorContext | EAutomationTestFlags::ProductFilter)
bool FInventoryStackOverflowTest::RunTest(const FString& Parameters) {
    FInventoryModel Inventory(/*Slots*/ 4);
    Inventory.Add(TEXT("potion"), 7, /*MaxStack*/ 5);
    TestEqual(TEXT("slot 0"), Inventory.Slot(0).Count, 5);
    TestEqual(TEXT("slot 1"), Inventory.Slot(1).Count, 2);
    return true;
}
```

- Keep the flags the project already uses; newer engine versions renamed the context *mask* constants, so copy from an existing test rather than from memory.
- Test plain C++ types (`FInventoryModel`, `FDamageMath`) — the reason to keep rules out of actors (testable-game-logic).
- `AddExpectedError(TEXT("..."))` when a test deliberately triggers a logged error; an unexpected error log fails the test.

## Latent tests

When something takes frames (async load, a timer, a physics settle), use `LatentIt` with a done delegate, or latent commands in simple tests:

```cpp
LatentIt("loads the boss definition asynchronously", EAsyncExecution::TaskGraphMainThread, [this](const FDoneDelegate& Done) {
    UAssetManager::GetStreamableManager().RequestAsyncLoad(BossPath, [this, Done]() {
        TestNotNull(TEXT("loaded"), BossPath.ResolveObject());
        Done.Execute();
    });
});
```

Every latent path calls `Done` — including failure — or the run hangs until the timeout.

## Functional tests

- A test map (`/Game/Tests/Maps/FTEST_Doors.umap`) holding `AFunctionalTest` actors (or a C++ subclass) next to the setup they test.
- Override `StartTest()`, drive the scenario, and finish with `FinishTest(EFunctionalTestResult::Succeeded, TEXT("door opened"))` or `Failed`; set a time limit on the actor.
- They appear in the automation tree under the project's functional tests group with the map path; filter by that prefix.

## Running headless

```sh
UE="/Users/Shared/Epic Games/UE_5.6"            # from EngineAssociation; Windows: C:\Program Files\Epic Games\UE_5.6
mkdir -p /tmp/tt-<task key>/ue-report
"$UE/Engine/Build/BatchFiles/Mac/Build.sh" <Project>Editor Mac Development -Project="$PWD/<Project>.uproject" -WaitMutex \
  > /tmp/tt-<task key>/build.log 2>&1 || { echo BUILD FAILED; tail -50 /tmp/tt-<task key>/build.log; }
"$UE/Engine/Binaries/Mac/UnrealEditor-Cmd" "$PWD/<Project>.uproject" \
  -ExecCmds="Automation RunTests Hero.;Quit" -TestExit="Automation Test Queue Empty" \
  -unattended -nullrhi -nosplash -nosound -log -ReportExportPath="/tmp/tt-<task key>/ue-report" \
  > /tmp/tt-<task key>/ue-tests.log 2>&1; echo "exit=$?"
```

- Build the editor target first; with `-unattended` an out-of-date module does not prompt, it fails.
- The filter after `RunTests` is a prefix (`Hero.` runs everything under it); `Automation RunAll` runs every test including engine ones — avoid it.
- Read `/tmp/tt-<task key>/ue-report/index.json` (succeeded/failed counts and per-test errors) and grep the log for `Test Completed. Result={Fail}` — do not trust the exit code alone.
- `-nullrhi` disables rendering. Screenshot or rendering tests need a real RHI: replace it with `-RenderOffscreen` (game-visual-self-review).
- First runs on a project compile shaders and build the derived data cache; give the command a long timeout or run it detached to the log.

## Gauntlet

For multi-process scenarios (dedicated server + clients, soak and performance runs) the project defines Gauntlet test nodes and runs them through RunUAT, e.g. `RunUAT.sh RunUnreal -project=<Project> -build=<path> -test=<TestNode>`. Use what the repository's CI already runs; do not invent a Gauntlet setup inside a feature task.

## No engine on this machine

Nothing Unreal compiles without the engine. Write the spec next to the existing tests, keep it small and obviously correct, and report: `NOT RUN: UE 5.6 not installed (EngineAssociation 5.6) — FDamageMathSpec written in Source/Hero/Tests/DamageMathSpec.cpp`.

## Common Mistakes

- Testing a rule by spawning an actor in a map when a plain C++ test would do.
- Running tests without building the editor target first.
- Reading the exit code, not the report.
- Latent tests that miss `Done` on a failure path.
- `RunAll` in a project with thousands of engine tests.

## Red Flags

- A new gameplay rule with only a functional test.
- Screenshot tests run with `-nullrhi`.
- A closing message claiming Unreal tests passed with no report path or result counts.
