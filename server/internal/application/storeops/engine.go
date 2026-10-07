package storeops

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const auditActionStoreBuild = "store_build"

var ErrInvalidEngine = errors.New("storeops: unknown release engine")

var ErrEngineUnavailable = errors.New("storeops: the pinned release engine is not available")

var ErrActionsNoWorkflow = errors.New("storeops: the repository has no mobile release workflow")

var ErrActionsBillingBlocked = errors.New("storeops: GitHub Actions is blocked for billing or quota reasons")

var ErrBuildTargetUnknown = errors.New("storeops: the repository does not state what a release build should archive")

type ActionsProbe func(ctx context.Context, repo domain.Repository, platform, workflowFile string) error

type LocalRunnerHost struct {
	Paired bool

	MacOS bool
}

type LocalRunnerProbe func(ctx context.Context) (LocalRunnerHost, error)

// ReleaseStarter starts a release build. buildNumber is the number the build
// must carry; "" leaves it to the engine (the workflow's own run number).
type ReleaseStarter func(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, engine string, artifacts []pipeline.Artifact, buildNumber string) error

type ReleaseParker interface {
	BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error)
}

func (s *Service) SetEngineProbes(actions ActionsProbe, local LocalRunnerProbe) {
	s.actionsProbe = actions
	s.localProbe = local
}

func (s *Service) SetReleaseStarter(start ReleaseStarter) { s.startRelease = start }

func (s *Service) SetReleaseParker(parker ReleaseParker) { s.parker = parker }

func (s *Service) ResolveEngine(ctx context.Context, repo domain.Repository, platform, workflowFile string) (string, error) {
	if !validPlatform(platform) {
		return "", fmt.Errorf("storeops: resolving release engine: unsupported platform %q: %w", platform, ErrInvalidPlatform)
	}
	engine := strings.TrimSpace(repo.ReleaseEngine)
	if engine == "" {
		engine = domain.ReleaseEngineAuto
	}
	if !domain.ValidReleaseEngine(engine) {
		return "", fmt.Errorf("storeops: %q: %w", engine, ErrInvalidEngine)
	}

	switch engine {
	case domain.ReleaseEngineActions:
		if err := s.actionsReady(ctx, repo, platform, workflowFile); err != nil {

			if !errors.Is(err, ErrActionsNoWorkflow) {
				return "", err
			}
		}
		return domain.ReleaseEngineActions, nil
	case domain.ReleaseEngineLocal:
		ok, err := s.localReady(ctx, platform)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf(
				"storeops: this repository pins the local runner and no paired host can build %s: %w", platform, ErrEngineUnavailable)
		}
		return domain.ReleaseEngineLocal, nil
	}

	actionsErr := s.actionsReady(ctx, repo, platform, workflowFile)
	switch {
	case actionsErr == nil:
		return domain.ReleaseEngineActions, nil
	case !errors.Is(actionsErr, ErrEngineUnavailable):

		return "", actionsErr
	}
	ok, err := s.localReady(ctx, platform)
	if err != nil {
		return "", err
	}
	if ok {
		return domain.ReleaseEngineLocal, nil
	}
	if errors.Is(actionsErr, ErrActionsNoWorkflow) {

		return domain.ReleaseEngineActions, nil
	}
	return "", domain.ErrNoReleaseEngine
}

func (s *Service) actionsReady(ctx context.Context, repo domain.Repository, platform, workflowFile string) error {
	if s.actionsProbe == nil {
		return fmt.Errorf("storeops: no GitHub Actions probe is wired: %w", ErrEngineUnavailable)
	}
	err := s.actionsProbe(ctx, repo, platform, workflowFile)
	if err == nil {
		return nil
	}
	if actionsUnavailable(err) {

		return fmt.Errorf("storeops: GitHub Actions cannot run this release (%w): %w", err, ErrEngineUnavailable)
	}
	return fmt.Errorf("storeops: checking GitHub Actions availability: %w", err)
}

func (s *Service) localReady(ctx context.Context, platform string) (bool, error) {
	if s.localProbe == nil {
		return false, nil
	}
	host, err := s.localProbe(ctx)
	if err != nil {
		return false, fmt.Errorf("storeops: checking the paired local runner: %w", err)
	}
	if !host.Paired {
		return false, nil
	}
	if platform == domain.MobileStorePlatformIOS {
		return host.MacOS, nil
	}
	return true, nil
}

func actionsUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrActionsNoWorkflow) || errors.Is(err, ErrActionsBillingBlocked) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "github api: 402") {
		return true
	}
	for _, needle := range []string{
		"billing", "spending limit", "quota", "payment",
		"actions is disabled", "actions are disabled", "upgrade",
		"has been disabled", "not allowed to run",
		"no workflow", "workflow not found",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

type BuildStart struct {
	Platform string `json:"platform"`
	Engine   string `json:"engine"`

	Artifacts []string `json:"artifacts"`
}

const workflowDirPrefix = ".github/workflows/"

func workflowFileName(artifacts []pipeline.Artifact) string {
	for _, artifact := range artifacts {
		if strings.HasPrefix(artifact.Path, workflowDirPrefix) {
			return path.Base(artifact.Path)
		}
	}
	return ""
}

func (s *Service) StartBuild(ctx context.Context, repositoryID uuid.UUID, platform, engine, actor string) (BuildStart, error) {

	if !validPlatform(platform) {
		err := fmt.Errorf("storeops: starting a release build: unsupported platform %q: %w", platform, ErrInvalidPlatform)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, nil, err)
		return BuildStart{}, err
	}
	engine = strings.TrimSpace(engine)
	if engine != "" && !domain.ValidReleaseEngine(engine) {
		err := fmt.Errorf("storeops: %q: %w", engine, ErrInvalidEngine)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, err)
		return BuildStart{}, err
	}

	repo, app, err := s.loadRepoAndApp(ctx, repositoryID, platform)
	if err != nil {
		return BuildStart{}, err
	}
	if app.Identifier == "" {
		notReady := fmt.Errorf("storeops: no store app is linked for %s: %w", platform, ErrAppNotReady)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, nil, notReady)
		return BuildStart{}, notReady
	}
	if engine != "" {

		if err := domain.ValidateReleaseEngine(engine, repo.Kind); err != nil {
			return BuildStart{}, fmt.Errorf("%v: %w", err, ErrInvalidEngine)
		}
		repo.ReleaseEngine = engine
	}

	spec := s.buildSpec(repo, app)
	if remedy := buildTargetRemedy(spec); remedy != "" {
		unknown := fmt.Errorf("%w (%s): %s", ErrBuildTargetUnknown, app.Identifier, remedy)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, unknown)
		return BuildStart{}, unknown
	}
	artifacts, err := pipeline.Render(spec)
	if err != nil {
		wrapped := fmt.Errorf("storeops: generating the release pipeline for %s: %w", app.Identifier, err)
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, wrapped)
		return BuildStart{}, wrapped
	}

	resolved, err := s.ResolveEngine(ctx, repo, platform, workflowFileName(artifacts))
	if err != nil {
		if errors.Is(err, domain.ErrNoReleaseEngine) {
			err = s.parkNoEngine(ctx, repo, app, err)
		}
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": engine}, err)
		return BuildStart{}, err
	}

	if s.startRelease == nil {

		wrapped := s.parkNoEngine(ctx, repo, app, fmt.Errorf(
			"storeops: no release starter is wired for the %s engine: %w", resolved, domain.ErrNoReleaseEngine))
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": resolved}, wrapped)
		return BuildStart{}, wrapped
	}
	buildNumber := ""
	if resolved == domain.ReleaseEngineActions {
		buildNumber, err = s.reserveReleaseBuild(ctx, repo, app, resolved, actor)
		if err != nil {
			s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": resolved}, err)
			return BuildStart{}, err
		}
	}
	if err := s.startRelease(ctx, repo, app, resolved, artifacts, buildNumber); err != nil {
		wrapped := fmt.Errorf("storeops: starting the %s release of %s: %w", resolved, app.Identifier, err)
		if errors.Is(err, domain.ErrNoReleaseEngine) {

			wrapped = s.parkNoEngine(ctx, repo, app, wrapped)
		}
		s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": resolved}, wrapped)
		return BuildStart{}, wrapped
	}

	paths := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		paths = append(paths, artifact.Path)
	}
	s.recordAudit(ctx, repositoryID, auditActionStoreBuild, platform, actor, map[string]string{"engine": resolved}, nil)
	return BuildStart{Platform: platform, Engine: resolved, Artifacts: paths}, nil
}

