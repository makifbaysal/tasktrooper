package domain

import (
	"time"

	"github.com/google/uuid"
)

type CloudProviderKind string

const (
	CloudVercel CloudProviderKind = "vercel"
	CloudGCP    CloudProviderKind = "gcp"
	CloudAWS    CloudProviderKind = "aws"
)

func ValidCloudProvider(p CloudProviderKind) bool {
	return p == CloudVercel || p == CloudGCP || p == CloudAWS
}

type CloudAccountStatus string

const (
	CloudAccountOK         CloudAccountStatus = "ok"
	CloudAccountError      CloudAccountStatus = "error"
	CloudAccountUnverified CloudAccountStatus = "unverified"
)

// CloudAccount is one connected provider login. Its credential lives only in
// the store, encrypted; Meta holds the non-secret identity the provider
// reported (team, project id, account id, region, client email).
type CloudAccount struct {
	ID           uuid.UUID          `json:"id"`
	Provider     CloudProviderKind  `json:"provider"`
	Label        string             `json:"label"`
	Meta         map[string]string  `json:"meta"`
	Status       CloudAccountStatus `json:"status"`
	StatusDetail string             `json:"status_detail,omitempty"`
	VerifiedAt   *time.Time         `json:"verified_at,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// SaveCloudAccountRequest carries the secret fields once, on the way in:
// vercel {token, team_id?}; gcp {service_account_json}; aws {access_key_id,
// secret_access_key, session_token?, region}.
type SaveCloudAccountRequest struct {
	Provider CloudProviderKind `json:"provider"`
	Label    string            `json:"label,omitempty"`
	Fields   map[string]string `json:"fields"`
}

// CloudCredential is what an adapter receives to call its provider.
type CloudCredential struct {
	AccountID uuid.UUID
	Provider  CloudProviderKind
	Meta      map[string]string
	Fields    map[string]string
}

type CloudResourceKind string

const (
	CloudResourceVercelProject    CloudResourceKind = "vercel_project"
	CloudResourceCloudRunService  CloudResourceKind = "cloud_run_service"
	CloudResourceCloudRunJob      CloudResourceKind = "cloud_run_job"
	CloudResourceAppEngineService CloudResourceKind = "app_engine_service"
	CloudResourceCloudFunction    CloudResourceKind = "cloud_function"
	CloudResourceGKEWorkload      CloudResourceKind = "gke_workload"
	CloudResourceECSService       CloudResourceKind = "ecs_service"
	CloudResourceLambdaFunction   CloudResourceKind = "lambda_function"
	CloudResourceAppRunnerService CloudResourceKind = "app_runner_service"
)

// CloudResourceRef identifies one deployable thing inside an account. ID is
// the provider-native identifier (prj_…, a Cloud Run resource name, an ARN).
type CloudResourceRef struct {
	Kind   CloudResourceKind `json:"kind"`
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Region string            `json:"region,omitempty"`
	Extra  map[string]string `json:"extra,omitempty"`
}

// CloudResource is one row of an account's resource listing; Labels carry
// what the matcher compares against deploy signals (git repo slug, root
// directory, framework, custom domains).
type CloudResource struct {
	AccountID uuid.UUID         `json:"account_id"`
	Provider  CloudProviderKind `json:"provider"`
	Ref       CloudResourceRef  `json:"ref"`
	URL       string            `json:"url,omitempty"`
	Domains   []string          `json:"domains,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type CloudResourceStatus string

const (
	CloudStatusHealthy   CloudResourceStatus = "healthy"
	CloudStatusDeploying CloudResourceStatus = "deploying"
	CloudStatusDegraded  CloudResourceStatus = "degraded"
	CloudStatusFailed    CloudResourceStatus = "failed"
	CloudStatusUnknown   CloudResourceStatus = "unknown"
)

type KeyValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type CloudResourceDetail struct {
	CloudResource
	Status           CloudResourceStatus `json:"status"`
	StatusDetail     string              `json:"status_detail,omitempty"`
	Revision         string              `json:"revision,omitempty"`
	ConsoleURL       string              `json:"console_url,omitempty"`
	LatestDeployment *CloudDeployment    `json:"latest_deployment,omitempty"`
	Facts            []KeyValue          `json:"facts,omitempty"`
}

type CloudDeploymentStatus string

const (
	CloudDeployReady    CloudDeploymentStatus = "ready"
	CloudDeployBuilding CloudDeploymentStatus = "building"
	CloudDeployError    CloudDeploymentStatus = "error"
	CloudDeployCanceled CloudDeploymentStatus = "canceled"
	CloudDeployUnknown  CloudDeploymentStatus = "unknown"
)

type CloudDeployment struct {
	ID            string                `json:"id"`
	Status        CloudDeploymentStatus `json:"status"`
	Environment   DeployEnvironment     `json:"environment,omitempty"`
	CommitSHA     string                `json:"commit_sha,omitempty"`
	CommitMessage string                `json:"commit_message,omitempty"`
	Branch        string                `json:"branch,omitempty"`
	URL           string                `json:"url,omitempty"`
	Creator       string                `json:"creator,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	ReadyAt       *time.Time            `json:"ready_at,omitempty"`
	InspectURL    string                `json:"inspect_url,omitempty"`
}

type LogSeverity string

const (
	LogDebug    LogSeverity = "debug"
	LogInfo     LogSeverity = "info"
	LogWarning  LogSeverity = "warning"
	LogError    LogSeverity = "error"
	LogCritical LogSeverity = "critical"
)

func (s LogSeverity) Rank() int {
	switch s {
	case LogDebug:
		return 0
	case LogInfo:
		return 1
	case LogWarning:
		return 2
	case LogError:
		return 3
	case LogCritical:
		return 4
	}
	return 1
}

type RuntimeLogQuery struct {
	Since       time.Time   `json:"since"`
	Until       time.Time   `json:"until"`
	MinSeverity LogSeverity `json:"min_severity,omitempty"`
	Text        string      `json:"text,omitempty"`
	Limit       int         `json:"limit,omitempty"`
	Cursor      string      `json:"cursor,omitempty"`
}

type RuntimeLogEntry struct {
	Timestamp  time.Time         `json:"timestamp"`
	Severity   LogSeverity       `json:"severity"`
	Message    string            `json:"message"`
	Source     string            `json:"source,omitempty"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	StatusCode int               `json:"status_code,omitempty"`
	TraceID    string            `json:"trace_id,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
}

type RuntimeLogPage struct {
	Entries    []RuntimeLogEntry `json:"entries"`
	NextCursor string            `json:"next_cursor,omitempty"`
	// Truncated reports that the provider capped the window; the UI says so
	// instead of implying the list is complete.
	Truncated bool `json:"truncated,omitempty"`
}

// RuntimeErrorGroup is one recurring error. Providers with native grouping
// (GCP Error Reporting) fill it directly; for the others the application
// groups error-level log lines by a normalised message fingerprint.
type RuntimeErrorGroup struct {
	Fingerprint string    `json:"fingerprint"`
	Message     string    `json:"message"`
	Count       int       `json:"count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Sample      string    `json:"sample,omitempty"`
	Source      string    `json:"source,omitempty"`
	// New is set when the group's first occurrence falls inside the queried
	// window — the "started with this deploy" signal.
	New         bool   `json:"new"`
	ExternalURL string `json:"external_url,omitempty"`
}

// ComponentEnvironment binds one component's environment to where it runs.
// Provider "" with only URL/HealthURL is a custom environment the platform
// can probe but not read logs from.
type ComponentEnvironment struct {
	ID           uuid.UUID         `json:"id"`
	RepositoryID uuid.UUID         `json:"repository_id"`
	ComponentID  uuid.UUID         `json:"component_id"`
	Environment  DeployEnvironment `json:"environment"`
	Provider     CloudProviderKind `json:"provider,omitempty"`
	AccountID    *uuid.UUID        `json:"account_id,omitempty"`
	Resource     *CloudResourceRef `json:"resource,omitempty"`
	URL          string            `json:"url,omitempty"`
	HealthURL    string            `json:"health_url,omitempty"`
	Status       LinkStatus        `json:"status"`
	Source       LinkSource        `json:"source"`
	Confidence   Confidence        `json:"confidence"`
	Reason       string            `json:"reason,omitempty"`
	// AutoConfirmed marks a binding the matcher made from an exact signal
	// (a .vercel project id, a unique service name) without asking.
	AutoConfirmed bool `json:"auto_confirmed"`
	// Candidates are the resources an ambiguous signal could mean; the human
	// picks one. Empty once confirmed.
	Candidates []CloudResource `json:"candidates,omitempty"`
	SignalKey  string          `json:"signal_key,omitempty"`
	// Health is the last background probe's summary; nil until one ran.
	Health    *EnvironmentHealth `json:"health,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

func (e ComponentEnvironment) Bound() bool { return e.AccountID != nil && e.Resource != nil }

type SaveEnvironmentRequest struct {
	AccountID *uuid.UUID        `json:"account_id,omitempty"`
	Resource  *CloudResourceRef `json:"resource,omitempty"`
	URL       string            `json:"url,omitempty"`
	HealthURL string            `json:"health_url,omitempty"`
}

// EnvironmentRuntime is GET …/environments/:env/overview: the live picture
// the Deploy & Runtime tab opens on.
type EnvironmentRuntime struct {
	Environment ComponentEnvironment `json:"environment"`
	Detail      *CloudResourceDetail `json:"detail,omitempty"`
	Deployments []CloudDeployment    `json:"deployments"`
	ErrorsLast  []RuntimeErrorGroup  `json:"errors"`
	Unavailable string               `json:"unavailable,omitempty"`
	// UnavailableCode lets the UI act on why: "not_connected", "cloud_auth"
	// (reconnect the account) or "provider_error".
	UnavailableCode string `json:"unavailable_code,omitempty"`
}

// EnvironmentHealth is refreshed by a background sweep so list views never
// call a provider on load.
type EnvironmentHealth struct {
	Status        CloudResourceStatus `json:"status"`
	ErrorCount24h int                 `json:"error_count_24h"`
	LastDeployAt  *time.Time          `json:"last_deploy_at,omitempty"`
	CheckedAt     time.Time           `json:"checked_at"`
	Detail        string              `json:"detail,omitempty"`
}
