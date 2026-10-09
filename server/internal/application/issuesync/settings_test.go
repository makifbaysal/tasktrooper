package issuesync

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestSetJiraValidatesSiteThenAuth(t *testing.T) {
	fx := newFixture(t)
	fx.svc.jiraSettings = &fakeJiraSettings{}

	if _, err := fx.svc.SetJira(context.Background(), "not a url", "e@acme.com", "tok"); !errors.Is(err, ErrInvalidJiraSite) {
		t.Fatalf("err = %v, want ErrInvalidJiraSite", err)
	}

	fx.jira.myselfErr = fmt.Errorf("%w: HTTP 401", port.ErrJiraUnauthorized)
	if _, err := fx.svc.SetJira(context.Background(), "https://acme.atlassian.net", "e@acme.com", "tok"); !errors.Is(err, ErrJiraAuthFailed) {
		t.Fatalf("err = %v, want ErrJiraAuthFailed", err)
	}

	// Jira being unreachable is not a typo in the credentials, and must not
	// read as one.
	fx.jira.myselfErr = errors.New("dial tcp: i/o timeout")
	if _, err := fx.svc.SetJira(context.Background(), "https://acme.atlassian.net", "e@acme.com", "tok"); !errors.Is(err, ErrUpstream) || errors.Is(err, ErrJiraAuthFailed) {
		t.Fatalf("err = %v, want ErrUpstream and not ErrJiraAuthFailed", err)
	}

	fx.jira.myselfErr = nil
	fx.jira.myself = port.JiraMyself{DisplayName: "Ada"}
	status, err := fx.svc.SetJira(context.Background(), "https://acme.atlassian.net", "e@acme.com", "tok")
	if err != nil {
		t.Fatalf("SetJira: %v", err)
	}
	if !status.Connected || status.DisplayName != "Ada" {
		t.Errorf("status = %+v", status)
	}

	got, err := fx.svc.GetJiraStatus(context.Background())
	if err != nil {
		t.Fatalf("GetJiraStatus: %v", err)
	}
	if !got.Connected || got.SiteURL != "https://acme.atlassian.net" {
		t.Errorf("status = %+v", got)
	}
}

func TestSetIssueSyncValidatesLabel(t *testing.T) {
	fx := newFixture(t)
	cases := []domain.IssueSyncSettings{
		{Label: ""},
		{Label: "has,comma"},
		{Label: "x" + string(make([]byte, 60))},
	}
	for _, c := range cases {
		if _, err := fx.svc.SetIssueSync(context.Background(), c); !errors.Is(err, ErrInvalidSettings) {
			t.Errorf("SetIssueSync(%+v) err = %v, want ErrInvalidSettings", c, err)
		}
	}
}

func TestSetIssueSyncValidatesJiraProjectKeyAndRepository(t *testing.T) {
	fx := newFixture(t)

	bad := domain.IssueSyncSettings{Label: "tasktrooper", JiraProjects: []domain.JiraProjectMapping{
		{ProjectKey: "lowercase", RepositoryID: fx.repoID},
	}}
	if _, err := fx.svc.SetIssueSync(context.Background(), bad); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("err = %v, want ErrInvalidSettings for bad project key", err)
	}

	unknownRepo := domain.IssueSyncSettings{Label: "tasktrooper", JiraProjects: []domain.JiraProjectMapping{
		{ProjectKey: "PROJ", RepositoryID: uuid.New()},
	}}
	if _, err := fx.svc.SetIssueSync(context.Background(), unknownRepo); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("err = %v, want ErrInvalidSettings for unknown repository", err)
	}

	good := domain.IssueSyncSettings{Label: "tasktrooper", ConvertWithPM: true, JiraProjects: []domain.JiraProjectMapping{
		{ProjectKey: "PROJ", RepositoryID: fx.repoID},
	}}
	saved, err := fx.svc.SetIssueSync(context.Background(), good)
	if err != nil {
		t.Fatalf("SetIssueSync: %v", err)
	}
	if len(saved.JiraProjects) != 1 || !saved.ConvertWithPM {
		t.Errorf("saved = %+v", saved)
	}
}

func TestDefaultIssueSyncSettingsConvertWithTheProductManager(t *testing.T) {
	d := domain.DefaultIssueSyncSettings()
	if !d.ConvertWithPM || !d.WriteBack || d.GitHubAutoImport || d.JiraAutoImport {
		t.Fatalf("defaults = %+v", d)
	}
}
