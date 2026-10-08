---
name: unreal-cpp-architecture
category: architecture
description: Use when writing Unreal Engine 5 C++ - the gameplay framework classes and where logic belongs, UObject/AActor/UActorComponent lifecycle, UPROPERTY/UFUNCTION and GC-safe pointers (TObjectPtr, TWeakObjectPtr), subsystems, the C++/Blueprint boundary, Enhanced Input, tick discipline, and compiling from the command line.
tech_stack: Unreal Engine
source: informed by the Unreal Engine 5 documentation (gameplay framework, object handling, garbage collection, subsystems, Enhanced Input); own wording
---
# Unreal C++ Architecture

## Overview

Unreal gives you a full gameplay framework, a reflection system and a garbage collector. Most bugs in Unreal C++ come from fighting them: raw `UObject*` members the GC frees underneath you, gameplay logic in constructors that only ever run on the class default object, logic on the wrong framework class (GameMode code running on clients), and everything ticking every frame.

**Core principle:** Put each piece of logic on the framework class that owns it, keep every UObject reference visible to the GC, and let C++ own systems while Blueprints own content and tuning.

## Where logic belongs

| Class | Lives on | Owns |
|---|---|---|
| `AGameModeBase` | Server only | Rules of the match: spawning, scoring rules, win conditions |
| `AGameStateBase` | Server + replicated to all | Match state everyone sees (time left, team scores) |
| `APlayerController` | Owning client + server | Input routing, UI ownership, camera for that player |
| `APlayerState` | Replicated to all | Per-player persistent data (name, score); survives pawn death |
| `APawn` / `ACharacter` | Replicated | The body: movement, the components that act in the world |
| `UActorComponent` | With its actor | Reusable behaviour (health, inventory, interaction) |
| `UGameInstanceSubsystem` / `UWorldSubsystem` / `ULocalPlayerSubsystem` | Process / world / local player | Services (save, matchmaking, spawning registries) — instead of singletons |

A client asking `GetWorld()->GetAuthGameMode()` gets `nullptr` — that is the design, not a bug to work around.

## Lifecycle

```cpp
AHeroCharacter::AHeroCharacter() {
    // Constructor: defaults and subobjects only. It runs for the class default object too,
    // so no world access, no spawning, no gameplay.
    PrimaryActorTick.bCanEverTick = false;
    Health = CreateDefaultSubobject<UHealthComponent>(TEXT("Health"));
}

void AHeroCharacter::BeginPlay() {
    Super::BeginPlay();                                   // always call Super
    Health->OnDied.AddDynamic(this, &AHeroCharacter::HandleDied);
}

void AHeroCharacter::EndPlay(const EEndPlayReason::Type Reason) {
    GetWorldTimerManager().ClearAllTimersForObject(this);
    Super::EndPlay(Reason);
}
```

Order to remember: constructor → `PostInitializeComponents` → `BeginPlay` → `Tick` → `EndPlay`. Components: `InitializeComponent` (if `bWantsInitializeComponent`) → `BeginPlay`.

## Reflection and the garbage collector

```cpp
UCLASS()
class UInventoryComponent : public UActorComponent {
    GENERATED_BODY()
public:
    UFUNCTION(BlueprintCallable, Category = "Inventory")
    bool TryAdd(const UItemDefinition* Item, int32 Count);

    DECLARE_DYNAMIC_MULTICAST_DELEGATE_OneParam(FOnInventoryChanged, const UInventoryComponent*, Inventory);
    UPROPERTY(BlueprintAssignable) FOnInventoryChanged OnChanged;

private:
    // ❌ UItemDefinition* Cached;                       invisible to the GC: freed under you, then a crash
    // ✅ visible to the GC, editor-tunable, null-safe
    UPROPERTY(EditDefaultsOnly, Category = "Inventory", meta = (ClampMin = "1"))
    int32 SlotCount = 20;

    UPROPERTY(Transient) TArray<FInventorySlot> Slots;
    UPROPERTY() TObjectPtr<const UItemDefinition> LastAdded;
    TWeakObjectPtr<AActor> LastInteractor;             // non-owning: check IsValid() before use
};
```

