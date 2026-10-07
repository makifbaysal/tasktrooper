package storeops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops/pipeline"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	auditActionTestBuild     = "store_test_build"
	auditActionTestBuildOpen = "store_test_build_open"

	testBuildRunTimeout       = 3 * time.Hour
	testFlightProcessingLimit = 90 * time.Minute
	defaultTestBuildPoll      = 30 * time.Second
	testBuildLogTailLines     = 120
	testBuildFailureLimit     = 4000
	whatToTestLocale          = "en-US"
)

// TestBuildCheckout is a commit checked out on this machine for one build.
type TestBuildCheckout struct {
	Dir     string
	SHA     string
	Branch  string
	Cleanup func()
}

// TestBuildSource says which commit a task's test build is built from and
// makes it available to each engine.
type TestBuildSource interface {
	// Head is the commit a build of task would build right now; task nil is
	// the repository's default branch.
	Head(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (sha, branch string, err error)
	// Checkout puts that commit in a directory of its own, apart from the
	// task's workspace, so a build never writes into a checkout an agent uses.
	Checkout(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (TestBuildCheckout, error)
	// Publish pushes that commit for an engine that builds somewhere else.
	Publish(ctx context.Context, repo domain.Repository, task *domain.BoardTask) (sha, branch string, err error)
}

type TaskReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

type TestBuildDeps struct {
	Builds         port.StoreTestBuildStore
	NewTestFlight  func(domain.StoreCredential) (port.TestFlightClient, error)
	NewPlayTesting func(domain.StoreCredential) (port.PlayTestingClient, error)
	Local          port.LocalMobileBuilder
	Actions        port.MobileBuilder
	Source         TestBuildSource
	Tasks          TaskReader
	// ArtifactDir keeps each Android build's signed bundle, so the binary a
	// person tried is the one later released to a Play track.
	ArtifactDir  string
	PollInterval time.Duration
}

type testBuilds struct {
	TestBuildDeps
	poll time.Duration

	allocMu sync.Mutex
	// lanes serialise builds per engine and platform: two xcodebuild archives
	// on one Mac fight over DerivedData and the keychain search list.
	lanesMu sync.Mutex
	lanes   map[string]chan struct{}

	wg sync.WaitGroup
}

var ErrTestBuildsNotConfigured = errors.New("storeops: test builds are not configured on this server")

func (s *Service) SetTestBuilds(d TestBuildDeps) {
	poll := d.PollInterval
	if poll <= 0 {
		poll = defaultTestBuildPoll
	}
	s.tb = &testBuilds{TestBuildDeps: d, poll: poll, lanes: map[string]chan struct{}{}}
}

// WaitTestBuilds blocks until every build goroutine has returned; tests and
// shutdown use it.
func (s *Service) WaitTestBuilds() {
	if s.tb != nil {
		s.tb.wg.Wait()
	}
}

func (s *Service) testBuildsReady() (*testBuilds, error) {
	if s.tb == nil || s.tb.Builds == nil {
		return nil, ErrTestBuildsNotConfigured
	}
	return s.tb, nil
}

// testableApp is a linked app the store can take a test build for.
func testableApp(app domain.MobileStoreApp) error {
	if app.Identifier == "" || (app.Platform == domain.MobileStorePlatformIOS && app.StoreAppID == "") {
		return domain.ErrTestBuildNoStoreApp
	}
	if app.State != domain.MobileStoreStateTestReady && app.State != domain.MobileStoreStateLive {
		return domain.ErrTestBuildAppNotTestable
	}
	return nil
}

// OnTaskEnteredTestStage is the board hook: a task entering a stage that
// carries store_test_build_on_enter. It never fails the move.
func (s *Service) OnTaskEnteredTestStage(ctx context.Context, repositoryID, taskID uuid.UUID) {
	if _, err := s.testBuildsReady(); err != nil {
		return
	}
	apps, err := s.apps.ListByRepository(ctx, repositoryID)
	if err != nil || len(apps) == 0 {
		return
	}
	if _, err := s.StartTestBuilds(ctx, repositoryID, &taskID, nil, domain.TestBuildTriggerHumanUAT, "system"); err != nil {
		log.Warn().Err(err).Str("repository_id", repositoryID.String()).Str("task_id", taskID.String()).
			Msg("storeops: starting the task's store test builds failed")
	}
}

// StartTestBuilds queues one build per platform (all testable linked apps
// when platforms is empty) and returns at once; each build runs on its own.
func (s *Service) StartTestBuilds(ctx context.Context, repositoryID uuid.UUID, taskID *uuid.UUID, platforms []string, trigger, actor string) ([]domain.StoreTestBuild, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return nil, err
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("storeops: loading repository: %w", err)
	}
	var task *domain.BoardTask
	if taskID != nil {
		if tb.Tasks == nil {
			return nil, errors.New("storeops: no task reader is wired")
		}
		t, err := tb.Tasks.GetTask(ctx, repositoryID, *taskID)
		if err != nil {
			return nil, fmt.Errorf("storeops: loading task: %w", err)
		}
		task = &t
	}

	apps, err := s.apps.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing store apps: %w", err)
	}
	explicit := len(platforms) > 0
	var started []domain.StoreTestBuild
	var errs []error
	for _, app := range apps {
		if explicit && !slices.Contains(platforms, app.Platform) {
			continue
		}
		if err := testableApp(app); err != nil {
			if explicit {
				errs = append(errs, fmt.Errorf("%s: %w", app.Platform, err))
			}
			continue
		}
		build, skip, err := s.queueTestBuild(ctx, tb, repo, app, task, trigger, actor)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", app.Platform, err))
		case skip:
		default:
			started = append(started, build)
		}
	}
	if explicit {
		for _, p := range platforms {
			if !slices.ContainsFunc(apps, func(a domain.MobileStoreApp) bool { return a.Platform == p }) {
				errs = append(errs, fmt.Errorf("%s: %w", p, domain.ErrTestBuildNoStoreApp))
			}
		}
	}
	return started, errors.Join(errs...)
}

