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