func (s *Service) buildSpec(repo domain.Repository, app domain.MobileStoreApp) pipeline.Spec {
	spec := pipeline.Spec{
		Platform:   app.Platform,
		Identifier: app.Identifier,
		AppName:    app.AppName,
		StoreAppID: app.StoreAppID,
	}
	if spec.AppName == "" {
		spec.AppName = repo.Name
	}

	targets := repo.DetectedBuildTargets
	for _, sub := range repo.SubProjects {
		if sub.Kind != domain.RepoKindMobile {
			continue
		}
		identity := sub.DetectedAppIdentity
		if identity.BundleID == app.Identifier || identity.PackageName == app.Identifier {
			spec.SubProjectPath, targets = sub.Path, sub.DetectedBuildTargets
			break
		}
		if spec.SubProjectPath == "" && subProjectCovers(sub.MobilePlatform, app.Platform) {
			spec.SubProjectPath, targets = sub.Path, sub.DetectedBuildTargets
		}
	}

	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		spec.Scheme = targets.XcodeScheme
	case domain.MobileStorePlatformAndroid:
		spec.Module = targets.GradleModule
	}
	return spec
}

func buildTargetRemedy(spec pipeline.Spec) string {
	switch spec.Platform {
	case domain.MobileStorePlatformIOS:
		if strings.TrimSpace(spec.Scheme) == "" {
			return prompt.Text(buildTargetIOSSchemeKey)
		}
	case domain.MobileStorePlatformAndroid:
		if strings.TrimSpace(spec.Module) == "" {
			return prompt.Text(buildTargetAndroidModuleKey)
		}
	}
	return ""
}

func subProjectCovers(mobilePlatform, storePlatform string) bool {
	switch mobilePlatform {
	case domain.MobilePlatformCross:
		return true
	case domain.MobilePlatformIOS:
		return storePlatform == domain.MobileStorePlatformIOS
	case domain.MobilePlatformAndroid:
		return storePlatform == domain.MobileStorePlatformAndroid
	}
	return false
}

func blockedReleaseRemedy() string { return prompt.Text(blockedReleaseRemedyKey) }

func (s *Service) parkNoEngine(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, cause error) error {
	detail := cause.Error() + " (" + app.Platform + ")"

	taskID, err := s.blockedReleaseTask(ctx, repo, app, detail)
	if err != nil {
		return fmt.Errorf("%w; no board card records it: %v", cause, err)
	}
	if s.parker == nil {
		return fmt.Errorf("%w; no board card records it: no release parker is wired", cause)
	}
	if _, err := s.parker.BlockOnResource(ctx, repo.ID, taskID, domain.ResourceHumanDecision, detail); err != nil {
		return fmt.Errorf("%w; parking the board card on human_decision failed: %v", cause, err)
	}
	if s.comments != nil {

		if _, err := s.comments.AddComment(ctx, repo.ID, taskID, domain.CreateTaskCommentRequest{
			Content:    "Release blocked: " + detail + "\n\n" + blockedReleaseRemedy(),
			AuthorType: "system",
		}); err != nil {
			log.Warn().Err(err).Str("repository_id", repo.ID.String()).
				Msg("storeops: commenting the release block failed")
		}
	}
	log.Warn().Err(cause).Str("repository_id", repo.ID.String()).Str("platform", app.Platform).
		Msg("release blocked: no release engine available")
	return cause
}

func (s *Service) blockedReleaseTask(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, detail string) (uuid.UUID, error) {
	if app.OnboardingTaskID != nil {
		return *app.OnboardingTaskID, nil
	}
	if s.tasks == nil {
		return uuid.Nil, errors.New("no board task creator is wired")
	}
	task, err := s.tasks.CreateTask(ctx, repo.ID, domain.CreateBoardTaskRequest{
		Title:       fmt.Sprintf("Release blocked: %s (%s)", app.Identifier, app.Platform),
		Description: detail + "\n\n" + blockedReleaseRemedy(),
		Priority:    domain.TaskPriorityHigh,
		Column:      domain.TaskColumnTodo,
		CreatedBy:   "system",
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("opening the board card failed: %w", err)
	}

	if err := s.rememberBlockedReleaseTask(ctx, app, task.ID); err != nil {
		log.Warn().Err(err).Str("repository_id", repo.ID.String()).
			Msg("storeops: recording the release block card on the store app row failed")
	}
	return task.ID, nil
}

func (s *Service) rememberBlockedReleaseTask(ctx context.Context, app domain.MobileStoreApp, taskID uuid.UUID) error {
	current, err := s.apps.Get(ctx, app.RepositoryID, app.Platform)
	if err != nil {
		return err
	}
	if current.OnboardingTaskID != nil {
		return nil
	}
	current.OnboardingTaskID = &taskID
	_, err = s.apps.Upsert(ctx, current)
	return err
}