func (s *Service) queueTestBuild(ctx context.Context, tb *testBuilds, repo domain.Repository, app domain.MobileStoreApp, task *domain.BoardTask, trigger, actor string) (domain.StoreTestBuild, bool, error) {
	var taskID *uuid.UUID
	if task != nil {
		taskID = &task.ID
	}
	if taskID != nil {
		prior, err := tb.Builds.List(ctx, repo.ID, app.Platform, taskID, 1)
		if err != nil {
			return domain.StoreTestBuild{}, false, fmt.Errorf("storeops: reading earlier builds: %w", err)
		}
		if len(prior) > 0 && !domain.TestBuildTerminal(prior[0].Status) && prior[0].Status != domain.TestBuildActionRequired {
			return prior[0], true, nil
		}
		// A task that comes back to UAT without a new commit already has its
		// build; only a person asking for one gets another.
		if len(prior) > 0 && trigger == domain.TestBuildTriggerHumanUAT && prior[0].Status == domain.TestBuildReady && tb.Source != nil {
			if sha, _, err := tb.Source.Head(ctx, repo, task); err == nil && sha != "" && sha == prior[0].CommitSHA {
				return prior[0], true, nil
			}
		}
	}

	build, err := s.allocateTestBuild(ctx, tb, repo, app, task, trigger, actor)
	if err != nil {
		return domain.StoreTestBuild{}, false, err
	}
	s.recordAudit(ctx, repo.ID, auditActionTestBuild, app.Platform, actor, map[string]string{"build_number": build.BuildNumber}, nil)

	tb.wg.Add(1)
	go func() {
		defer tb.wg.Done()
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), testBuildRunTimeout)
		defer cancel()
		s.runTestBuild(runCtx, tb, repo, app, task, build)
	}()
	return build, false, nil
}

// allocateTestBuild takes the next build number. The counter is read from the
// store as well as from this table: builds uploaded by anything else (a CI run,
// Xcode's own Organizer) move the store's high-water mark, and the store
// refuses anything at or below it.
func (s *Service) allocateTestBuild(ctx context.Context, tb *testBuilds, repo domain.Repository, app domain.MobileStoreApp, task *domain.BoardTask, trigger, actor string) (domain.StoreTestBuild, error) {
	tb.allocMu.Lock()
	defer tb.allocMu.Unlock()

	storeMax, err := s.storeBuildSequence(ctx, tb, app)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	localMax, err := tb.Builds.MaxSequence(ctx, repo.ID, app.Platform)
	if err != nil {
		return domain.StoreTestBuild{}, fmt.Errorf("storeops: reading the build counter: %w", err)
	}
	seq := max(storeMax, localMax) + 1
	if app.Platform == domain.MobileStorePlatformAndroid && seq > domain.MaxAndroidVersionCode {
		return domain.StoreTestBuild{}, fmt.Errorf("storeops: the next versionCode %d is above Play's ceiling %d", seq, domain.MaxAndroidVersionCode)
	}

	build := domain.StoreTestBuild{
		RepositoryID: repo.ID,
		Platform:     app.Platform,
		Sequence:     seq,
		Status:       domain.TestBuildQueued,
		Trigger:      trigger,
		CreatedBy:    actor,
	}
	if task != nil {
		attempt, err := tb.Builds.MaxAttempt(ctx, task.ID, app.Platform)
		if err != nil {
			return domain.StoreTestBuild{}, fmt.Errorf("storeops: reading the task's earlier builds: %w", err)
		}
		build.TaskID = &task.ID
		build.TaskKey = task.Key
		build.TaskNumber = task.TaskNumber
		build.Attempt = attempt + 1
	}
	build.BuildNumber = domain.TestBuildNumber(app.Platform, seq, build.TaskNumber, build.Attempt)
	build.Notes = testBuildNotes(build, task)

	created, err := tb.Builds.Create(ctx, build)
	if err != nil {
		return domain.StoreTestBuild{}, fmt.Errorf("storeops: recording the test build: %w", err)
	}
	return created, nil
}

// reserveReleaseBuild takes the next number for a release build, so release
// and test builds share one counter: a release numbered by the workflow's run
// count would sit below every test build TestFlight already holds, and be
// refused. "" when test builds are not configured on this server.
func (s *Service) reserveReleaseBuild(ctx context.Context, repo domain.Repository, app domain.MobileStoreApp, engine, actor string) (string, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return "", nil
	}
	build, err := s.allocateTestBuild(ctx, tb, repo, app, nil, domain.TestBuildTriggerRelease, actor)
	if err != nil {
		return "", err
	}
	now := time.Now()
	build.Status = domain.TestBuildDispatched
	build.Engine = engine
	build.FinishedAt = &now
	if _, err := tb.Builds.Update(ctx, build); err != nil {
		return "", fmt.Errorf("storeops: recording the release build number: %w", err)
	}
	return build.BuildNumber, nil
}

func (s *Service) storeBuildSequence(ctx context.Context, tb *testBuilds, app domain.MobileStoreApp) (int64, error) {
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.testFlight(ctx, tb)
		if err != nil {
			return 0, err
		}
		n, err := client.LatestBuildSequence(ctx, app.StoreAppID)
		if err != nil {
			return 0, fmt.Errorf("storeops: reading the highest TestFlight build number: %w", err)
		}
		return n, nil
	case domain.MobileStorePlatformAndroid:
		client, err := s.playTesting(ctx, tb)
		if err != nil {
			return 0, err
		}
		n, err := client.LatestVersionCode(ctx, app.Identifier)
		if err != nil {
			return 0, fmt.Errorf("storeops: reading the highest Play versionCode: %w", err)
		}
		return n, nil
	}
	return 0, ErrInvalidPlatform
}

