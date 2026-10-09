package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The Jira site and email are stored in the clear; the API token is encrypted
// with the same boot-time cipher as github_token.
const (
	jiraSiteURLKey = "jira_site_url"
	jiraEmailKey   = "jira_email"
	jiraTokenKey   = "jira_api_token"

	issueSyncSettingsKey = "issue_sync_settings"
)

func (s *SettingsStore) JiraSettings(ctx context.Context) (siteURL, email, apiToken string, err error) {
	siteURL, err = s.plain(ctx, jiraSiteURLKey)
	if err != nil || siteURL == "" {
		return "", "", "", err
	}
	email, err = s.plain(ctx, jiraEmailKey)
	if err != nil {
		return "", "", "", err
	}
	apiToken, err = s.secret(ctx, jiraTokenKey)
	if err != nil {
		return "", "", "", err
	}
	return siteURL, email, apiToken, nil
}

func (s *SettingsStore) SetJiraSettings(ctx context.Context, siteURL, email, apiToken string) error {
	if err := s.setSecret(ctx, jiraTokenKey, apiToken); err != nil {
		return err
	}
	if err := s.setPlain(ctx, jiraEmailKey, email); err != nil {
		return err
	}
	return s.setPlain(ctx, jiraSiteURLKey, siteURL)
}

func (s *SettingsStore) DeleteJiraSettings(ctx context.Context) error {
	return s.forgetKeys(ctx, jiraSiteURLKey, jiraEmailKey, jiraTokenKey)
}

// IssueSyncSettings decodes over the defaults, so a field added after the row
// was saved reads as its default rather than as false.
func (s *SettingsStore) IssueSyncSettings(ctx context.Context) (domain.IssueSyncSettings, error) {
	out := domain.DefaultIssueSyncSettings()
	raw, err := s.plain(ctx, issueSyncSettingsKey)
	if err != nil {
		return domain.IssueSyncSettings{}, err
	}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return domain.IssueSyncSettings{}, fmt.Errorf("decode issue sync settings: %w", err)
	}
	if out.JiraProjects == nil {
		out.JiraProjects = []domain.JiraProjectMapping{}
	}
	return out, nil
}

func (s *SettingsStore) SetIssueSyncSettings(ctx context.Context, settings domain.IssueSyncSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode issue sync settings: %w", err)
	}
	return s.setPlain(ctx, issueSyncSettingsKey, string(raw))
}

func (s *SettingsStore) plain(ctx context.Context, key string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get %s: %w", key, err)
	}
	return value, nil
}
