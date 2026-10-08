---
name: unreal-gameplay-ability-system
category: gameplay
description: Use when working with Unreal's Gameplay Ability System - when GAS is worth it, where the AbilitySystemComponent lives and how it is initialised on server and client, AttributeSets with correct clamping and replication, GameplayEffects and execution calculations as data, abilities with commit/end, gameplay tags, prediction, and testing.
tech_stack: Unreal Engine
source: informed by the Unreal Engine Gameplay Ability System documentation and the community GAS documentation (tranek/GASDocumentation); own wording
---
# Unreal Gameplay Ability System

## Overview

GAS is Unreal's framework for abilities, attributes, buffs, cooldowns and costs, with network prediction built in. It pays off in games with many abilities and stacking effects, especially multiplayer ones; it is heavy machinery for a platformer with one jump. Most GAS bugs are setup bugs: the component initialised on one side only, clamping in the wrong callback, abilities that never end.

**Core principle:** Abilities are code, numbers are GameplayEffects (data), state is gameplay tags, and every attribute change goes through an effect so it replicates and predicts correctly.

## When to use it

- Use when: many abilities with cooldowns/costs, buffs and debuffs that stack and expire, attributes modified by many sources, multiplayer with predicted abilities. If the project already uses GAS, new abilities go through it.
- Skip when: a handful of actions with simple cooldowns in a single-player game — a component with a few functions is clearer.

## Setup

| Owner of the ASC | Use for |
|---|---|
| `APlayerState` | Player characters — attributes and effects survive pawn death and respawn |
| The `ACharacter` itself | AI and simple pawns |

Replication mode: `Full` (single-player), `Mixed` (player-controlled, multiplayer), `Minimal` (AI in multiplayer).

```cpp
// Server: when the controller possesses the pawn
void AHeroCharacter::PossessedBy(AController* NewController) {
    Super::PossessedBy(NewController);
    InitAbilitySystem();
}
// Client: when the PlayerState replicates
void AHeroCharacter::OnRep_PlayerState() {
    Super::OnRep_PlayerState();
    InitAbilitySystem();
}
void AHeroCharacter::InitAbilitySystem() {
    auto* PS = GetPlayerState<AHeroPlayerState>();
    if (!PS) return;
    AbilitySystem = PS->GetAbilitySystemComponent();
    AbilitySystem->InitAbilityActorInfo(PS, this);       // owner = PlayerState, avatar = this pawn
    if (HasAuthority()) GrantStartupAbilitiesAndEffects();
}
```

Missing the client-side `InitAbilityActorInfo` is the classic "works on the server, abilities do nothing on the client" bug. Implement `IAbilitySystemInterface` on the actors that own or carry the ASC.

## AttributeSets

```cpp
UCLASS()
class UHealthSet : public UAttributeSet {
    GENERATED_BODY()
public:
    UPROPERTY(BlueprintReadOnly, ReplicatedUsing = OnRep_Health) FGameplayAttributeData Health;
    ATTRIBUTE_ACCESSORS(UHealthSet, Health)
    UPROPERTY(BlueprintReadOnly, ReplicatedUsing = OnRep_MaxHealth) FGameplayAttributeData MaxHealth;
    ATTRIBUTE_ACCESSORS(UHealthSet, MaxHealth)
    UPROPERTY(BlueprintReadOnly) FGameplayAttributeData IncomingDamage;   // meta attribute, server only
    ATTRIBUTE_ACCESSORS(UHealthSet, IncomingDamage)

    virtual void PreAttributeChange(const FGameplayAttribute& Attribute, float& NewValue) override;
    virtual void PostGameplayEffectExecute(const FGameplayEffectModCallbackData& Data) override;
    virtual void GetLifetimeReplicatedProps(TArray<FLifetimeProperty>& Out) const override;
    UFUNCTION() void OnRep_Health(const FGameplayAttributeData& Old) { GAMEPLAYATTRIBUTE_REPNOTIFY(UHealthSet, Health, Old); }
    UFUNCTION() void OnRep_MaxHealth(const FGameplayAttributeData& Old) { GAMEPLAYATTRIBUTE_REPNOTIFY(UHealthSet, MaxHealth, Old); }
};

void UHealthSet::PostGameplayEffectExecute(const FGameplayEffectModCallbackData& Data) {
    if (Data.EvaluatedData.Attribute == GetIncomingDamageAttribute()) {
        const float Damage = GetIncomingDamage();
        SetIncomingDamage(0.f);
        SetHealth(FMath::Clamp(GetHealth() - Damage, 0.f, GetMaxHealth()));   // the real clamp
    }
}
```