func testBuildNotes(build domain.StoreTestBuild, task *domain.BoardTask) string {
	var b strings.Builder
	if label := build.Label(); label != "" {
		b.WriteString(label)
	}
	if task != nil && strings.TrimSpace(task.Title) != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimSpace(task.Title))
	}
	return b.String()
}

func (s *Service) testFlight(ctx context.Context, tb *testBuilds) (port.TestFlightClient, error) {
	if tb.NewTestFlight == nil {
		return nil, ErrTestBuildsNotConfigured
	}
	cred, err := s.credential(ctx, domain.StoreCredentialASC)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreCredentialUnavailable, err)
	}
	return tb.NewTestFlight(cred)
}

func (s *Service) playTesting(ctx context.Context, tb *testBuilds) (port.PlayTestingClient, error) {
	if tb.NewPlayTesting == nil {
		return nil, ErrTestBuildsNotConfigured
	}
	cred, err := s.credential(ctx, domain.StoreCredentialGooglePlay)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreCredentialUnavailable, err)
	}
	return tb.NewPlayTesting(cred)
}

// testBuildRun carries one build through its steps and owns its row.
type testBuildRun struct {
	s     *Service
	tb    *testBuilds
	repo  domain.Repository
	app   domain.MobileStoreApp
	task  *domain.BoardTask
	build domain.StoreTestBuild

	logMu sync.Mutex
	tail  []string
}

func (r *testBuildRun) logLine(line string) {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	r.tail = append(r.tail, line)
	if len(r.tail) > testBuildLogTailLines {
		r.tail = r.tail[len(r.tail)-testBuildLogTailLines:]
	}
}

func (r *testBuildRun) logTail() string {
	r.logMu.Lock()
	defer r.logMu.Unlock()
	return strings.Join(r.tail, "\n")
}

// save and comment outlive the run's own deadline: the write that records a
// timeout is made after the context that timed out.
func (r *testBuildRun) save(ctx context.Context) {
	r.build.LogTail = r.logTail()
	stored, err := r.tb.Builds.Update(context.WithoutCancel(ctx), r.build)
	if err != nil {
		log.Warn().Err(err).Str("build_id", r.build.ID.String()).Msg("storeops: persisting a test build failed")
		return
	}
	r.build = stored
}

func (r *testBuildRun) fail(ctx context.Context, err error) {
	now := time.Now()
	r.build.Status = domain.TestBuildFailed
	r.build.Failure = domain.TruncateHead(err.Error(), testBuildFailureLimit)
	r.build.FinishedAt = &now
	r.save(ctx)
	log.Warn().Err(err).Str("build_id", r.build.ID.String()).Str("platform", r.build.Platform).Msg("storeops: test build failed")
	r.comment(ctx, fmt.Sprintf("%s test build %s failed: %s", platformName(r.build.Platform), r.build.BuildNumber, firstLine(r.build.Failure)))
}

func (r *testBuildRun) comment(ctx context.Context, content string) {
	if r.task == nil || r.s.comments == nil {
		return
	}
	if _, err := r.s.comments.AddComment(context.WithoutCancel(ctx), r.repo.ID, r.task.ID, domain.CreateTaskCommentRequest{
		Content:    content,
		AuthorType: "system",
	}); err != nil {
		log.Warn().Err(err).Str("build_id", r.build.ID.String()).Msg("storeops: commenting a test build failed")
	}
}

func platformName(platform string) string {
	if platform == domain.MobileStorePlatformIOS {
		return "iOS"
	}
	return "Android"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func (s *Service) runTestBuild(ctx context.Context, tb *testBuilds, repo domain.Repository, app domain.MobileStoreApp, task *domain.BoardTask, build domain.StoreTestBuild) {
	r := &testBuildRun{s: s, tb: tb, repo: repo, app: app, task: task, build: build}
	defer func() {
		if p := recover(); p != nil {
			r.fail(ctx, fmt.Errorf("storeops: the test build panicked: %v", p))
		}
	}()

	engine, err := s.testBuildEngine(ctx, tb, repo, app.Platform)
	if err != nil {
		r.fail(ctx, err)
		return
	}
	release := tb.acquireLane(engine + "/" + app.Platform)
	defer release()

	r.build.Engine = engine
	r.build.Status = domain.TestBuildBuilding
	r.save(ctx)

	artifact, err := r.build_(ctx, engine)
	if err != nil {
		r.fail(ctx, err)
		return
	}

	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		r.build.Status = domain.TestBuildProcessing
		r.save(ctx)
		r.finishIOS(ctx)
	case domain.MobileStorePlatformAndroid:
		r.finishAndroid(ctx, artifact)
	}
}

func (tb *testBuilds) acquireLane(key string) func() {
	tb.lanesMu.Lock()
	lane, ok := tb.lanes[key]
	if !ok {
		lane = make(chan struct{}, 1)
		tb.lanes[key] = lane
	}
	tb.lanesMu.Unlock()
	lane <- struct{}{}
	return func() { <-lane }
}

