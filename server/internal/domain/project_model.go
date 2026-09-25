package domain

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Confidence grades a detected value. Exact matches are applied without asking;
// medium ones are queued for the human; low ones never reach the model.
type Confidence string

const (
	ConfidenceExact  Confidence = "exact"
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

func (c Confidence) NeedsReview() bool { return c == ConfidenceMedium }

// SourceEvidence is a repo-relative path (optionally a line) a detected value
// was read from.
type SourceEvidence struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Note string `json:"note,omitempty"`
}

func (e SourceEvidence) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d", e.Path, e.Line)
	}
	return e.Path
}

// Fact is one field of the project model: what a scan detected and what the
// human set. A rescan only ever rewrites Detected; Override survives every
// rescan until the human reverts it.
type Fact[T any] struct {
	Detected   *T               `json:"detected,omitempty"`
	Override   *T               `json:"override,omitempty"`
	Confidence Confidence       `json:"confidence,omitempty"`
	Evidence   []SourceEvidence `json:"evidence,omitempty"`
}

func Detected[T any](v T, c Confidence, ev ...SourceEvidence) Fact[T] {
	return Fact[T]{Detected: &v, Confidence: c, Evidence: ev}
}

func (f Fact[T]) Value() (T, bool) {
	if f.Override != nil {
		return *f.Override, true
	}
	if f.Detected != nil {
		return *f.Detected, true
	}
	var zero T
	return zero, false
}

func (f Fact[T]) Get() T {
	v, _ := f.Value()
	return v
}

func (f Fact[T]) Overridden() bool { return f.Override != nil }

// WithDetected replaces the detected half and keeps the human's override.
func (f Fact[T]) WithDetected(next Fact[T]) Fact[T] {
	next.Override = f.Override
	return next
}

type ComponentRole string

const (
	ComponentRoleFrontend ComponentRole = "frontend"
	ComponentRoleBackend  ComponentRole = "backend"
	ComponentRoleMobile   ComponentRole = "mobile"
	ComponentRoleDesktop  ComponentRole = "desktop"
	ComponentRoleWorker   ComponentRole = "worker"
	ComponentRoleLibrary  ComponentRole = "library"
	ComponentRoleInfra    ComponentRole = "infra"
	ComponentRoleCLI      ComponentRole = "cli"
	ComponentRoleOther    ComponentRole = "other"
)

func AllComponentRoles() []ComponentRole {
	return []ComponentRole{
		ComponentRoleFrontend, ComponentRoleBackend, ComponentRoleMobile, ComponentRoleDesktop,
		ComponentRoleWorker, ComponentRoleLibrary, ComponentRoleInfra, ComponentRoleCLI, ComponentRoleOther,
	}
}

func ValidComponentRole(r ComponentRole) bool {
	for _, known := range AllComponentRoles() {
		if r == known {
			return true
		}
	}
	return false
}

// LegacyRepoKind maps a role onto the pre-component repo kinds that storeops,
// deploy, prodops and the pipeline gate still read.
func (r ComponentRole) LegacyRepoKind() string {
	switch r {
	case ComponentRoleFrontend, ComponentRoleDesktop:
		return RepoKindFrontend
	case ComponentRoleMobile:
		return RepoKindMobile
	case ComponentRoleWorker:
		return RepoKindWorker
	default:
		return RepoKindBackend
	}
}

type ComponentStatus string

const (
	ComponentStatusActive    ComponentStatus = "active"
	ComponentStatusDismissed ComponentStatus = "dismissed"
)

// StackItem is one named technology with the version the tree pins, if any.
type StackItem struct {
	Name     string          `json:"name"`
	Version  string          `json:"version,omitempty"`
	Evidence *SourceEvidence `json:"evidence,omitempty"`
}

type ComponentStack struct {
	Languages      []StackItem `json:"languages,omitempty"`
	Frameworks     []StackItem `json:"frameworks,omitempty"`
	Libraries      []StackItem `json:"libraries,omitempty"`
	Runtime        *StackItem  `json:"runtime,omitempty"`
	PackageManager string      `json:"package_manager,omitempty"`
	Container      string      `json:"container,omitempty"`
	// PackageName and DevPort are how other components reach this one (an
	// import name, a localhost port); the cross-repository matcher reads them.
	PackageName string `json:"package_name,omitempty"`
	DevPort     int    `json:"dev_port,omitempty"`
}

