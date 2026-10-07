// Package simrun builds a board task's checkout for a local simulator or
// emulator and opens it there, so a person can try the change without a
// store upload.
package simrun

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	runTimeout   = 45 * time.Minute
	logTailLines = 80
	failureLimit = 4000
)

type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type TaskReader interface {
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// Checkouter puts the task's current commit in a directory of its own.
type Checkouter interface {
	CheckoutTask(ctx context.Context, repo domain.Repository, task domain.BoardTask) (dir, sha string, cleanup func(), err error)
}

type Deps struct {
	Repos    RepositoryResolver
	Tasks    TaskReader
	Source   Checkouter
	Host     port.SimulatorHost
	CacheDir string
}

type Service struct {
	d Deps

	mu   sync.Mutex
	runs map[uuid.UUID]*domain.SimulatorRun
	busy bool
	wg   sync.WaitGroup
}

func New(d Deps) *Service {
	return &Service{d: d, runs: map[uuid.UUID]*domain.SimulatorRun{}}
}

func (s *Service) Devices(ctx context.Context) ([]domain.SimulatorDevice, error) {
	if s.d.Host == nil {
		return nil, domain.ErrNoSimulators
	}
	devices, err := s.d.Host.Devices(ctx)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, domain.ErrNoSimulators
	}
	return devices, nil
}

// Latest is the task's most recent run.
func (s *Service) Latest(taskID uuid.UUID) (domain.SimulatorRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[taskID]
	if !ok {
		return domain.SimulatorRun{}, domain.ErrSimulatorRunNotFound
	}
	return *run, nil
}

// Wait blocks until every run has finished; tests use it.
func (s *Service) Wait() { s.wg.Wait() }

// Start builds the task for platform and opens it on deviceID. One run at a
// time: two builds racing for one simulator, or for Xcode's derived data,
// would each fail in the other's way.
func (s *Service) Start(ctx context.Context, repositoryID, taskID uuid.UUID, platform, deviceID string) (domain.SimulatorRun, error) {
	if s.d.Host == nil || s.d.Source == nil {
		return domain.SimulatorRun{}, domain.ErrNoSimulators
	}
	repo, err := s.d.Repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.SimulatorRun{}, fmt.Errorf("simrun: loading repository: %w", err)
	}
	task, err := s.d.Tasks.GetTask(ctx, repositoryID, taskID)
	if err != nil {
		return domain.SimulatorRun{}, fmt.Errorf("simrun: loading task: %w", err)
	}
	target, err := resolveTarget(repo, platform)
	if err != nil {
		return domain.SimulatorRun{}, err
	}
	devices, err := s.Devices(ctx)
	if err != nil {
		return domain.SimulatorRun{}, err
	}
	idx := slices.IndexFunc(devices, func(d domain.SimulatorDevice) bool { return d.ID == deviceID && d.Platform == platform })
	if idx < 0 {
		return domain.SimulatorRun{}, fmt.Errorf("simrun: no %s device %q on this machine: %w", platform, deviceID, domain.ErrNoSimulators)
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return domain.SimulatorRun{}, domain.ErrSimulatorRunBusy
	}
	s.busy = true
	run := &domain.SimulatorRun{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		TaskID:       taskID,
		Platform:     platform,
		DeviceID:     deviceID,
		DeviceName:   devices[idx].Name,
		Status:       domain.SimulatorRunPreparing,
		StartedAt:    time.Now(),
	}
	s.runs[taskID] = run
	snapshot := *run
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runTimeout)
		defer cancel()
		s.execute(runCtx, run, repo, task, target)
	}()
	return snapshot, nil
}

type target struct {
	subPath    string
	scheme     string
	module     string
	identifier string
}

