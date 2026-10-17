package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TestEveryDeclaredCapabilityIsImplemented: a capability is what the
// console offers a source for, so one the kind cannot read is a dead end.
func TestEveryDeclaredCapabilityIsImplemented(t *testing.T) {
	implements := map[domain.Capability]func(port.DataSourceKindAdapter) bool{
		domain.CapabilityMetrics: func(k port.DataSourceKindAdapter) bool { _, ok := k.(port.MetricsQuerier); return ok },
		domain.CapabilityRUM:     func(k port.DataSourceKindAdapter) bool { _, ok := k.(port.MetricsQuerier); return ok },
		domain.CapabilityLogs:    func(k port.DataSourceKindAdapter) bool { _, ok := k.(port.LogsQuerier); return ok },
		domain.CapabilityTraces:  func(k port.DataSourceKindAdapter) bool { _, ok := k.(port.TracesQuerier); return ok },
		domain.CapabilityErrors:  func(k port.DataSourceKindAdapter) bool { _, ok := k.(port.ErrorsQuerier); return ok },
		domain.CapabilityCrashes: func(k port.DataSourceKindAdapter) bool { _, ok := k.(port.TableQuerier); return ok },
		domain.CapabilityQueues:  func(k port.DataSourceKindAdapter) bool { _, ok := k.(port.QueueDiscoverer); return ok },
	}
	for _, k := range dataSourceKinds() {
		for _, c := range k.Capabilities() {
			check, known := implements[c]
			if assert.True(t, known, "%s declares %s", k.Kind(), c) {
				assert.True(t, check(k), "%s declares %s but does not implement it", k.Kind(), c)
			}
		}
	}
}
