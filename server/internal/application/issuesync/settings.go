package issuesync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxLabelLen = 50

// GetJiraStatus never carries the token itself.
func (s *Service) GetJiraStatus(ctx context.Context) (domain.JiraStatus, error) {
	if s.jiraSettings == nil {
		return domain.JiraStatus{}, nil
	}
	site, email, token, err := s.jiraSettings.JiraSettings(ctx)
	if err != nil {
		return domain.JiraStatus{}, fmt.Errorf("read jira settings: %w", err)
	}
	return domain.JiraStatus{Connected: site != "" && token != "", SiteURL: site, Email: email}, nil
}

// SetJira stores credentials only after Myself proved them; building the
// client validates the site URL.
func (s *Service) SetJira(ctx context.Context, siteURL, email, apiToken string) (domain.JiraStatus, error) {
	if s.jiraFactory == nil || s.jiraSettings == nil {
		return domain.JiraStatus{}, ErrSourceNotConfigured
	}
	client, err := s.jiraFactory(siteURL, email, apiToken)
	if err != nil {
		return domain.JiraStatus{}, fmt.Errorf("%w: %w", ErrInvalidJiraSite, err)
	}
	me, err := client.Myself(ctx)
	if err != nil {
		if errors.Is(err, port.ErrJiraUnauthorized) {
			return domain.JiraStatus{}, fmt.Errorf("%w: %w", ErrJiraAuthFailed, err)
		}
		return domain.JiraStatus{}, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	if err := s.jiraSettings.SetJiraSettings(ctx, siteURL, email, apiToken); err != nil {
		return domain.JiraStatus{}, fmt.Errorf("store jira settings: %w", err)
	}
	status, err := s.GetJiraStatus(ctx)
	if err != nil {
		return domain.JiraStatus{}, err
	}
	status.DisplayName = me.DisplayName
	return status, nil
}

func (s *Service) DeleteJira(ctx context.Context) error {
	if s.jiraSettings == nil {
		return nil
	}
	return s.jiraSettings.DeleteJiraSettings(ctx)
}

func (s *Service) ListJiraProjects(ctx context.Context) ([]domain.JiraProjectRef, error) {
	client, connected, err := s.jiraClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("jira settings: %w", err)
	}
	if !connected {
		return nil, fmt.Errorf("%w: jira is not connected", ErrSourceNotConfigured)
	}
	projects, err := client.Projects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list jira projects: %w: %w", ErrUpstream, err)
	}
	return projects, nil
}

func (s *Service) GetIssueSync(ctx context.Context) (domain.IssueSyncSettings, error) {
	if s.syncSettings == nil {
		return domain.DefaultIssueSyncSettings(), nil
	}
	return s.syncSettings.IssueSyncSettings(ctx)
}

// SetIssueSync: label is 1..50 characters with no comma or control character,
// and every Jira project mapping names a real project key and repository.
func (s *Service) SetIssueSync(ctx context.Context, settings domain.IssueSyncSettings) (domain.IssueSyncSettings, error) {
	if s.syncSettings == nil {
		return domain.IssueSyncSettings{}, ErrSourceNotConfigured
	}
	if err := validateIssueSyncSettings(settings); err != nil {
		return domain.IssueSyncSettings{}, err
	}
	if s.repos != nil {
		for _, mapping := range settings.JiraProjects {
			if _, err := s.repos.Get(ctx, mapping.RepositoryID); err != nil {
				return domain.IssueSyncSettings{}, fmt.Errorf("%w: unknown repository %s for project %s", ErrInvalidSettings, mapping.RepositoryID, mapping.ProjectKey)
			}
		}
	}
	if err := s.syncSettings.SetIssueSyncSettings(ctx, settings); err != nil {
		return domain.IssueSyncSettings{}, fmt.Errorf("store issue sync settings: %w", err)
	}
	return settings, nil
}

func validateIssueSyncSettings(settings domain.IssueSyncSettings) error {
	label := strings.TrimSpace(settings.Label)
	if label == "" || len([]rune(label)) > maxLabelLen {
		return fmt.Errorf("%w: label must be 1-%d characters", ErrInvalidSettings, maxLabelLen)
	}
	if strings.ContainsRune(label, ',') {
		return fmt.Errorf("%w: label may not contain a comma", ErrInvalidSettings)
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: label may not contain control characters", ErrInvalidSettings)
		}
	}
	for _, mapping := range settings.JiraProjects {
		if !jiraProjectKeyPattern.MatchString(mapping.ProjectKey) {
			return fmt.Errorf("%w: invalid jira project key %q", ErrInvalidSettings, mapping.ProjectKey)
		}
	}
	return nil
}
