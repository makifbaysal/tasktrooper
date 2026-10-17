package domain

import "github.com/google/uuid"

// OpsUnavailableCode says why one environment is missing from a
// cross-component Operations answer, so the UI can offer the right fix.
type OpsUnavailableCode string

const (
	OpsNotConnected  OpsUnavailableCode = "not_connected"
	OpsCloudAuth     OpsUnavailableCode = "cloud_auth"
	OpsProviderError OpsUnavailableCode = "provider_error"
	OpsTimeout       OpsUnavailableCode = "timeout"
)

// OpsUnavailable is one environment a fan-out could not read; the rest of
// the answer stands without it. One read through a component's data source
// binding names that source, with the zero EnvironmentID.
type OpsUnavailable struct {
	ComponentID   uuid.UUID          `json:"component_id"`
	EnvironmentID uuid.UUID          `json:"environment_id"`
	DataSourceID  *uuid.UUID         `json:"data_source_id,omitempty"`
	Code          OpsUnavailableCode `json:"code"`
	Message       string             `json:"message"`
}

// OpsErrorGroup: a group read through a component's errors.issues binding
// carries the data source it came from, the zero EnvironmentID, and the
// source's kind as Provider ("sentry").
type OpsErrorGroup struct {
	ComponentID   uuid.UUID         `json:"component_id"`
	RepositoryID  uuid.UUID         `json:"repository_id"`
	EnvironmentID uuid.UUID         `json:"environment_id"`
	DataSourceID  *uuid.UUID        `json:"data_source_id,omitempty"`
	Provider      CloudProviderKind `json:"provider"`
	RuntimeErrorGroup
	// Definition is the active error definition the group matches (migration
	// 191); null when none does.
	Definition *ErrorDefinitionRef `json:"definition"`
}

// OpsErrors is GET /v1/operations/errors. Truncated: more environments
// matched than one call reads. IgnoredCount is how many groups an ignore
// definition left out.
type OpsErrors struct {
	Groups       []OpsErrorGroup  `json:"groups"`
	Unavailable  []OpsUnavailable `json:"unavailable"`
	Truncated    bool             `json:"truncated"`
	IgnoredCount int              `json:"ignored_count,omitempty"`
}

// OpsLogEntry: an entry read through a component's logs binding carries the
// data source it came from and the zero EnvironmentID, since no cloud
// environment was read.
type OpsLogEntry struct {
	RuntimeLogEntry
	ComponentID   uuid.UUID  `json:"component_id"`
	EnvironmentID uuid.UUID  `json:"environment_id"`
	DataSourceID  *uuid.UUID `json:"data_source_id,omitempty"`
}

// OpsLogs is GET /v1/operations/logs. Truncated: the window holds more than
// was returned — an environment had more, the merge was cut to the limit, or
// environments were left out.
type OpsLogs struct {
	Entries     []OpsLogEntry    `json:"entries"`
	Unavailable []OpsUnavailable `json:"unavailable"`
	Truncated   bool             `json:"truncated"`
}
