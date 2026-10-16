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