func (s ComponentStack) Summary(limit int) string {
	var parts []string
	add := func(items []StackItem) {
		for _, it := range items {
			label := it.Name
			if it.Version != "" {
				label += " " + it.Version
			}
			parts = append(parts, label)
		}
	}
	add(s.Frameworks)
	add(s.Languages)
	add(s.Libraries)
	if limit > 0 && len(parts) > limit {
		parts = parts[:limit]
	}
	return strings.Join(parts, " · ")
}

type CommandPurpose string

const (
	CommandInstall   CommandPurpose = "install"
	CommandDev       CommandPurpose = "dev"
	CommandBuild     CommandPurpose = "build"
	CommandTest      CommandPurpose = "test"
	CommandLint      CommandPurpose = "lint"
	CommandTypecheck CommandPurpose = "typecheck"
	CommandFormat    CommandPurpose = "format"
	CommandE2E       CommandPurpose = "e2e"
	CommandMigrate   CommandPurpose = "migrate"
)

func AllCommandPurposes() []CommandPurpose {
	return []CommandPurpose{CommandInstall, CommandDev, CommandBuild, CommandTest, CommandLint, CommandTypecheck, CommandFormat, CommandE2E, CommandMigrate}
}

func ValidCommandPurpose(p CommandPurpose) bool {
	for _, known := range AllCommandPurposes() {
		if p == known {
			return true
		}
	}
	return false
}

// ComponentCommand runs inside the component's own directory.
type ComponentCommand struct {
	Purpose CommandPurpose `json:"purpose"`
	Command Fact[string]   `json:"command"`
}

// MobileFacts is the store identity and release targets read off a mobile
// component; storeops builds releases from these and refuses on "".
type MobileFacts struct {
	Platform     string       `json:"platform,omitempty"`
	Identity     AppIdentity  `json:"identity"`
	BuildTargets BuildTargets `json:"build_targets"`
}

// ComponentGates carries the per-component quality-gate overrides that used to
// ride inside repositories.sub_projects; nil means the default (gate off;
// coverage threshold falls back to the board default).
type ComponentGates struct {
	CoverageEnabled   *bool    `json:"coverage_enabled,omitempty"`
	CoverageThreshold *float64 `json:"coverage_threshold,omitempty"`
	MutationEnabled   *bool    `json:"mutation_enabled,omitempty"`
	MutationThreshold *float64 `json:"mutation_threshold,omitempty"`
}

