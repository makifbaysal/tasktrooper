package datasource

import (
	"regexp"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The store and Sentry kinds speak JSON queries held to the broker kinds'
// rules: a param may sit only in a value naming what is read (the app, the
// project, its environment or release), never in the metric or a key, and no
// built-in applies, since each kind picks the interval its API answers at.
var (
	appInventoryPattern     = regexp.MustCompile(`^app/.+$`)
	projectInventoryPattern = regexp.MustCompile(`^project/.+$`)

	storeNoBuiltins = "the store answers at its own interval"

	playVitalsDialect = brokerDialect{
		label: "Google Play vitals", nameKeys: []string{"package"}, inventory: appInventoryPattern,
		noBuiltins: storeNoBuiltins,
		parse:      func(q string) error { _, err := domain.ParsePlayVitalsQuery(q); return err },
	}
	appStoreMetricsDialect = brokerDialect{
		label: "App Store Connect metrics", nameKeys: []string{"app", "version", "build"}, inventory: appInventoryPattern,
		noBuiltins: storeNoBuiltins,
		parse:      func(q string) error { _, err := domain.ParseAppStoreMetricsQuery(q); return err },
	}
	sentryDialect = brokerDialect{
		label: "Sentry", nameKeys: []string{"project", "environment", "release"}, inventory: projectInventoryPattern,
		noBuiltins: "Sentry picks the interval from the step",
		parse:      func(q string) error { _, err := domain.ParseSentryQuery(q); return err },
	}
)
