package notify_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
)

func TestCooldownAllowsOneNotificationPerKeyPerWindow(t *testing.T) {
	c := notify.NewCooldown(30 * time.Minute)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	assert.True(t, c.Allow("a", at))
	assert.False(t, c.Allow("a", at.Add(29*time.Minute)))
	assert.True(t, c.Allow("b", at.Add(time.Minute)), "keys are cooled apart")
	assert.True(t, c.Allow("a", at.Add(30*time.Minute)))
}

func TestCooldownDefaultsItsWindow(t *testing.T) {
	c := notify.NewCooldown(0)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	assert.True(t, c.Allow("a", at))
	assert.False(t, c.Allow("a", at.Add(notify.DefaultCooldown-time.Second)))
	assert.True(t, c.Allow("a", at.Add(notify.DefaultCooldown)))
}

func TestCooldownTransitionsHoldBackOnlyARepeatOfTheLastKindTold(t *testing.T) {
	c := notify.NewCooldown(30 * time.Minute)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	assert.True(t, c.AllowTransition("rule", "firing", at))
	assert.False(t, c.AllowTransition("rule", "firing", at.Add(time.Minute)), "a repeat inside the window")
	assert.True(t, c.AllowTransition("rule", "resolved", at.Add(2*time.Minute)))
	assert.True(t, c.AllowTransition("rule", "firing", at.Add(3*time.Minute)), "a re-fire after a told recovery")
	assert.True(t, c.AllowTransition("other", "firing", at.Add(3*time.Minute)), "keys are cooled apart")
	assert.False(t, c.AllowTransition("rule", "firing", at.Add(32*time.Minute)), "measured from the last one told")
	assert.True(t, c.AllowTransition("rule", "firing", at.Add(33*time.Minute)))
}

func TestAFlappingKeyIsHeldUntilItSettlesThenToldWhereItEnded(t *testing.T) {
	c := notify.NewCooldown(30*time.Minute).WithFlapLimit(4, 15*time.Minute)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	kinds := []string{"firing", "resolved", "firing", "resolved"}
	for i, kind := range kinds {
		assert.True(t, c.AllowTransitionHolding("rule", kind, at.Add(time.Duration(i)*time.Minute), nil), kind)
	}
	assert.False(t, c.AllowTransitionHolding("rule", "firing", at.Add(4*time.Minute), "fifth"), "the fifth transition in the window is held")
	assert.False(t, c.AllowTransitionHolding("rule", "resolved", at.Add(5*time.Minute), "sixth"))
	assert.False(t, c.AllowTransitionHolding("rule", "firing", at.Add(6*time.Minute), "seventh"))
	assert.True(t, c.AllowTransitionHolding("other", "firing", at.Add(6*time.Minute), nil), "keys flap apart")

	assert.Empty(t, c.Settled("", at.Add(20*time.Minute)), "fourteen minutes in one state is not settled")
	got := c.Settled("", at.Add(21*time.Minute))
	assert.Equal(t, []notify.Held{{Key: "rule", Kind: "firing", Payload: "seventh"}}, got, "the final state is told once it settles")
	assert.Empty(t, c.Settled("", at.Add(22*time.Minute)), "and only once")
}

func TestAFlappingKeyThatSettlesWhereItWasLastToldTellsNothing(t *testing.T) {
	c := notify.NewCooldown(30*time.Minute).WithFlapLimit(2, 15*time.Minute)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	assert.True(t, c.AllowTransition("rule", "firing", at))
	assert.True(t, c.AllowTransition("rule", "resolved", at.Add(time.Minute)))
	assert.False(t, c.AllowTransitionHolding("rule", "firing", at.Add(2*time.Minute), nil))
	assert.False(t, c.AllowTransitionHolding("rule", "resolved", at.Add(3*time.Minute), nil))
	assert.Empty(t, c.Settled("", at.Add(time.Hour)), "it ended resolved, which is what was last told")
	assert.True(t, c.AllowTransition("rule", "firing", at.Add(time.Hour+time.Minute)), "after settling it is told again")
}

func TestSettledHandsBackOnlyTheKeysOfItsPrefix(t *testing.T) {
	c := notify.NewCooldown(30*time.Minute).WithFlapLimit(1, time.Minute)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, key := range []string{"a\x00rule", "b\x00rule"} {
		assert.True(t, c.AllowTransition(key, "firing", at))
		assert.False(t, c.AllowTransitionHolding(key, "resolved", at.Add(time.Second), key))
	}
	got := c.Settled("a\x00", at.Add(time.Hour))
	assert.Equal(t, []notify.Held{{Key: "a\x00rule", Kind: "resolved", Payload: "a\x00rule"}}, got)
	assert.Len(t, c.Settled("b\x00", at.Add(time.Hour)), 1)
}

func TestWithoutAFlapLimitEveryAlternationIsTold(t *testing.T) {
	c := notify.NewCooldown(30 * time.Minute)
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for i := range 10 {
		kind := "firing"
		if i%2 == 1 {
			kind = "resolved"
		}
		assert.True(t, c.AllowTransition("rule", kind, at.Add(time.Duration(i)*time.Minute)))
	}
}