// Component is one buildable/deployable unit of a repository: the whole repo
// ("." ) for a single-purpose repo, one directory per unit in a monorepo.
type Component struct {
	ID           uuid.UUID            `json:"id"`
	RepositoryID uuid.UUID            `json:"repository_id"`
	Path         string               `json:"path"`
	Name         Fact[string]         `json:"name"`
	Role         Fact[ComponentRole]  `json:"role"`
	Stack        Fact[ComponentStack] `json:"stack"`
	Commands     []ComponentCommand   `json:"commands"`
	Mobile       *Fact[MobileFacts]   `json:"mobile,omitempty"`
	Docs         RepositoryDocs       `json:"docs"`
	Gates        ComponentGates       `json:"gates"`
	Status       ComponentStatus      `json:"status"`
	// ManuallyAdded components are never deleted by a rescan that no longer
	// finds them.
	ManuallyAdded bool `json:"manually_added"`
	// NeedsReview marks a component a later scan (a push, not the first
	// import) found on its own; the human keeps or dismisses it.
	NeedsReview bool       `json:"needs_review"`
	LastScanID  *uuid.UUID `json:"last_scan_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (c Component) DisplayName() string {
	if n := strings.TrimSpace(c.Name.Get()); n != "" {
		return n
	}
	if c.Path == "." || c.Path == "" {
		return "root"
	}
	return path.Base(c.Path)
}

func (c Component) Command(p CommandPurpose) string {
	for _, cmd := range c.Commands {
		if cmd.Purpose == p {
			return cmd.Command.Get()
		}
	}
	return ""
}

// Contains reports whether a repo-relative path belongs to this component; the
// root component contains everything no deeper component claims, which the
// caller resolves with OwningComponent.
func (c Component) Contains(p string) bool {
	if c.Path == "." || c.Path == "" {
		return true
	}
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	return p == c.Path || strings.HasPrefix(p, c.Path+"/")
}

// OwningComponent picks the deepest active component whose path contains p.
func OwningComponent(components []Component, p string) (Component, bool) {
	var best Component
	found := false
	for _, c := range components {
		if c.Status != ComponentStatusActive || !c.Contains(p) {
			continue
		}
		if !found || len(c.Path) > len(best.Path) || best.Path == "." {
			best, found = c, true
		}
	}
	return best, found
}

// NormalizeComponentPath returns the repo-relative, slash-separated form with
// "." for the root; ".." escapes are rejected.
func NormalizeComponentPath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || p == "." || p == "./" || p == "/" {
		return ".", nil
	}
	clean := path.Clean(strings.TrimPrefix(p, "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("component path %q escapes the repository", p)
	}
	return clean, nil
}

type CheckPurpose string

const (
	CheckLint      CheckPurpose = "lint"
	CheckTypecheck CheckPurpose = "typecheck"
	CheckTest      CheckPurpose = "test"
	CheckBuild     CheckPurpose = "build"
	CheckE2E       CheckPurpose = "e2e"
	CheckSecurity  CheckPurpose = "security"
	CheckDeploy    CheckPurpose = "deploy"
	CheckRelease   CheckPurpose = "release"
	CheckOther     CheckPurpose = "other"
)

func ValidCheckPurpose(p CheckPurpose) bool {
	switch p {
	case CheckLint, CheckTypecheck, CheckTest, CheckBuild, CheckE2E, CheckSecurity, CheckDeploy, CheckRelease, CheckOther:
		return true
	}
	return false
}

// CheckGate decides what an agent's hand-off does with a check: required ones
// are run locally before the hand-off and read from CI by the pipeline gate.
type CheckGate string

const (
	CheckGateRequired CheckGate = "required"
	CheckGateInfo     CheckGate = "info"
	CheckGateOff      CheckGate = "off"
)

func ValidCheckGate(g CheckGate) bool {
	return g == CheckGateRequired || g == CheckGateInfo || g == CheckGateOff
}

// DefaultCheckGate: only cheap, locally reproducible verification is required
// by default; everything else informs.
func DefaultCheckGate(p CheckPurpose, runnable bool) CheckGate {
	switch p {
	case CheckLint, CheckTypecheck, CheckTest, CheckBuild:
		if runnable {
			return CheckGateRequired
		}
	}
	return CheckGateInfo
}

type DeployEnvironment string

const (
	EnvironmentProduction  DeployEnvironment = "production"
	EnvironmentStaging     DeployEnvironment = "staging"
	EnvironmentPreview     DeployEnvironment = "preview"
	EnvironmentDevelopment DeployEnvironment = "development"
)

// LocalCommand is one argv run without a shell inside Dir (repo-relative).
type LocalCommand struct {
	Dir  string   `json:"dir"`
	Argv []string `json:"argv"`
}

func (l LocalCommand) Display() string {
	cmd := strings.Join(l.Argv, " ")
	if l.Dir == "" || l.Dir == "." {
		return cmd
	}
	return "cd " + l.Dir + " && " + cmd
}

// JoinLocalCommands renders a command sequence the way a person would type
// it: commands sharing a directory run under one `cd`, groups separated by
// "; " because every Dir is relative to the repository root.
func JoinLocalCommands(cmds []LocalCommand) string {
	var groups []string
	var current []string
	currentDir := ""
	flush := func() {
		if len(current) == 0 {
			return
		}
		g := strings.Join(current, " && ")
		if currentDir != "." {
			g = "cd " + currentDir + " && " + g
		}
		groups = append(groups, g)
		current = nil
	}
	for i, c := range cmds {
		dir := c.Dir
		if dir == "" {
			dir = "."
		}
		if i == 0 || dir != currentDir {
			flush()
			currentDir = dir
		}
		current = append(current, strings.Join(c.Argv, " "))
	}
	flush()
	return strings.Join(groups, "; ")
}

type CheckStep struct {
	Name             string `json:"name,omitempty"`
	Run              string `json:"run,omitempty"`
	Uses             string `json:"uses,omitempty"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}

type CheckSource string

const (
	CheckSourceCI     CheckSource = "ci"
	CheckSourceManual CheckSource = "manual"
)

type ModelStatus string

const (
	ModelStatusActive    ModelStatus = "active"
	ModelStatusDismissed ModelStatus = "dismissed"
)

