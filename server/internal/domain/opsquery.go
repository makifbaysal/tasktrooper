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
// the answer stands without it.
type OpsUnavailable struct {
	ComponentID   uuid.UUID          `json:"component_id"`
	EnvironmentID uuid.UUID          `json:"environment_id"`
	Code          OpsUnavailableCode `json:"code"`
	Message       string             `json:"message"`
}

type OpsErrorGroup struct {
	ComponentID   uuid.UUID         `json:"component_id"`
	RepositoryID  uuid.UUID         `json:"repository_id"`
	EnvironmentID uuid.UUID         `json:"environment_id"`
	Provider      CloudProviderKind `json:"provider"`
	RuntimeErrorGroup
}

// OpsErrors is GET /v1/operations/errors. Truncated: more environments
// matched than one call reads.
type OpsErrors struct {
	Groups      []OpsErrorGroup  `json:"groups"`
	Unavailable []OpsUnavailable `json:"unavailable"`
	Truncated   bool             `json:"truncated"`
}

type OpsLogEntry struct {
	RuntimeLogEntry
	ComponentID   uuid.UUID `json:"component_id"`
	EnvironmentID uuid.UUID `json:"environment_id"`
}

// OpsLogs is GET /v1/operations/logs. Truncated: the window holds more than
// was returned — an environment had more, the merge was cut to the limit, or
// environments were left out.
type OpsLogs struct {
	Entries     []OpsLogEntry    `json:"entries"`
	Unavailable []OpsUnavailable `json:"unavailable"`
	Truncated   bool             `json:"truncated"`
}