- `ATTRIBUTE_ACCESSORS` is the project's helper macro wrapping the `GAMEPLAYATTRIBUTE_*` getters/setters/initter — use the one the project defines.
- `PreAttributeChange` only clamps the value being queried (current value from modifiers); it does not permanently change the base value. Clamp for real in `PostGameplayEffectExecute`.
- Damage goes through a **meta attribute** (`IncomingDamage`) so armour, shields and death are handled in one place.
- Replicate with `DOREPLIFETIME_CONDITION_NOTIFY(UHealthSet, Health, COND_None, REPNOTIFY_Always)`.

## GameplayEffects: numbers as data

- Instant (damage, heal), Duration (a 5 s slow), Infinite (an equipped item's bonus, removed explicitly).
- Magnitudes come from scalable floats with curve tables, `SetByCaller` (tagged values set by the ability), Modifier Magnitude Calculations, or an Execution Calculation for formulas that read several attributes (damage vs armour).
- Cooldowns and costs are GameplayEffects referenced by the ability, applied by `CommitAbility`.
- Keep the formula in a pure static function called by the execution calculation, so it can be unit-tested without a world.

## Abilities

```cpp
void UGA_Fireball::ActivateAbility(const FGameplayAbilitySpecHandle Handle, const FGameplayAbilityActorInfo* Info,
                                   const FGameplayAbilityActivationInfo Activation, const FGameplayEventData* Trigger) {
    if (!CommitAbility(Handle, Info, Activation)) {                 // pays cost and starts cooldown, or fails
        EndAbility(Handle, Info, Activation, /*bReplicate*/ true, /*bWasCancelled*/ true);
        return;
    }
    auto* Task = UAbilityTask_PlayMontageAndWait::CreatePlayMontageAndWaitProxy(this, NAME_None, CastMontage);
    Task->OnCompleted.AddDynamic(this, &UGA_Fireball::OnCastFinished);
    Task->OnCancelled.AddDynamic(this, &UGA_Fireball::OnCastCancelled);
    Task->ReadyForActivation();
}
```

- Every activation path ends in `EndAbility` — including failure and cancellation — or the ability stays active forever and blocks itself.
- Instancing policy `InstancedPerActor` unless there is a reason otherwise; net execution `LocalPredicted` for player abilities.
- Spawning projectiles and applying damage happen on the server (`HasAuthority(&ActivationInfo)`); the client predicts cosmetics.

## Gameplay tags

- State as hierarchical tags: `State.Dead`, `State.Stunned`, `Ability.Skill.Fireball`, `Cooldown.Skill.Fireball`.
- Abilities declare `ActivationBlockedTags` (`State.Stunned`), `CancelAbilitiesWithTag`, `ActivationOwnedTags`.
- Define tags natively (`UE_DEFINE_GAMEPLAY_TAG`) or in `DefaultGameplayTags.ini` — the way the project already does; no tag strings typed ad hoc in code.
- GameplayCues are cosmetic only (sound, particles); gameplay never depends on a cue having played.

## Testing

- Pure formulas (damage, scaling curves) as static functions in Automation Specs.
- Attribute clamping and effect stacking: a spec that creates a transient actor with an ASC and the AttributeSet in a test world, applies a `FGameplayEffectSpec`, and asserts attribute values (unreal-automation-testing).
- Ability flows (activate → commit → end, blocked by tag): a functional test map with a test pawn.
- Multiplayer: Play-In-Editor with two clients and simulated lag for manual checks — report if not run here.

## Common Mistakes

- `InitAbilityActorInfo` called on the server only.
- Clamping only in `PreAttributeChange`.
- Abilities that never call `EndAbility` on some path.
- Setting attributes directly from gameplay code instead of applying an effect.
- Granting abilities on the client.

## Red Flags

- A cooldown implemented with a timer inside an ability instead of a cooldown GameplayEffect.
- Gameplay logic in a GameplayCue.
- `Full` replication mode on hundreds of AI.