func resolveTarget(repo domain.Repository, platform string) (target, error) {
	if platform != domain.MobileStorePlatformIOS && platform != domain.MobileStorePlatformAndroid {
		return target{}, fmt.Errorf("simrun: unsupported platform %q", platform)
	}
	t := target{}
	targets, identity := repo.DetectedBuildTargets, repo.DetectedAppIdentity
	found := repo.Kind == domain.RepoKindMobile && covers(repo.MobilePlatform, platform)
	for _, sub := range repo.SubProjects {
		if sub.Kind != domain.RepoKindMobile || !covers(sub.MobilePlatform, platform) {
			continue
		}
		t.subPath, targets, identity = sub.Path, sub.DetectedBuildTargets, sub.DetectedAppIdentity
		found = true
		break
	}
	if !found {
		return target{}, domain.ErrSimulatorNotMobile
	}
	switch platform {
	case domain.MobileStorePlatformIOS:
		t.scheme, t.identifier = strings.TrimSpace(targets.XcodeScheme), identity.BundleID
		if t.scheme == "" {
			return target{}, errors.New("simrun: the repository does not name an Xcode scheme to build; set it in the repository's mobile settings")
		}
	case domain.MobileStorePlatformAndroid:
		t.module, t.identifier = strings.TrimSpace(targets.GradleModule), identity.PackageName
		if t.module == "" {
			return target{}, errors.New("simrun: the repository does not name a Gradle module to build; set it in the repository's mobile settings")
		}
	}
	return t, nil
}

func covers(mobilePlatform, platform string) bool {
	switch mobilePlatform {
	case domain.MobilePlatformCross:
		return true
	case domain.MobilePlatformIOS:
		return platform == domain.MobileStorePlatformIOS
	case domain.MobilePlatformAndroid:
		return platform == domain.MobileStorePlatformAndroid
	}
	return false
}

func (s *Service) update(run *domain.SimulatorRun, fn func(*domain.SimulatorRun)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(run)
}

func (s *Service) execute(ctx context.Context, run *domain.SimulatorRun, repo domain.Repository, task domain.BoardTask, t target) {
	var tail []string
	logLine := func(line string) {
		s.update(run, func(r *domain.SimulatorRun) {
			tail = append(tail, line)
			if len(tail) > logTailLines {
				tail = tail[len(tail)-logTailLines:]
			}
			r.LogTail = strings.Join(tail, "\n")
		})
	}
	finish := func(err error) {
		now := time.Now()
		s.update(run, func(r *domain.SimulatorRun) {
			r.FinishedAt = &now
			if err != nil {
				r.Status = domain.SimulatorRunFailed
				r.Failure = domain.TruncateHead(err.Error(), failureLimit)
			} else {
				r.Status = domain.SimulatorRunRunning
			}
		})
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
		if err != nil {
			log.Warn().Err(err).Str("task_id", run.TaskID.String()).Str("platform", run.Platform).Msg("simrun: run failed")
		}
	}
	defer func() {
		if p := recover(); p != nil {
			finish(fmt.Errorf("simrun: the run panicked: %v", p))
		}
	}()

	dir, sha, cleanup, err := s.d.Source.CheckoutTask(ctx, repo, task)
	if err != nil {
		finish(fmt.Errorf("simrun: checking out the task: %w", err))
		return
	}
	defer cleanup()
	s.update(run, func(r *domain.SimulatorRun) { r.CommitSHA = sha })

	cache := ""
	if s.d.CacheDir != "" {
		cache = filepath.Join(s.d.CacheDir, repo.ID.String(), run.Platform)
	}
	err = s.d.Host.BuildAndLaunch(ctx, port.SimulatorLaunch{
		Platform:   run.Platform,
		DeviceID:   run.DeviceID,
		ProjectDir: filepath.Join(dir, filepath.FromSlash(t.subPath)),
		Scheme:     t.scheme,
		Module:     t.module,
		Identifier: t.identifier,
		CacheDir:   cache,
		Status: func(status string) {
			s.update(run, func(r *domain.SimulatorRun) { r.Status = status })
		},
		Log: logLine,
	})
	finish(err)
}
