package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Repository struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	RootPath    string    `json:"root_path"`
	// RemoteURL is the git origin the working copy came from, persisted so a
	// lost RootPath can be restored without the human supplying the path again.
	RemoteURL     string `json:"remote_url,omitempty"`
	VerifyCommand string `json:"verify_command"`
	BuildCommand  string `json:"build_command"`
	TestCommand   string `json:"test_command"`
	// CoverageThreshold is the whole-repo line-coverage bar; 0 = the board's
	// 90% default. It blocks nothing — coverage is measured and stated in the
	// hand-off, never used to hold a task.
	CoverageThreshold float64 `json:"coverage_threshold,omitempty"`
	// RequireOverallCoverage decides how a shortfall is phrased (repo's own bar
	// vs. bare figure), not whether anything stops.
	RequireOverallCoverage bool `json:"require_overall_coverage"`
	// MutationEnabled arms the mutation-score bar the same way, phrasing only;
	// off by default because a repo with no mutation tooling scores nothing.
	MutationEnabled bool `json:"mutation_enabled"`
	// MutationThreshold is the percentage of mutants a run is expected to kill;
	// 0 reports the score bare.
	MutationThreshold float64 `json:"mutation_threshold,omitempty"`
	// Kind feeds pipeline auto-detect's keyword set (e.g. mobile never matches
	// a docker build). One of RepoKind*.
	Kind string `json:"kind"`
	// MobilePlatform is only meaningful when Kind == RepoKindMobile; one of
	// MobilePlatform*, "" = unset.
	MobilePlatform string `json:"mobile_platform,omitempty"`
	// ReleaseEngine is where mobile releases are built (auto/actions/local),
	// only meaningful on a mobile repo; a per-environment answer would be wrong
	// because both engines produce the same artifact.
	ReleaseEngine string `json:"release_engine,omitempty"`
	// DetectedAppIdentity is the store identity read off the working copy at
	// import, prefilling a deploy target a human still confirms.
	DetectedAppIdentity AppIdentity `json:"detected_app_identity"`
	// DetectedBuildTargets is the Xcode scheme / Gradle module read off the
	// working copy, and the ONLY source the generated release script gets them
	// from; either half "" refuses the release rather than guessing.
	DetectedBuildTargets BuildTargets `json:"detected_build_targets"`
	// SubRepoKinds is only meaningful when Kind == RepoKindMonorepo: the set of
	// sub-repo kinds present, each getting its own category → job mapping.
	SubRepoKinds []string `json:"sub_repo_kinds,omitempty"`
	// SubProjects is the human-curated, addressable list of sub-projects
	// detected at import — distinct from SubRepoKinds, neither derived from the
	// other.
	SubProjects []RepoSubProject `json:"sub_projects,omitempty"`
	// AutoReleaseOnDone, when false, stops the done→prod-deploy auto-trigger so
	// batched release trains are not force-deployed per task.
	AutoReleaseOnDone bool `json:"auto_release_on_done"`
	// RequireHumanReview makes code_review a human approval gate: an approval is
	// held, a rejection still goes through. Only code_review — pm_uat's forward
	// move is into human_uat, so holding both would make one person approve the
	// same task twice.
	RequireHumanReview bool `json:"require_human_review"`
	// RequireReviewChain, when true, refuses done/released until every review
	// stage the type requires has passed. Off by default: on a board not wired
	// for the chain (no QA agent, custom columns), tasks would park in front of
	// a stage nothing can satisfy.
	RequireReviewChain bool `json:"require_review_chain"`
	// RequireReleaseDeploy, when true, refuses released until a production
	// deploy actually succeeded. A repo with no deploy workflow records that as
	// SKIPPED, which is not evidence of a deploy, so this must stay off there.
	RequireReleaseDeploy bool `json:"require_release_deploy"`
	// RequirePipelineForReview (default true) holds the reviewer until the
	// build/test pipeline has reported; false dispatches immediately. It is
	// opt-OUT-able where the other gates are opt-in: this behaviour has always
	// been on. The gate still opens on timeout even here.
	RequirePipelineForReview bool `json:"require_pipeline_for_review"`
	// IncidentPolicy decides what a production incident on this repo triggers.
	IncidentPolicy IncidentPolicy `json:"incident_policy"`
	// TestStrategy decides how a task is verified: local, stage (default), or
	// per_step.
	TestStrategy string `json:"test_strategy"`
	// WebhookInstalled reports whether a GitHub push webhook is registered
	// (from the stored hook id; the secret itself never leaves the store).
	WebhookInstalled bool `json:"webhook_installed"`
	// Docs points at the four reference docs agents should read before touching
	// this repo's code; "" fields are unset.
	Docs RepositoryDocs `json:"docs"`
	// DocsTaskID is the board task of the last reference-doc bundle asked for;
	// overwritten by the next and cleared when its PR merges.
	DocsTaskID string      `json:"docs_task_id,omitempty"`
	ProjectIDs []uuid.UUID `json:"project_ids,omitempty"`
	// GitWarning is the finished sentence the repository card shows above its
	// root path. Rendered verbatim by the web app — server-composed English,
	// not a translation key, so localising it would mean threading a language
	// through every read path (a deliberate not-yet).
	GitWarning string `json:"git_warning,omitempty"`
	// GitRestorable reports that the folder is genuinely missing AND a remote
	// is recorded, i.e. the code can be fetched onto this machine.
	GitRestorable bool `json:"git_restorable,omitempty"`
	// GitRestore is the running or last restore attempt, so a reload mid-clone
	// still shows the clone.
	GitRestore *RepositoryRestore `json:"git_restore,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

const (
	RepoKindBackend  = "backend"
	RepoKindFrontend = "frontend"
	RepoKindMobile   = "mobile"
	RepoKindWorker   = "worker"
	RepoKindMonorepo = "monorepo"
)

// ValidRepoKind reports whether k is a known repo kind.
func ValidRepoKind(k string) bool {
	switch k {
	case RepoKindBackend, RepoKindFrontend, RepoKindMobile, RepoKindWorker, RepoKindMonorepo:
		return true
	}
	return false
}

// Mobile platforms, only meaningful on a mobile repo; "" is always allowed
// because a repo registered before this existed has no guess.
const (
	MobilePlatformIOS     = "ios"
	MobilePlatformAndroid = "android"
	MobilePlatformCross   = "cross_platform"
)

// AppIdentity is a mobile app's store identity READ OFF THE WORKING COPY
// (repository.DetectAppIdentity), never typed by a human here. Both fields are
// always serialised so a consumer prefilling a form sees "nobody could read
// one".
type AppIdentity struct {
	BundleID    string `json:"bundle_id"`
	PackageName string `json:"package_name"`
}

// IsZero reports whether neither identifier could be read.
func (a AppIdentity) IsZero() bool { return a.BundleID == "" && a.PackageName == "" }

// BuildTargets is what a mobile working copy calls the thing that produces its
// release artifact, READ OFF THAT WORKING COPY (repository.DetectBuildTargets).
// Unlike AppIdentity these are not a prefill anybody reviews: "" stays ""
// through to the release, where it refuses the build — the conventions they
// replace (scheme = display name, module = "app") are true often enough to
// archive a target that does not exist.
type BuildTargets struct {
	// XcodeScheme is the iOS half: the SHARED scheme name — an unshared scheme
	// is not in the repository, so CI cannot archive it.
	XcodeScheme string `json:"xcode_scheme"`
	// GradleModule is the Android half, colon-separated without the leading
	// colon — the <module> in :<module>:bundleRelease.
	GradleModule string `json:"gradle_module"`
}

// IsZero reports whether neither target could be read.
func (b BuildTargets) IsZero() bool { return b.XcodeScheme == "" && b.GradleModule == "" }

// ValidMobilePlatform reports whether p is a known mobile platform; "" is not
// one — callers that allow the unset value test for it themselves.
func ValidMobilePlatform(p string) bool {
	switch p {
	case MobilePlatformIOS, MobilePlatformAndroid, MobilePlatformCross:
		return true
	}
	return false
}

// ValidSubRepoKind reports whether k is a valid monorepo sub-repo kind
// (single-kind only; monorepo cannot nest).
func ValidSubRepoKind(k string) bool {
	switch k {
	case RepoKindBackend, RepoKindFrontend, RepoKindMobile, RepoKindWorker:
		return true
	}
	return false
}

// AllSubRepoKinds returns every valid monorepo sub-repo kind, for computing
// auto-detect suggestions.
func AllSubRepoKinds() []string {
	return []string{RepoKindBackend, RepoKindFrontend, RepoKindMobile, RepoKindWorker}
}

// RepoSubProject is one project inside a monorepo working copy, detected at
// import and curated by the human before it is saved.
type RepoSubProject struct {
	// Path is repo-relative, slash-separated; "." is the repository root.
	Path string `json:"path"`
	// Kind is one of RepoKind* except RepoKindMonorepo — monorepos do not nest.
	Kind string `json:"kind"`
	// MobilePlatform has the same meaning as Repository.MobilePlatform, scoped
	// to this sub-project.
	MobilePlatform string `json:"mobile_platform,omitempty"`
	// DetectedAppIdentity is Repository.DetectedAppIdentity scoped to this
	// sub-project; rides inside the sub_projects JSON column.
	DetectedAppIdentity AppIdentity `json:"detected_app_identity"`
	// DetectedBuildTargets is Repository.DetectedBuildTargets scoped to this
	// sub-project, never inherited: a monorepo's mobile app has its own Xcode
	// project and Gradle build.
	DetectedBuildTargets BuildTargets `json:"detected_build_targets"`
	// Docs has the same meaning as Repository.Docs, paths relative to Path.
	Docs RepositoryDocs `json:"docs,omitempty"`
	// The four quality-gate overrides; nil means "inherit the repository's
	// setting", so a sub-project with no opinion follows the repo even after
	// the repo's own number changes.
	CoverageEnabled   *bool    `json:"coverage_enabled,omitempty"`
	CoverageThreshold *float64 `json:"coverage_threshold,omitempty"`
	MutationEnabled   *bool    `json:"mutation_enabled,omitempty"`
	MutationThreshold *float64 `json:"mutation_threshold,omitempty"`
}

// QualityGate is one resolved quality bar — armed or not and at what
// percentage; threshold 0 means no number was set.
type QualityGate struct {
	Enabled   bool
	Threshold float64
}

// EffectiveCoverageGate resolves the whole-repo line-coverage bar for one
// scope: the sub-project when it overrides it, otherwise the repository's own.
// "" asks for the repository itself.
func (r Repository) EffectiveCoverageGate(subProjectPath string) QualityGate {
	gate := QualityGate{Enabled: r.RequireOverallCoverage, Threshold: r.CoverageThreshold}
	if sp, ok := r.subProject(subProjectPath); ok {
		if sp.CoverageEnabled != nil {
			gate.Enabled = *sp.CoverageEnabled
		}
		if sp.CoverageThreshold != nil {
			gate.Threshold = *sp.CoverageThreshold
		}
	}
	return gate
}

// EffectiveMutationGate is EffectiveCoverageGate for the mutation score.
func (r Repository) EffectiveMutationGate(subProjectPath string) QualityGate {
	gate := QualityGate{Enabled: r.MutationEnabled, Threshold: r.MutationThreshold}
	if sp, ok := r.subProject(subProjectPath); ok {
		if sp.MutationEnabled != nil {
			gate.Enabled = *sp.MutationEnabled
		}
		if sp.MutationThreshold != nil {
			gate.Threshold = *sp.MutationThreshold
		}
	}
	return gate
}

func (r Repository) subProject(path string) (RepoSubProject, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return RepoSubProject{}, false
	}
	for _, sp := range r.SubProjects {
		if sp.Path == path {
			return sp, true
		}
	}
	return RepoSubProject{}, false
}

// RepositoryDocs names the reference docs agents should read before touching a
// repository (or one of its sub-projects); each field is a scoped-relative
// path, "" meaning not set.
type RepositoryDocs struct {
	CodingStandards string `json:"coding_standards,omitempty"`
	TestStandards   string `json:"test_standards,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	LocalRun        string `json:"local_run,omitempty"`
}