// testBuildEngine prefers this machine: it costs nothing and needs no pushed
// secrets, and GitHub Actions is the fallback for a platform this machine
// cannot build (iOS anywhere but a Mac with Xcode). A repository that pins an
// engine gets that engine or an error, never the other one.
func (s *Service) testBuildEngine(ctx context.Context, tb *testBuilds, repo domain.Repository, platform string) (string, error) {
	localOK, reason := false, "no local builder is wired"
	if tb.Local != nil {
		localOK, reason = tb.Local.Available(ctx, platform)
	}
	switch strings.TrimSpace(repo.ReleaseEngine) {
	case domain.ReleaseEngineLocal:
		if !localOK {
			return "", fmt.Errorf("storeops: this repository builds on this machine only, and it cannot build %s: %s: %w", platform, reason, ErrEngineUnavailable)
		}
		return domain.ReleaseEngineLocal, nil
	case domain.ReleaseEngineActions:
		if tb.Actions == nil {
			return "", fmt.Errorf("storeops: this repository builds on GitHub Actions, which is not connected: %w", ErrEngineUnavailable)
		}
		return domain.ReleaseEngineActions, nil
	}
	if localOK {
		return domain.ReleaseEngineLocal, nil
	}
	if tb.Actions != nil {
		return domain.ReleaseEngineActions, nil
	}
	return "", fmt.Errorf("storeops: this machine cannot build %s (%s) and GitHub Actions is not connected: %w", platform, reason, domain.ErrNoReleaseEngine)
}

func (r *testBuildRun) secrets(ctx context.Context) (map[string]string, error) {
	switch r.app.Platform {
	case domain.MobileStorePlatformIOS:
		return r.s.EnsureIOSSigning(ctx, r.app.Identifier)
	case domain.MobileStorePlatformAndroid:
		return r.s.EnsureAndroidKeystore(ctx, r.app.Identifier)
	}
	return nil, ErrInvalidPlatform
}

// build_ runs the release script's stage channel and returns the signed
// artifact kept on this machine (Android only; TestFlight receives the iOS
// build from inside the script).
func (r *testBuildRun) build_(ctx context.Context, engine string) (string, error) {
	spec := r.s.buildSpec(r.repo, r.app)
	if remedy := buildTargetRemedy(spec); remedy != "" {
		return "", fmt.Errorf("%w (%s): %s", ErrBuildTargetUnknown, r.app.Identifier, remedy)
	}
	artifacts, err := pipeline.Render(spec)
	if err != nil {
		return "", fmt.Errorf("storeops: generating the release pipeline for %s: %w", r.app.Identifier, err)
	}
	secrets, err := r.secrets(ctx)
	if err != nil {
		return "", fmt.Errorf("storeops: preparing signing for %s: %w", r.app.Identifier, err)
	}
	env := map[string]string{"BUILD_NUMBER": r.build.BuildNumber}
	if r.app.Platform == domain.MobileStorePlatformAndroid {
		env["MOBILE_RELEASE_UPLOAD"] = "false"
	}

	req := port.MobileBuildRequest{
		BuildID:    r.build.ID,
		Platform:   r.app.Platform,
		Identifier: r.app.Identifier,
		Env:        env,
		Log:        r.logLine,
	}

	var result port.MobileBuildResult
	switch engine {
	case domain.ReleaseEngineLocal:
		if r.tb.Source == nil {
			return "", errors.New("storeops: no source checkout is wired for local builds")
		}
		checkout, err := r.tb.Source.Checkout(ctx, r.repo, r.task)
		if err != nil {
			return "", fmt.Errorf("storeops: checking out the commit to build: %w", err)
		}
		defer checkout.Cleanup()
		r.build.CommitSHA, r.build.Branch = checkout.SHA, checkout.Branch
		r.build.Notes = testBuildNotes(r.build, r.task)
		r.save(ctx)

		req.ProjectDir = filepath.Join(checkout.Dir, filepath.FromSlash(spec.SubProjectPath))
		req.Script = scriptBody(artifacts)
		req.Secrets = secrets
		result, err = r.tb.Local.Run(ctx, req)
		if err != nil {
			return "", err
		}
		// The checkout goes when this function returns; the bundle a person
		// will try is what a later track release must ship, so it is kept.
		return r.keepArtifact(result.Artifact)

	case domain.ReleaseEngineActions:
		if r.tb.Source == nil {
			return "", errors.New("storeops: no source is wired for GitHub Actions builds")
		}
		sha, branch, err := r.tb.Source.Publish(ctx, r.repo, r.task)
		if err != nil {
			return "", fmt.Errorf("storeops: pushing the commit to build: %w", err)
		}
		r.build.CommitSHA, r.build.Branch = sha, branch
		r.build.Notes = testBuildNotes(r.build, r.task)
		r.save(ctx)
		if err := r.s.pushSecrets(ctx, r.repo.ID, secrets); err != nil {
			return "", err
		}
		files := make([]port.PipelineFile, 0, len(artifacts))
		for _, a := range artifacts {
			files = append(files, port.PipelineFile{Path: a.Path, Body: a.Body, Mode: a.Mode})
		}
		req.Repository = r.repo
		req.Files = files
		req.Workflow = workflowFileName(artifacts)
		req.Ref = sha
		req.ArtifactDir = filepath.Join(r.tb.ArtifactDir, r.build.ID.String()+"-run")
		req.OnRunURL = func(url string) {
			r.build.RunURL = url
			r.save(ctx)
		}
		defer os.RemoveAll(req.ArtifactDir)
		result, err = r.tb.Actions.Run(ctx, req)
		if result.RunURL != "" {
			r.build.RunURL = result.RunURL
		}
		if err != nil {
			return "", err
		}
		return r.keepArtifact(result.Artifact)
	}
	return "", fmt.Errorf("storeops: %q: %w", engine, ErrInvalidEngine)
}

func scriptBody(artifacts []pipeline.Artifact) string {
	for _, a := range artifacts {
		if path.Ext(a.Path) == ".sh" {
			return a.Body
		}
	}
	return ""
}

