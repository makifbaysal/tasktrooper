package release

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	localRunTimeout       = 60 * time.Minute
	localRunReportTimeout = 70 * time.Minute
)

// forbiddenLocalCommandTokens are shell metacharacters a local release
// command may not use: LocalRunner runs argv directly with shell: false, so
// none of these would do what a person typing them expects — they would
// become a literal argument instead of a pipe/chain/substitution, silently
// wrong rather than refused, unless caught here first.
var forbiddenLocalCommandTokens = []string{"|", "&&", ";", "`", "$("}

func rejectShellMetacharacters(cmd string) error {
	for _, tok := range forbiddenLocalCommandTokens {
		if strings.Contains(cmd, tok) {
			return fmt.Errorf("the local release command may not contain %q — shell: false runs it as a literal argument, not a shell pipeline; run a script instead", tok)
		}
	}
	return nil
}

// splitArgv is a minimal shell-words split: whitespace separates arguments,
// single and double quotes group one argument and are stripped, no other
// shell syntax (expansion, escaping, substitution) is interpreted — the
// command is never handed to a shell, so none of that would apply anyway.
func splitArgv(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inSingle, inDouble, has := false, false, false

	flush := func() {
		if has {
			args = append(args, cur.String())
			cur.Reset()
			has = false
		}
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(c)
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inSingle, has = true, true
		case c == '"':
			inDouble, has = true, true
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteByte(c)
			has = true
		}
	}
	if inSingle || inDouble {
		return nil, fmt.Errorf("the local release command has an unterminated quote")
	}
	flush()
	return args, nil
}

// deployBatchLocal starts the profile's local command (with {version}
// substituted) in a detached worktree of the cut commit, argv-split and never
// shelled out to. The claim (deploying + LocalRun) is persisted BEFORE
// LocalRunner.Start: a command that finishes very fast could otherwise call
// CompleteLocalRun before the claim landed, and its conditional Update
// (expect=deploying) would lose the race against a release still reading as
// pending — the outcome would be silently dropped. Start returns as soon as
// the process has started; the run's outcome arrives later through
// CompleteLocalRun.
func (s *Service) deployBatchLocal(ctx context.Context, r domain.Release) (domain.Release, error) {
	if s.localRunner == nil {
		return domain.Release{}, fmt.Errorf("no local runner is configured on this deployment")
	}
	cmd := strings.ReplaceAll(r.Profile.LocalCommand, "{version}", r.Version)
	if err := rejectShellMetacharacters(cmd); err != nil {
		return domain.Release{}, err
	}
	argv, err := splitArgv(cmd)
	if err != nil {
		return domain.Release{}, err
	}
	if len(argv) == 0 {
		return domain.Release{}, fmt.Errorf("this component has no local release command configured")
	}

	repo, err := s.repo(ctx, r.RepositoryID)
	if err != nil {
		return domain.Release{}, err
	}

	logPath := filepath.Join(s.dataDir, "releases", r.ID.String()+".log")
	env := append(os.Environ(),
		"RELEASE_VERSION="+r.Version,
		"RELEASE_TAG="+r.Tag,
		"RELEASE_COMMIT="+r.CommitSHA,
	)

	now := s.now()
	r.LocalRun = &domain.ReleaseLocalRun{Argv: argv, LogPath: logPath, StartedAt: now}
	r.Status = domain.ReleaseDeploying
	r.DeployStartedAt = &now
	claimed, err := s.store.Update(ctx, r, domain.ReleasePending)
	if err != nil {
		return domain.Release{}, err
	}

	releaseID := claimed.ID
	spec := LocalRunSpec{
		RootPath:  repo.RootPath,
		CommitSHA: claimed.CommitSHA,
		Argv:      argv,
		Env:       env,
		LogPath:   logPath,
		Timeout:   localRunTimeout,
	}
	if err := s.localRunner.Start(ctx, spec, func(exitCode int, tail string, runErr error) {
		s.CompleteLocalRun(context.Background(), releaseID, exitCode, tail, runErr)
	}); err != nil {
		return s.failClaimed(ctx, claimed, fmt.Sprintf("starting the local release run: %s", err))
	}

	return claimed, nil
}

// CompleteLocalRun is the LocalRunner's callback: it records the run's
// outcome and a synthesized Deploy status but leaves Status at deploying (a
// plain optimistic Update expecting deploying) — the sweeper is what reads
// LocalRun.FinishedAt/Deploy and advances the release, the same way it reads
// an Actions run's conclusion for github_actions.
func (s *Service) CompleteLocalRun(ctx context.Context, releaseID uuid.UUID, exitCode int, tail string, runErr error) {
	r, err := s.store.Get(ctx, releaseID)
	if err != nil {
		log.Warn().Err(err).Str("release_id", releaseID.String()).Msg("release: loading the release to record its local run outcome failed")
		return
	}
	if r.LocalRun == nil {
		log.Warn().Str("release_id", releaseID.String()).Msg("release: local run completion for a release with no local run recorded")
		return
	}

	now := s.now()
	code := exitCode
	r.LocalRun.ExitCode = &code
	r.LocalRun.FinishedAt = &now
	r.LocalRun.Tail = tail
	if runErr != nil {
		r.LocalRun.Error = runErr.Error()
	}

	status := domain.DeployWatchStatus{
		RepositoryID: r.RepositoryID,
		MergeSHA:     r.CommitSHA,
		Signal:       "local_run",
		CheckedAt:    now,
	}
	if runErr == nil && exitCode == 0 {
		status.State = domain.DeployWatchSuccess
		status.Detail = "the local release command exited 0"
	} else {
		status.State = domain.DeployWatchFailure
		status.Detail = localFailureDetail(exitCode, tail, runErr)
	}
	r.Deploy = &status

	if _, uerr := s.store.Update(ctx, r, domain.ReleaseDeploying); uerr != nil {
		s.logSweepUpdate(uerr, r.ID)
	}
}

func localFailureDetail(exitCode int, tail string, runErr error) string {
	reason := fmt.Sprintf("the local release command exited %d", exitCode)
	if runErr != nil {
		reason = runErr.Error()
	}
	if tail == "" {
		return reason
	}
	return reason + ": " + tail
}

// sweepDeployingLocal watches a batch/local release's run: no report within
// 70 minutes of it starting is treated as the server having restarted mid-run
// (the run itself has no way to call back once the process that would run its
// callback is gone), not as a hung command.
func (s *Service) sweepDeployingLocal(ctx context.Context, r domain.Release) {
	now := s.now()
	if r.LocalRun == nil || r.LocalRun.FinishedAt == nil {
		started := r.DeployStartedAt
		if r.LocalRun != nil {
			started = &r.LocalRun.StartedAt
		}
		if started != nil && now.Sub(*started) > localRunReportTimeout {
			logPath := ""
			if r.LocalRun != nil {
				logPath = r.LocalRun.LogPath
			}
			s.failDeploying(ctx, r, fmt.Sprintf(
				"the local release did not report back — the server may have restarted while it ran; check %s", logPath))
		}
		return
	}
	if r.Deploy != nil && r.Deploy.State == domain.DeployWatchSuccess {
		s.settleDeploySuccess(ctx, r, *r.Deploy)
		return
	}
	detail := ""
	if r.Deploy != nil {
		detail = r.Deploy.Detail
	}
	s.failDeploying(ctx, r, detail)
}