const (
	RepoDocCodingStandards = "coding_standards"
	RepoDocTestStandards   = "test_standards"
	RepoDocArchitecture    = "architecture"
	RepoDocLocalRun        = "local_run"
)

// ValidRepoDocKind reports whether k is a known reference-doc kind.
func ValidRepoDocKind(k string) bool {
	switch k {
	case RepoDocCodingStandards, RepoDocTestStandards, RepoDocArchitecture, RepoDocLocalRun:
		return true
	}
	return false
}

// DefaultRepoDocPath is where a generated doc of this kind lands absent an
// explicit path — the .ai/ convention. local_run is deliberately the bootstrap
// script, not a document: what a fresh machine needs is a thing to run.
func DefaultRepoDocPath(kind string) string {
	switch kind {
	case RepoDocCodingStandards:
		return ".ai/coding-standards.md"
	case RepoDocTestStandards:
		return ".ai/test-standards.md"
	case RepoDocArchitecture:
		return ".ai/architecture.md"
	case RepoDocLocalRun:
		return "scripts/dev.sh"
	default:
		return ""
	}
}

// maxSubProjects bounds a PATCH's sub_projects list so a hostile or buggy
// client cannot grow the column without limit.
const maxSubProjects = 200

// ValidateSubProjects cleans and validates a client-supplied list: trims
// whitespace, rejects empty/absolute/.. paths, duplicates, invalid kinds, a
// platform on a non-mobile sub-project, and thresholds outside 0-100.
func ValidateSubProjects(in []RepoSubProject) ([]RepoSubProject, error) {
	if len(in) > maxSubProjects {
		return nil, fmt.Errorf("too many sub-projects: %d (max %d)", len(in), maxSubProjects)
	}
	seen := make(map[string]bool, len(in))
	out := make([]RepoSubProject, 0, len(in))
	for _, sp := range in {
		path := strings.TrimSpace(sp.Path)
		kind := strings.TrimSpace(sp.Kind)
		if path == "" {
			return nil, fmt.Errorf("sub-project path must not be empty")
		}
		if filepath.IsAbs(path) {
			return nil, fmt.Errorf("sub-project path must be relative: %s", path)
		}
		for _, seg := range strings.Split(path, "/") {
			if seg == ".." {
				return nil, fmt.Errorf("sub-project path must not contain '..': %s", path)
			}
		}
		if seen[path] {
			return nil, fmt.Errorf("duplicate sub-project path: %s", path)
		}
		if !ValidSubRepoKind(kind) {
			return nil, fmt.Errorf("invalid sub-project kind: %s", kind)
		}
		platform := strings.TrimSpace(sp.MobilePlatform)
		if err := validateMobilePlatform(platform, kind); err != nil {
			return nil, fmt.Errorf("sub-project %s: %w", path, err)
		}
		for label, value := range map[string]*float64{
			"coverage_threshold": sp.CoverageThreshold,
			"mutation_threshold": sp.MutationThreshold,
		} {
			if value != nil && (*value < 0 || *value > 100) {
				return nil, fmt.Errorf("sub-project %s: %s must be between 0 and 100, got %.1f", path, label, *value)
			}
		}
		// Detections are carried through, not validated: they are a detection
		// result the client is echoing, and are dropped only on a sub-project
		// that is no longer mobile.
		identity := sp.DetectedAppIdentity
		targets := sp.DetectedBuildTargets
		if kind != RepoKindMobile {
			identity = AppIdentity{}
			targets = BuildTargets{}
		}
		seen[path] = true
		out = append(out, RepoSubProject{
			Path:                 path,
			Kind:                 kind,
			MobilePlatform:       platform,
			DetectedAppIdentity:  identity,
			DetectedBuildTargets: targets,
			Docs:                 sp.Docs,
			CoverageEnabled:      sp.CoverageEnabled,
			CoverageThreshold:    sp.CoverageThreshold,
			MutationEnabled:      sp.MutationEnabled,
			MutationThreshold:    sp.MutationThreshold,
		})
	}
	return out, nil
}