func (r *testBuildRun) keepArtifact(produced string) (string, error) {
	if r.app.Platform != domain.MobileStorePlatformAndroid {
		return "", nil
	}
	if produced == "" {
		return "", errors.New("storeops: the build finished without producing a signed .aab")
	}
	if r.tb.ArtifactDir == "" {
		return "", errors.New("storeops: no directory is configured to keep test builds in")
	}
	if err := os.MkdirAll(r.tb.ArtifactDir, 0o700); err != nil {
		return "", fmt.Errorf("storeops: creating the test build directory: %w", err)
	}
	dest := filepath.Join(r.tb.ArtifactDir, r.build.ID.String()+".aab")
	if err := copyFile(produced, dest); err != nil {
		return "", fmt.Errorf("storeops: keeping the signed bundle: %w", err)
	}
	r.build.ArtifactPath = dest
	return dest, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// finishIOS waits for App Store Connect to process the upload, then opens the
// build to its groups. It is also how a restarted server picks a processing
// build back up.
func (r *testBuildRun) finishIOS(ctx context.Context) {
	client, err := r.s.testFlight(ctx, r.tb)
	if err != nil {
		r.fail(ctx, err)
		return
	}
	deadline := time.Now().Add(testFlightProcessingLimit)
	for {
		found, ok, err := client.FindBuild(ctx, r.app.StoreAppID, r.build.BuildNumber)
		if err != nil {
			r.logLine("App Store Connect lookup failed: " + err.Error())
		}
		if ok {
			r.build.StoreBuildID = found.ID
			if found.MarketingVersion != "" {
				r.build.VersionName = found.MarketingVersion
			}
			switch found.ProcessingState {
			case "VALID":
				r.afterProcessing(ctx, client, found)
				return
			case "FAILED", "INVALID":
				r.fail(ctx, fmt.Errorf("storeops: App Store Connect marked build %s %s", r.build.BuildNumber, found.ProcessingState))
				return
			}
			r.save(ctx)
		}
		if time.Now().After(deadline) {
			r.fail(ctx, fmt.Errorf("storeops: App Store Connect had not finished processing build %s after %s", r.build.BuildNumber, testFlightProcessingLimit))
			return
		}
		select {
		case <-ctx.Done():
			r.fail(ctx, fmt.Errorf("storeops: stopped waiting for App Store Connect: %w", ctx.Err()))
			return
		case <-time.After(r.tb.poll):
		}
	}
}

func (r *testBuildRun) afterProcessing(ctx context.Context, client port.TestFlightClient, found port.TestFlightBuild) {
	if r.build.Notes != "" {
		if err := client.SetWhatToTest(ctx, found.ID, whatToTestLocale, r.build.Notes); err != nil {
			r.logLine("setting What to Test failed: " + err.Error())
		}
	}
	// Export compliance is a legal statement about the app's cryptography;
	// it is the developer's to make, so the build waits for them.
	if found.UsesNonExemptEncryption == nil {
		r.build.Status = domain.TestBuildActionRequired
		r.build.Failure = testBuildNeedsCompliance
		r.save(ctx)
		r.comment(ctx, fmt.Sprintf("iOS test build %s is processed but waits for its export compliance answer before anyone can install it.", r.build.BuildNumber))
		return
	}
	r.distribute(ctx)
}

const testBuildNeedsCompliance = "export_compliance"

func (r *testBuildRun) finishAndroid(ctx context.Context, artifact string) {
	client, err := r.s.playTesting(ctx, r.tb)
	if err != nil {
		r.fail(ctx, err)
		return
	}
	link, err := client.UploadInternalSharing(ctx, r.app.Identifier, artifact)
	if err != nil {
		r.fail(ctx, fmt.Errorf("storeops: uploading to Play internal app sharing: %w", err))
		return
	}
	r.build.InstallURL = link.DownloadURL
	r.save(ctx)
	r.distribute(ctx)
}

// distribute opens a ready build to the repository's automatic groups. A
// failure here leaves the build ready — it is installable — and says what did
// not happen.
func (r *testBuildRun) distribute(ctx context.Context) {
	groups, err := r.s.autoGroups(ctx, r.tb, r.repo.ID, r.app)
	if err != nil {
		r.logLine("reading the automatic test groups failed: " + err.Error())
	}
	var openErr error
	if len(groups) > 0 {
		openErr = r.s.openBuild(ctx, r.tb, r.app, &r.build, groups)
	}
	if r.app.Platform == domain.MobileStorePlatformIOS {
		r.build.Groups = mergeGroups(r.build.Groups, r.s.allBuildsGroups(ctx, r.tb, r.app))
	}
	now := time.Now()
	r.build.Status = domain.TestBuildReady
	r.build.Failure = ""
	if openErr != nil {
		r.build.Failure = domain.TruncateHead(openErr.Error(), testBuildFailureLimit)
	}
	r.build.FinishedAt = &now
	r.save(ctx)

	switch r.app.Platform {
	case domain.MobileStorePlatformIOS:
		r.comment(ctx, fmt.Sprintf("iOS test build %s (%s) is on TestFlight.", r.build.BuildNumber, r.build.Label()))
	case domain.MobileStorePlatformAndroid:
		r.comment(ctx, fmt.Sprintf("Android test build %s (%s) is on Play internal app sharing: %s", r.build.BuildNumber, r.build.Label(), r.build.InstallURL))
	}
}

func mergeGroups(have, add []string) []string {
	out := append([]string{}, have...)
	for _, g := range add {
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// autoGroups are the groups a ready build opens to. Unset, iOS opens to every
// internal group that is not already given every build, and Android to none:
// the internal app sharing link is the Android build's own way in.
func (s *Service) autoGroups(ctx context.Context, tb *testBuilds, repositoryID uuid.UUID, app domain.MobileStoreApp) ([]string, error) {
	groups, set, err := tb.Builds.AutoGroups(ctx, repositoryID, app.Platform)
	if err != nil {
		return nil, err
	}
	if set || app.Platform != domain.MobileStorePlatformIOS {
		return groups, nil
	}
	client, err := s.testFlight(ctx, tb)
	if err != nil {
		return nil, err
	}
	all, err := client.BetaGroups(ctx, app.StoreAppID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, g := range all {
		if g.Internal && !g.AllBuilds {
			ids = append(ids, g.ID)
		}
	}
	return ids, nil
}

func (s *Service) allBuildsGroups(ctx context.Context, tb *testBuilds, app domain.MobileStoreApp) []string {
	client, err := s.testFlight(ctx, tb)
	if err != nil {
		return nil
	}
	all, err := client.BetaGroups(ctx, app.StoreAppID)
	if err != nil {
		return nil
	}
	var ids []string
	for _, g := range all {
		if g.Internal && g.AllBuilds {
			ids = append(ids, g.ID)
		}
	}
	return ids
}

// openBuild opens build to groups and records them on it. External TestFlight
// groups need Beta App Review, so the build is submitted once when one of them
// is in the list.
func (s *Service) openBuild(ctx context.Context, tb *testBuilds, app domain.MobileStoreApp, build *domain.StoreTestBuild, groups []string) error {
	switch app.Platform {
	case domain.MobileStorePlatformIOS:
		if build.StoreBuildID == "" {
			return domain.ErrTestBuildNotReady
		}
		client, err := s.testFlight(ctx, tb)
		if err != nil {
			return err
		}
		all, err := client.BetaGroups(ctx, app.StoreAppID)
		if err != nil {
			return fmt.Errorf("storeops: listing TestFlight groups: %w", err)
		}
		var add []string
		external := false
		for _, id := range groups {
			idx := slices.IndexFunc(all, func(g port.BetaGroup) bool { return g.ID == id })
			if idx < 0 {
				return fmt.Errorf("storeops: TestFlight has no group %s for this app", id)
			}
			if all[idx].AllBuilds {
				continue
			}
			if !all[idx].Internal {
				external = true
			}
			add = append(add, id)
		}
		if err := client.AddBuildToGroups(ctx, build.StoreBuildID, add); err != nil {
			return fmt.Errorf("storeops: opening build %s to its TestFlight groups: %w", build.BuildNumber, err)
		}
		if external {
			if err := client.SubmitForBetaReview(ctx, build.StoreBuildID); err != nil {
				return fmt.Errorf("storeops: submitting build %s to Beta App Review: %w", build.BuildNumber, err)
			}
		}
		build.Groups = mergeGroups(build.Groups, groups)
		return nil

	case domain.MobileStorePlatformAndroid:
		client, err := s.playTesting(ctx, tb)
		if err != nil {
			return err
		}
		versionCode, err := strconv.ParseInt(build.BuildNumber, 10, 64)
		if err != nil {
			return fmt.Errorf("storeops: build number %q is not a versionCode: %w", build.BuildNumber, err)
		}
		artifact := build.ArtifactPath
		if artifact != "" {
			if _, err := os.Stat(artifact); err != nil {
				artifact = ""
			}
		}
		name := build.BuildNumber
		if label := build.Label(); label != "" {
			name += " (" + label + ")"
		}
		for _, track := range groups {
			if track == androidTrackProduction {
				return fmt.Errorf("storeops: production is not a test track; promote through the release flow")
			}
			if err := client.ReleaseToTrack(ctx, app.Identifier, track, artifact, versionCode, name, build.Notes); err != nil {
				if artifact == "" {
					return fmt.Errorf("%w (%v)", domain.ErrTestBuildNoArtifact, err)
				}
				return fmt.Errorf("storeops: releasing versionCode %d to the %s track: %w", versionCode, track, err)
			}
			build.Groups = mergeGroups(build.Groups, []string{track})
		}
		return nil
	}
	return ErrInvalidPlatform
}

func (s *Service) loadTestBuild(ctx context.Context, tb *testBuilds, repositoryID, buildID uuid.UUID) (domain.StoreTestBuild, domain.MobileStoreApp, error) {
	build, err := tb.Builds.Get(ctx, buildID)
	if err != nil {
		return domain.StoreTestBuild{}, domain.MobileStoreApp{}, err
	}
	if build.RepositoryID != repositoryID {
		return domain.StoreTestBuild{}, domain.MobileStoreApp{}, domain.ErrTestBuildNotFound
	}
	app, err := s.apps.Get(ctx, repositoryID, build.Platform)
	if err != nil {
		return domain.StoreTestBuild{}, domain.MobileStoreApp{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	return build, app, nil
}

func (s *Service) TestBuilds(ctx context.Context, repositoryID uuid.UUID, platform string, taskID *uuid.UUID, limit int) ([]domain.StoreTestBuild, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return nil, err
	}
	if platform != "" && !validPlatform(platform) {
		return nil, ErrInvalidPlatform
	}
	return tb.Builds.List(ctx, repositoryID, platform, taskID, limit)
}

func (s *Service) TestBuild(ctx context.Context, repositoryID, buildID uuid.UUID) (domain.StoreTestBuild, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	build, _, err := s.loadTestBuild(ctx, tb, repositoryID, buildID)
	return build, err
}

// OpenTestBuild opens a ready build to the given groups (TestFlight group ids
// or Play track names).
func (s *Service) OpenTestBuild(ctx context.Context, repositoryID, buildID uuid.UUID, groups []string, actor string) (domain.StoreTestBuild, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	build, app, err := s.loadTestBuild(ctx, tb, repositoryID, buildID)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	if build.Status != domain.TestBuildReady {
		return domain.StoreTestBuild{}, domain.ErrTestBuildNotReady
	}
	err = s.openBuild(ctx, tb, app, &build, groups)
	s.recordAudit(ctx, repositoryID, auditActionTestBuildOpen, build.Platform, actor,
		map[string]string{"build_number": build.BuildNumber, "groups": strings.Join(groups, ",")}, err)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	return tb.Builds.Update(ctx, build)
}

// CloseTestBuild takes a build out of TestFlight groups. Play has no such
// step: a track serves one release until another replaces it.
func (s *Service) CloseTestBuild(ctx context.Context, repositoryID, buildID uuid.UUID, groups []string, actor string) (domain.StoreTestBuild, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	build, _, err := s.loadTestBuild(ctx, tb, repositoryID, buildID)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	if build.Platform != domain.MobileStorePlatformIOS {
		return domain.StoreTestBuild{}, domain.ErrTestBuildUnsupported
	}
	if build.StoreBuildID == "" {
		return domain.StoreTestBuild{}, domain.ErrTestBuildNotReady
	}
	client, err := s.testFlight(ctx, tb)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	err = client.RemoveBuildFromGroups(ctx, build.StoreBuildID, groups)
	s.recordAudit(ctx, repositoryID, auditActionTestBuildOpen, build.Platform, actor,
		map[string]string{"build_number": build.BuildNumber, "removed": strings.Join(groups, ",")}, err)
	if err != nil {
		return domain.StoreTestBuild{}, fmt.Errorf("storeops: removing build %s from TestFlight groups: %w", build.BuildNumber, err)
	}
	build.Groups = slices.DeleteFunc(build.Groups, func(g string) bool { return slices.Contains(groups, g) })
	return tb.Builds.Update(ctx, build)
}

// AnswerExportCompliance records the developer's export compliance answer and
// carries the build on to its groups.
func (s *Service) AnswerExportCompliance(ctx context.Context, repositoryID, buildID uuid.UUID, usesNonExemptEncryption bool, actor string) (domain.StoreTestBuild, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	build, app, err := s.loadTestBuild(ctx, tb, repositoryID, buildID)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	if build.Platform != domain.MobileStorePlatformIOS {
		return domain.StoreTestBuild{}, domain.ErrTestBuildUnsupported
	}
	if build.Status != domain.TestBuildActionRequired || build.StoreBuildID == "" {
		return domain.StoreTestBuild{}, domain.ErrTestBuildNotReady
	}
	client, err := s.testFlight(ctx, tb)
	if err != nil {
		return domain.StoreTestBuild{}, err
	}
	err = client.DeclareEncryption(ctx, build.StoreBuildID, usesNonExemptEncryption)
	s.recordAudit(ctx, repositoryID, auditActionTestBuildOpen, build.Platform, actor,
		map[string]string{"build_number": build.BuildNumber, "uses_non_exempt_encryption": strconv.FormatBool(usesNonExemptEncryption)}, err)
	if err != nil {
		return domain.StoreTestBuild{}, fmt.Errorf("storeops: answering export compliance for build %s: %w", build.BuildNumber, err)
	}
	r := &testBuildRun{s: s, tb: tb, repo: domain.Repository{ID: repositoryID}, app: app, build: build}
	if build.TaskID != nil && tb.Tasks != nil {
		if task, err := tb.Tasks.GetTask(ctx, repositoryID, *build.TaskID); err == nil {
			r.task = &task
		}
	}
	r.distribute(ctx)
	return r.build, nil
}

// TestGroups lists where a build can be opened: TestFlight groups, or the
// Play testing tracks (production excluded).
func (s *Service) TestGroups(ctx context.Context, repositoryID uuid.UUID, platform string) ([]domain.StoreTestGroup, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return nil, err
	}
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		return nil, fmt.Errorf("storeops: loading store app: %w", err)
	}
	if err := testableApp(app); err != nil {
		return nil, err
	}
	auto, set, err := tb.Builds.AutoGroups(ctx, repositoryID, platform)
	if err != nil {
		return nil, err
	}

	switch platform {
	case domain.MobileStorePlatformIOS:
		client, err := s.testFlight(ctx, tb)
		if err != nil {
			return nil, err
		}
		groups, err := client.BetaGroups(ctx, app.StoreAppID)
		if err != nil {
			return nil, fmt.Errorf("storeops: listing TestFlight groups: %w", err)
		}
		out := make([]domain.StoreTestGroup, 0, len(groups))
		for _, g := range groups {
			kind := "external"
			if g.Internal {
				kind = "internal"
			}
			autoOn := slices.Contains(auto, g.ID)
			if !set {
				autoOn = g.Internal
			}
			out = append(out, domain.StoreTestGroup{
				ID: g.ID, Name: g.Name, Platform: platform, Kind: kind, AllBuilds: g.AllBuilds,
				PublicLink: g.PublicLink, TesterCount: g.TesterCount, AutoDistribute: autoOn || g.AllBuilds,
			})
		}
		return out, nil

	case domain.MobileStorePlatformAndroid:
		client, err := s.playTesting(ctx, tb)
		if err != nil {
			return nil, err
		}
		tracks, err := client.ListTracks(ctx, app.Identifier)
		if err != nil {
			return nil, fmt.Errorf("storeops: listing Play tracks: %w", err)
		}
		out := make([]domain.StoreTestGroup, 0, len(tracks))
		for _, t := range tracks {
			if t.Name == androidTrackProduction {
				continue
			}
			out = append(out, domain.StoreTestGroup{
				ID: t.Name, Name: t.Name, Platform: platform, Kind: playTrackKind(t.Name),
				TesterCount: -1, AutoDistribute: slices.Contains(auto, t.Name), CurrentBuild: newestVersionCode(t),
			})
		}
		return out, nil
	}
	return nil, ErrInvalidPlatform
}

func playTrackKind(track string) string {
	switch track {
	case androidTrackInternal:
		return "internal"
	case "beta":
		return "open"
	}
	return "closed"
}

func newestVersionCode(t port.PlayTrack) string {
	var best int64
	for _, rel := range t.Releases {
		for _, vc := range rel.VersionCodes {
			best = max(best, vc)
		}
	}
	if best == 0 {
		return ""
	}
	return strconv.FormatInt(best, 10)
}

func (s *Service) SetTestAutoGroups(ctx context.Context, repositoryID uuid.UUID, platform string, groups []string) error {
	tb, err := s.testBuildsReady()
	if err != nil {
		return err
	}
	if !validPlatform(platform) {
		return ErrInvalidPlatform
	}
	if slices.Contains(groups, androidTrackProduction) {
		return fmt.Errorf("storeops: production is not a test track: %w", ErrInvalidPlatform)
	}
	return tb.Builds.SetAutoGroups(ctx, repositoryID, platform, groups)
}

func (s *Service) CreateTestGroup(ctx context.Context, repositoryID uuid.UUID, platform, name string, internal bool) (domain.StoreTestGroup, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return domain.StoreTestGroup{}, err
	}
	if platform != domain.MobileStorePlatformIOS {
		return domain.StoreTestGroup{}, domain.ErrTestBuildUnsupported
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.StoreTestGroup{}, errors.New("storeops: a TestFlight group needs a name")
	}
	app, err := s.apps.Get(ctx, repositoryID, platform)
	if err != nil {
		return domain.StoreTestGroup{}, fmt.Errorf("storeops: loading store app: %w", err)
	}
	client, err := s.testFlight(ctx, tb)
	if err != nil {
		return domain.StoreTestGroup{}, err
	}
	g, err := client.CreateBetaGroup(ctx, app.StoreAppID, name, internal)
	if err != nil {
		return domain.StoreTestGroup{}, fmt.Errorf("storeops: creating TestFlight group %q: %w", name, err)
	}
	kind := "external"
	if g.Internal {
		kind = "internal"
	}
	return domain.StoreTestGroup{ID: g.ID, Name: g.Name, Platform: platform, Kind: kind, PublicLink: g.PublicLink, TesterCount: 0}, nil
}

func (s *Service) TestGroupTesters(ctx context.Context, repositoryID uuid.UUID, groupID string) ([]domain.StoreTester, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return nil, err
	}
	client, err := s.testFlightForRepo(ctx, tb, repositoryID)
	if err != nil {
		return nil, err
	}
	testers, err := client.GroupTesters(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("storeops: listing the group's testers: %w", err)
	}
	out := make([]domain.StoreTester, 0, len(testers))
	for _, t := range testers {
		out = append(out, domain.StoreTester{ID: t.ID, Email: t.Email, FirstName: t.FirstName, LastName: t.LastName, State: t.State})
	}
	return out, nil
}

func (s *Service) AddTestGroupTester(ctx context.Context, repositoryID uuid.UUID, groupID, email, firstName, lastName string) (domain.StoreTester, error) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return domain.StoreTester{}, err
	}
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") {
		return domain.StoreTester{}, errors.New("storeops: a tester needs an email address")
	}
	client, err := s.testFlightForRepo(ctx, tb, repositoryID)
	if err != nil {
		return domain.StoreTester{}, err
	}
	t, err := client.AddTester(ctx, groupID, email, strings.TrimSpace(firstName), strings.TrimSpace(lastName))
	if err != nil {
		return domain.StoreTester{}, fmt.Errorf("storeops: inviting %s: %w", email, err)
	}
	return domain.StoreTester{ID: t.ID, Email: t.Email, FirstName: t.FirstName, LastName: t.LastName, State: t.State}, nil
}