- Every `UObject*` member is a `UPROPERTY` (`TObjectPtr<T>` for members in UE5), a `TWeakObjectPtr`, or a `TStrongObjectPtr` (from non-UObject code). Raw pointers only as locals and parameters.
- Create objects with `NewObject<T>(Outer)`, `CreateDefaultSubobject` (constructor only) or `GetWorld()->SpawnActor` — never `new`.
- `IsValid(Ptr)` before using anything that may have been destroyed (`IsValid` also catches "pending kill").
- `EditDefaultsOnly` for tuning on the Blueprint class, `EditAnywhere` only when level instances should differ, `VisibleAnywhere` for components.

## The C++ / Blueprint boundary

- C++: systems, rules, replication, anything performance-sensitive or tested.
- Blueprints: content (which mesh, which sound, which montage), tuning values, level scripting, and composition of C++ components.
- Expose a small surface: `BlueprintCallable` for actions, `BlueprintPure` for queries, `BlueprintImplementableEvent` / `BlueprintNativeEvent` for designer hooks. No gameplay rule that exists only in a Blueprint graph nobody can test.
- Blueprints reference heavy assets through soft references (`TSoftObjectPtr`) so loading one class does not pull the whole content graph (game-asset-pipeline).

## Enhanced Input

```cpp
void AHeroController::BeginPlay() {
    Super::BeginPlay();
    if (auto* Subsystem = ULocalPlayer::GetSubsystem<UEnhancedInputLocalPlayerSubsystem>(GetLocalPlayer())) {
        Subsystem->AddMappingContext(DefaultContext, /*Priority*/ 0);
    }
}

void AHeroCharacter::SetupPlayerInputComponent(UInputComponent* PlayerInputComponent) {
    auto* Input = CastChecked<UEnhancedInputComponent>(PlayerInputComponent);
    Input->BindAction(MoveAction, ETriggerEvent::Triggered, this, &AHeroCharacter::Move);
    Input->BindAction(JumpAction, ETriggerEvent::Started, this, &ACharacter::Jump);
}

void AHeroCharacter::Move(const FInputActionValue& Value) {
    const FVector2D Axis = Value.Get<FVector2D>();
    AddMovementInput(GetActorForwardVector(), Axis.Y);
    AddMovementInput(GetActorRightVector(), Axis.X);
}
```

Input actions and mapping contexts are assets (data); the legacy `BindAxis`/`BindAction` string API is not used in new code.

## Tick discipline

- `PrimaryActorTick.bCanEverTick = false` by default; enable only where per-frame work is needed, with `SetActorTickInterval` where 60 Hz is not needed.
- Timers (`GetWorldTimerManager().SetTimer`) and events instead of polling in `Tick`.
- No `TActorIterator`, `GetAllActorsOfClass` or `FindComponentByClass` per frame — cache, or register with a subsystem.

## Compile and project hygiene

```sh
# macOS (Win64: Engine/Build/BatchFiles/Build.bat ... Win64; Linux: Engine/Build/BatchFiles/Linux/Build.sh ... Linux)
"<UE>/Engine/Build/BatchFiles/Mac/Build.sh" <Project>Editor Mac Development \
  -Project="$PWD/<Project>.uproject" -WaitMutex > /tmp/tt-<task key>/build.log 2>&1; echo "exit=$?"
```

- Compile the editor target before running automation tests; with `-unattended`, an out-of-date module fails instead of prompting.
- New module dependencies go in `<Module>.Build.cs` (`PublicDependencyModuleNames` / `PrivateDependencyModuleNames`), e.g. `EnhancedInput`, `GameplayAbilities`.
- Never commit `Binaries/`, `Intermediate/`, `Saved/`, `DerivedDataCache/`; `.uasset`/`.umap` go through LFS (asset-hygiene).
- Log with a category: `DEFINE_LOG_CATEGORY_STATIC(LogInventory, Log, All); UE_LOG(LogInventory, Warning, TEXT("..."));`. `check()` for programmer errors that must crash in development; `ensure()` for recoverable ones.

## Common Mistakes

- Gameplay or world access in a constructor.
- Raw `UObject*` members without `UPROPERTY`.
- Forgetting `Super::` calls in overridden lifecycle functions.
- GameMode logic expected to run on clients.
- Ticking actors that only need to react to events.

## Red Flags

- `new UMyObject` or `new AMyActor` anywhere.
- `GetAllActorsOfClass` inside `Tick`.
- A rule implemented only in a Blueprint graph with no C++ counterpart or test.
- `Binaries/` or `Intermediate/` in the diff.