// validateMobilePlatform is the one rule both the repository and its
// sub-projects are held to: a platform must be a known one and sit on something
// mobile. "" always passes — it is the unset value.
func validateMobilePlatform(platform, kind string) error {
	if platform == "" {
		return nil
	}
	if !ValidMobilePlatform(platform) {
		return fmt.Errorf("invalid mobile platform: %s", platform)
	}
	if kind != RepoKindMobile {
		return fmt.Errorf("mobile_platform is only meaningful on a %s project, not %s", RepoKindMobile, kind)
	}
	return nil
}

// ValidateMobilePlatform is validateMobilePlatform for a repository-level value.
func ValidateMobilePlatform(platform, kind string) error {
	return validateMobilePlatform(strings.TrimSpace(platform), strings.TrimSpace(kind))
}

// validateReleaseEngine mirrors validateMobilePlatform's shape but not its unset
// rule: "" is a legal answer here, reading as ReleaseEngineAuto (the column's
// default), not as "nothing stated".
func validateReleaseEngine(engine, kind string) error {
	if engine == "" {
		return nil
	}
	if !ValidReleaseEngine(engine) {
		return fmt.Errorf("invalid release engine: %s", engine)
	}
	if engine != ReleaseEngineAuto && kind != RepoKindMobile {
		return fmt.Errorf("release_engine is only meaningful on a %s project, not %s", RepoKindMobile, kind)
	}
	return nil
}