// ComponentCheck is a CI job mapped onto the component it verifies. One job
// that touches two components becomes one check per component, each holding
// only that component's local commands.
type ComponentCheck struct {
	ID            uuid.UUID            `json:"id"`
	RepositoryID  uuid.UUID            `json:"repository_id"`
	ComponentID   uuid.UUID            `json:"component_id"`
	Source        CheckSource          `json:"source"`
	Workflow      string               `json:"workflow"`
	WorkflowName  string               `json:"workflow_name,omitempty"`
	JobKey        string               `json:"job_key"`
	JobName       string               `json:"job_name,omitempty"`
	Purpose       Fact[CheckPurpose]   `json:"purpose"`
	Environment   DeployEnvironment    `json:"environment,omitempty"`
	Triggers      []string             `json:"triggers,omitempty"`
	PathFilters   []string             `json:"path_filters,omitempty"`
	Steps         []CheckStep          `json:"steps,omitempty"`
	LocalCommands Fact[[]LocalCommand] `json:"local_commands"`
	Gate          Fact[CheckGate]      `json:"gate"`
	Dispatchable  bool                 `json:"dispatchable"`
	Status        ModelStatus          `json:"status"`
	// Missing is set when the latest scan no longer found this job but the
	// human had touched the row, so it is kept rather than deleted.
	Missing bool `json:"missing"`
	// NeedsReview marks a required check a later scan added, since it changes
	// what every agent must pass before hand-off.
	NeedsReview bool      `json:"needs_review"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (c ComponentCheck) Runnable() bool { return len(c.LocalCommands.Get()) > 0 }

func (c ComponentCheck) Required() bool {
	return c.Status == ModelStatusActive && !c.Missing && c.Gate.Get() == CheckGateRequired
}

type ResourceKind string

const (
	ResourceDatabase      ResourceKind = "database"
	ResourceCache         ResourceKind = "cache"
	ResourceQueue         ResourceKind = "queue"
	ResourceStorage       ResourceKind = "storage"
	ResourceSearch        ResourceKind = "search"
	ResourceAPI           ResourceKind = "api"
	ResourceAuth          ResourceKind = "auth"
	ResourceEmail         ResourceKind = "email"
	ResourcePayments      ResourceKind = "payments"
	ResourceAI            ResourceKind = "ai"
	ResourceObservability ResourceKind = "observability"
	ResourceOther         ResourceKind = "other"
)

func ValidResourceKind(k ResourceKind) bool {
	switch k {
	case ResourceDatabase, ResourceCache, ResourceQueue, ResourceStorage, ResourceSearch, ResourceAPI,
		ResourceAuth, ResourceEmail, ResourcePayments, ResourceAI, ResourceObservability, ResourceOther:
		return true
	}
	return false
}

// SystemResource is a workspace-wide node something connects to. IdentityKey
// dedupes it: a SaaS API is one node however many components call it; a
// database seen only in code is scoped to its repository until a cloud binding
// proves two repositories share it.
type SystemResource struct {
	ID          uuid.UUID         `json:"id"`
	Kind        ResourceKind      `json:"kind"`
	Vendor      string            `json:"vendor"`
	Name        string            `json:"name"`
	IdentityKey string            `json:"identity_key"`
	Details     map[string]string `json:"details,omitempty"`
	// NameLocked marks a human-renamed resource: EnsureResource must never let
	// a rescan's detected name overwrite it.
	NameLocked bool      `json:"name_locked"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type LinkProtocol string

const (
	LinkHTTP    LinkProtocol = "http"
	LinkGRPC    LinkProtocol = "grpc"
	LinkGraphQL LinkProtocol = "graphql"
	LinkSQL     LinkProtocol = "sql"
	LinkRedis   LinkProtocol = "redis"
	LinkQueue   LinkProtocol = "queue"
	LinkSDK     LinkProtocol = "sdk"
	LinkPackage LinkProtocol = "package"
	LinkOther   LinkProtocol = "other"
)

func ValidLinkProtocol(p LinkProtocol) bool {
	switch p {
	case LinkHTTP, LinkGRPC, LinkGraphQL, LinkSQL, LinkRedis, LinkQueue, LinkSDK, LinkPackage, LinkOther:
		return true
	}
	return false
}

type LinkStatus string

const (
	LinkSuggested LinkStatus = "suggested"
	LinkConfirmed LinkStatus = "confirmed"
	LinkDismissed LinkStatus = "dismissed"
)

type LinkSource string

const (
	LinkSourceScan LinkSource = "scan"
	LinkSourceUser LinkSource = "user"
)

// ComponentLink is one outgoing edge. Exactly one of ToComponentID and
// ToResourceID is set once resolved; an unresolved suggestion has neither and
// carries its hint for the human to pick a target.
type ComponentLink struct {
	ID              uuid.UUID    `json:"id"`
	RepositoryID    uuid.UUID    `json:"repository_id"`
	FromComponentID uuid.UUID    `json:"from_component_id"`
	ToComponentID   *uuid.UUID   `json:"to_component_id,omitempty"`
	ToResourceID    *uuid.UUID   `json:"to_resource_id,omitempty"`
	Protocol        LinkProtocol `json:"protocol"`
	// Detail is protocol-specific wording, e.g. "publish"/"subscribe" on a queue.
	Detail     string           `json:"detail,omitempty"`
	EnvVars    []string         `json:"env_vars,omitempty"`
	Evidence   []SourceEvidence `json:"evidence,omitempty"`
	Confidence Confidence       `json:"confidence"`
	// Reason is the matcher's one-line justification shown next to a suggestion.
	Reason string     `json:"reason,omitempty"`
	Hint   string     `json:"hint,omitempty"`
	Status LinkStatus `json:"status"`
	Source LinkSource `json:"source"`
	// AutoConfirmed marks an exact match the platform confirmed without asking.
	AutoConfirmed bool `json:"auto_confirmed"`
	// SignalKey is the scan's natural key ("sdk:stripe", "env:BILLING_SVC_URL")
	// so a rescan updates this row instead of resurrecting a dismissed one.
	SignalKey string `json:"signal_key,omitempty"`
	// TargetHost/TargetPort keep what an unresolved URL pointed at, so the
	// link can be matched later against environments bound after the scan.
	TargetHost string    `json:"target_host,omitempty"`
	TargetPort int       `json:"target_port,omitempty"`
	Missing    bool      `json:"missing"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (l ComponentLink) Resolved() bool { return l.ToComponentID != nil || l.ToResourceID != nil }

type ScanTrigger string

const (
	ScanTriggerImport  ScanTrigger = "import"
	ScanTriggerManual  ScanTrigger = "manual"
	ScanTriggerPush    ScanTrigger = "push"
	ScanTriggerStale   ScanTrigger = "stale"
	ScanTriggerMigrate ScanTrigger = "migrate"
)

type ScanStatus string

const (
	ScanQueued    ScanStatus = "queued"
	ScanRunning   ScanStatus = "running"
	ScanSucceeded ScanStatus = "succeeded"
	ScanFailed    ScanStatus = "failed"
)

type ScanStage string

const (
	ScanStageClone      ScanStage = "clone"
	ScanStageInventory  ScanStage = "inventory"
	ScanStageShape      ScanStage = "shape"
	ScanStageComponents ScanStage = "components"
	ScanStageStack      ScanStage = "stack"
	ScanStageChecks     ScanStage = "checks"
	ScanStageLinks      ScanStage = "links"
	ScanStageDeploy     ScanStage = "deploy"
	ScanStageMatch      ScanStage = "match"
)

// ScanEvent is one progress line the add-repository flow streams; Summary is
// a finished English sentence of what the stage found.
type ScanEvent struct {
	Stage   ScanStage `json:"stage"`
	Done    bool      `json:"done"`
	Summary string    `json:"summary,omitempty"`
	At      time.Time `json:"at"`
}

type ProjectScan struct {
	ID           uuid.UUID   `json:"id"`
	RepositoryID uuid.UUID   `json:"repository_id"`
	Trigger      ScanTrigger `json:"trigger"`
	Status       ScanStatus  `json:"status"`
	Stage        ScanStage   `json:"stage,omitempty"`
	CommitSHA    string      `json:"commit_sha,omitempty"`
	Events       []ScanEvent `json:"events"`
	Result       *ScanResult `json:"result,omitempty"`
	// ReviewCount is how many medium-confidence items this scan queued.
	ReviewCount int        `json:"review_count"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

func (s ProjectScan) Finished() bool { return s.Status == ScanSucceeded || s.Status == ScanFailed }

// RepoShape is computed per repository; a project's type is computed from its
// repositories' shapes and never stored.
type RepoShape string

const (
	RepoShapeSingle   RepoShape = "single"
	RepoShapeMonorepo RepoShape = "monorepo"
)

type ProjectType string

const (
	ProjectTypeEmpty     ProjectType = "empty"
	ProjectTypeSingle    ProjectType = "single_repo"
	ProjectTypeMonorepo  ProjectType = "monorepo"
	ProjectTypeMultiRepo ProjectType = "multi_repo"
)

func ComputeProjectType(repoShapes []RepoShape) ProjectType {
	switch len(repoShapes) {
	case 0:
		return ProjectTypeEmpty
	case 1:
		if repoShapes[0] == RepoShapeMonorepo {
			return ProjectTypeMonorepo
		}
		return ProjectTypeSingle
	default:
		return ProjectTypeMultiRepo
	}
}

func ShapeFromComponents(components []Component) RepoShape {
	active := 0
	for _, c := range components {
		if c.Status == ComponentStatusActive {
			active++
		}
	}
	if active > 1 {
		return RepoShapeMonorepo
	}
	return RepoShapeSingle
}