func (s *Service) RemoveTestGroupTester(ctx context.Context, repositoryID uuid.UUID, groupID, testerID string) error {
	tb, err := s.testBuildsReady()
	if err != nil {
		return err
	}
	client, err := s.testFlightForRepo(ctx, tb, repositoryID)
	if err != nil {
		return err
	}
	if err := client.RemoveTester(ctx, groupID, testerID); err != nil {
		return fmt.Errorf("storeops: removing the tester: %w", err)
	}
	return nil
}

func (s *Service) testFlightForRepo(ctx context.Context, tb *testBuilds, repositoryID uuid.UUID) (port.TestFlightClient, error) {
	app, err := s.apps.Get(ctx, repositoryID, domain.MobileStorePlatformIOS)
	if err != nil {
		return nil, fmt.Errorf("storeops: loading store app: %w", err)
	}
	if err := testableApp(app); err != nil {
		return nil, err
	}
	return s.testFlight(ctx, tb)
}

// ResumeTestBuilds picks up what a previous process left. A build still being
// processed by App Store Connect is waited on again; one that was mid-build has
// lost its process and is failed, so a person can start it again.
func (s *Service) ResumeTestBuilds(ctx context.Context) {
	tb, err := s.testBuildsReady()
	if err != nil {
		return
	}
	builds, err := tb.Builds.ListUnfinished(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("storeops: listing unfinished test builds failed")
		return
	}
	for _, build := range builds {
		app, err := s.apps.Get(ctx, build.RepositoryID, build.Platform)
		if err != nil {
			continue
		}
		r := &testBuildRun{s: s, tb: tb, repo: domain.Repository{ID: build.RepositoryID}, app: app, build: build}
		if build.TaskID != nil && tb.Tasks != nil {
			if task, err := tb.Tasks.GetTask(ctx, build.RepositoryID, *build.TaskID); err == nil {
				r.task = &task
			}
		}
		switch build.Status {
		case domain.TestBuildProcessing:
			tb.wg.Add(1)
			go func() {
				defer tb.wg.Done()
				runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), testFlightProcessingLimit+time.Minute)
				defer cancel()
				r.finishIOS(runCtx)
			}()
		case domain.TestBuildActionRequired:
		default:
			r.fail(ctx, errors.New("storeops: the server restarted while this build was running; start it again"))
		}
	}
}