// ValidateReleaseEngine is validateReleaseEngine for a repository-level value.
func ValidateReleaseEngine(engine, kind string) error {
	return validateReleaseEngine(strings.TrimSpace(engine), strings.TrimSpace(kind))
}

// Pipeline job mapping categories.
const (
	PipelineCategoryValidate     = "validate"
	PipelineCategoryBuild        = "build"
	PipelineCategoryTest         = "test"
	PipelineCategoryMutationTest = "mutation_test"
	// PipelineCategoryPROpen names the workflow that opens a PR once CI is
	// green: never dispatched/gated because agent task branches (tt-123) do not
	// match its feature/** trigger — the board opens their PR itself.
	PipelineCategoryPROpen        = "pr_open"
	PipelineCategoryStageDeploy   = "stage_deploy"
	PipelineCategoryPreProdDeploy = "preprod_deploy"
	PipelineCategoryProdDeploy    = "prod_deploy"
)

const (
	PipelineTargetJob      = "job"      // validate/build/test/mutation_test — a run job whose conclusion is read
	PipelineTargetWorkflow = "workflow" // pr_open/stage_deploy/prod_deploy — a workflow file
)

// RepositoryPipelineJob maps one (sub-repo, category) slot to a GitHub Actions
// job (status-read) or workflow file (dispatch).
type RepositoryPipelineJob struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	// SubProjectPath is what disambiguates two sub-projects that share a kind;
	// "" = the repository itself or a non-monorepo repo.
	SubProjectPath string `json:"sub_project_path,omitempty"`
	SubRepoKind    string `json:"sub_repo_kind"`
	Category       string `json:"category"`
	TargetKind     string `json:"target_kind"`
	TargetRef      string `json:"target_ref"`
	AutoDetected   bool   `json:"auto_detected"`
}

