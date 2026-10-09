package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	jiraclient "github.com/makifbaysal/tasktrooper/server/internal/adapter/issuetracker/jira"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// jiraClientAdapter is the one place that translates between port.JiraClient
// and internal/adapter/issuetracker/jira, which knows nothing about this
// server's types on purpose.
type jiraClientAdapter struct {
	c *jiraclient.Client
}

// newJiraClientFactory never caches a client: the credentials come fresh from
// Settings on every call.
func newJiraClientFactory() port.JiraClientFactory {
	return func(site, email, apiToken string) (port.JiraClient, error) {
		c, err := jiraclient.New(site, email, apiToken, nil)
		if err != nil {
			return nil, err
		}
		return &jiraClientAdapter{c: c}, nil
	}
}

func (a *jiraClientAdapter) Myself(ctx context.Context) (port.JiraMyself, error) {
	me, err := a.c.Myself(ctx)
	if err != nil {
		var apiErr *jiraclient.APIError
		if errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden) {
			return port.JiraMyself{}, fmt.Errorf("%w: %w", port.ErrJiraUnauthorized, err)
		}
		return port.JiraMyself{}, err
	}
	return port.JiraMyself{AccountID: me.AccountID, DisplayName: me.DisplayName, Email: me.EmailAddress}, nil
}

func (a *jiraClientAdapter) Projects(ctx context.Context) ([]domain.JiraProjectRef, error) {
	projects, err := a.c.Projects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.JiraProjectRef, len(projects))
	for i, p := range projects {
		out[i] = domain.JiraProjectRef{Key: p.Key, Name: p.Name}
	}
	return out, nil
}

func (a *jiraClientAdapter) Search(ctx context.Context, jql string, max int) ([]domain.ExternalIssue, error) {
	issues, err := a.c.Search(ctx, jql, max)
	if err != nil {
		return nil, err
	}
	return jiraIssuesToExternal(issues), nil
}

func (a *jiraClientAdapter) GetIssue(ctx context.Context, key string) (domain.ExternalIssue, error) {
	issue, err := a.c.GetIssue(ctx, key)
	if err != nil {
		return domain.ExternalIssue{}, err
	}
	return jiraIssueToExternal(issue), nil
}

func (a *jiraClientAdapter) AddComment(ctx context.Context, key, text string) error {
	return a.c.AddComment(ctx, key, text)
}

func (a *jiraClientAdapter) TransitionToDone(ctx context.Context, key string) (bool, error) {
	return a.c.TransitionToDone(ctx, key)
}

func jiraIssueToExternal(issue jiraclient.Issue) domain.ExternalIssue {
	return domain.ExternalIssue{
		Provider:  domain.IssueProviderJira,
		Key:       issue.Key,
		Title:     issue.Summary,
		Body:      issue.DescriptionMarkdown,
		URL:       issue.URL,
		State:     issue.Status,
		Labels:    issue.Labels,
		IssueType: issue.IssueType,
		Priority:  issue.Priority,
		UpdatedAt: issue.Updated,
	}
}

func jiraIssuesToExternal(issues []jiraclient.Issue) []domain.ExternalIssue {
	out := make([]domain.ExternalIssue, len(issues))
	for i, iss := range issues {
		out[i] = jiraIssueToExternal(iss)
	}
	return out
}
