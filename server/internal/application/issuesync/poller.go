package issuesync

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// PollInterval is also the whole story on a local install, where GitHub's
// webhooks usually cannot reach this server.
const PollInterval = 2 * time.Minute

// pollFirstRunDelay lets the process finish booting before the first pass.
const pollFirstRunDelay = 30 * time.Second

const pollLabelledPageSize = 50

// Start launches the automatic-import poller; it exits with ctx.
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.tasks == nil || s.links == nil || s.imports == nil || s.syncSettings == nil {
		return
	}
	if interval <= 0 {
		interval = PollInterval
	}
	go func() {
		t := time.NewTimer(pollFirstRunDelay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.RunPollOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RunPollOnce(ctx)
			}
		}
	}()
}

// RunPollOnce imports every not-yet-imported open issue carrying the
// configured label. A failing repository or project is logged and skipped.
func (s *Service) RunPollOnce(ctx context.Context) {
	settings, err := s.syncSettings.IssueSyncSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("issuesync: poll skipped, reading settings failed")
		return
	}
	if settings.GitHubAutoImport {
		s.pollGitHub(ctx, settings.Label)
	}
	if settings.JiraAutoImport {
		s.pollJira(ctx, settings)
	}
}

func (s *Service) pollGitHub(ctx context.Context, label string) {
	if s.repos == nil || s.githubIssues == nil {
		return
	}
	token, err := s.githubToken(ctx)
	if err != nil || token == "" {
		return
	}
	repos, err := s.repos.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("issuesync: github poll: listing repositories failed")
		return
	}
	for _, repo := range repos {
		owner, name, ok := githubOwnerRepo(repo.RemoteURL)
		if !ok {
			continue
		}
		issues, err := s.githubIssues.ListLabelled(ctx, token, owner, name, label, pollLabelledPageSize)
		if err != nil {
			log.Warn().Err(err).Str("repository_id", repo.ID.String()).Msg("issuesync: github poll: listing issues failed")
			continue
		}
		for _, issue := range issues {
			s.pollImport(ctx, domain.IssueProviderGitHub, issue.Key, repo.ID)
		}
	}
}

func (s *Service) pollJira(ctx context.Context, settings domain.IssueSyncSettings) {
	if len(settings.JiraProjects) == 0 {
		return
	}
	client, connected, err := s.jiraClient(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("issuesync: jira poll: reading connection settings failed")
		return
	}
	if !connected {
		return
	}
	for _, mapping := range settings.JiraProjects {
		jql := "project = " + quoteJQL(mapping.ProjectKey) +
			" AND labels = " + quoteJQL(settings.Label) +
			" AND statusCategory != Done ORDER BY created DESC"
		issues, err := client.Search(ctx, jql, pollLabelledPageSize)
		if err != nil {
			log.Warn().Err(err).Str("project", mapping.ProjectKey).Msg("issuesync: jira poll: search failed")
			continue
		}
		for _, issue := range issues {
			s.pollImport(ctx, domain.IssueProviderJira, issue.Key, mapping.RepositoryID)
		}
	}
}

// pollImport skips an issue that is already imported without a word: that is
// the poller's steady state on every pass after the first.
func (s *Service) pollImport(ctx context.Context, provider domain.IssueProvider, key string, repositoryID uuid.UUID) {
	if _, err := s.imports.GetByProviderKey(ctx, provider, key); err == nil {
		return
	} else if !errors.Is(err, port.ErrNotFound) {
		log.Warn().Err(err).Str("key", key).Msg("issuesync: poll: checking existing import failed")
		return
	}
	if _, err := s.Import(ctx, provider, key, repositoryID, string(provider), ""); err != nil {
		var already *AlreadyImportedError
		if errors.As(err, &already) {
			return
		}
		log.Warn().Err(err).Str("key", key).Str("provider", string(provider)).Msg("issuesync: automatic import failed")
	}
}