type OpenRepositoryRequest struct {
	RootPath    string      `json:"root_path"`
	Description string      `json:"description"`
	ProjectIDs  []uuid.UUID `json:"project_ids,omitempty"`
	// Owner: klasör git'siz ise GitHub reposunun açılacağı hesap/org ("" = token sahibi).
	Owner string `json:"owner,omitempty"`
	// CloneURL: bilinen origin adresidir; boşsa çalışma kopyasının origin'inden
	// okunur ve kalıcı saklanır ki kopya kaybolursa repo geri çekilebilsin.
	CloneURL string `json:"clone_url,omitempty"`
	// Kind: boşsa disktekinden otomatik tespit edilir.
	Kind string `json:"kind,omitempty"`
}

type CreateRepositoryRequest struct {
	Name        string      `json:"name"`
	ParentDir   string      `json:"parent_dir"`
	Description string      `json:"description"`
	ProjectIDs  []uuid.UUID `json:"project_ids,omitempty"`
	// Owner: yeni GitHub reposunun açılacağı hesap/org ("" = token sahibi).
	Owner string `json:"owner,omitempty"`
	// Kind: empty means auto-detect.
	Kind string `json:"kind,omitempty"`
}

// ImportGitHubRepositoryRequest, mevcut bir GitHub reposunu çalışma alanına
// klonlayıp kod deposu olarak kaydeder.
type ImportGitHubRepositoryRequest struct {
	Owner       string      `json:"owner"`
	Name        string      `json:"name"`
	CloneURL    string      `json:"clone_url,omitempty"`
	Description string      `json:"description"`
	ProjectIDs  []uuid.UUID `json:"project_ids,omitempty"`
	// Kind: empty means auto-detect from the cloned working copy.
	Kind string `json:"kind,omitempty"`
}

type UpdateRepositoryRequest struct {
	Name           string  `json:"name,omitempty"`
	Description    string  `json:"description,omitempty"`
	VerifyCommand  *string `json:"verify_command,omitempty"`
	BuildCommand   *string `json:"build_command,omitempty"`
	TestCommand    *string `json:"test_command,omitempty"`
	Kind           *string `json:"kind,omitempty"`
	MobilePlatform *string `json:"mobile_platform,omitempty"`
	// ReleaseEngine picks where mobile releases are built; a non-nil empty
	// string legally means ReleaseEngineAuto, the column's default.
	ReleaseEngine      *string           `json:"release_engine,omitempty"`
	MutationEnabled    *bool             `json:"mutation_enabled,omitempty"`
	MutationThreshold  *float64          `json:"mutation_threshold,omitempty"`
	SubRepoKinds       *[]string         `json:"sub_repo_kinds,omitempty"`
	SubProjects        *[]RepoSubProject `json:"sub_projects,omitempty"`
	AutoReleaseOnDone  *bool             `json:"auto_release_on_done,omitempty"`
	RequireHumanReview *bool             `json:"require_human_review,omitempty"`
	IncidentPolicy     *IncidentPolicy   `json:"incident_policy,omitempty"`
	TestStrategy       *string           `json:"test_strategy,omitempty"`
	// Docs replaces the repository's own reference-doc pointers wholesale when
	// set; nil leaves them untouched.
	Docs *RepositoryDocs `json:"docs,omitempty"`
}

// SavePipelineJobsRequest replaces the full category→target mapping for a repo.
type SavePipelineJobsRequest struct {
	Jobs []RepositoryPipelineJob `json:"jobs"`
}

// TaskGitInfo is the GitHub coordinates of a task workspace's branch, used to
// locate its GitHub Actions runs and dispatch deploy workflows.
type TaskGitInfo struct {
	Owner   string
	Repo    string
	Branch  string
	HeadSHA string
}

type SetRepositoryProjectsRequest struct {
	ProjectIDs []uuid.UUID `json:"project_ids"`
}
