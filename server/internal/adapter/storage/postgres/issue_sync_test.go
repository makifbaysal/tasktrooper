package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// IssueSyncSuite covers migration 183's stores: one issue_imports row per
// issue, any number of issue_links per issue, and the Jira and issue-sync
// settings on app_settings.
type IssueSyncSuite struct {
	suite.Suite
	ctx      context.Context
	cancel   context.CancelFunc
	pg       *database.Embedded
	pool     *pgxpool.Pool
	tasks    *postgres.BoardTaskStore
	sessions *postgres.SessionStore
	links    *postgres.IssueLinkStore
	imports  *postgres.IssueImportStore
	settings *postgres.SettingsStore
	repoID   uuid.UUID
}

func TestIssueSyncSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(IssueSyncSuite))
}

func (s *IssueSyncSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.tasks = postgres.NewBoardTaskStore(db)
	s.sessions = postgres.NewSessionStore(db)
	s.links = postgres.NewIssueLinkStore(db)
	s.imports = postgres.NewIssueImportStore(db)
	s.settings = postgres.NewSettingsStore(db, "./data/workspaces", "en", false)
	cipher, err := secrets.NewCipher(make([]byte, 32))
	s.Require().NoError(err)
	s.settings.SetCipher(cipher, nil)
	repo, err := postgres.NewRepositoryStore(db).Create(s.ctx, "issue-sync-test", "", "/tmp/issue-sync-test-"+uuid.NewString(), "", "")
	s.Require().NoError(err)
	s.repoID = repo.ID
}

func (s *IssueSyncSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *IssueSyncSuite) newTask(title string) domain.BoardTask {
	num, err := s.tasks.NextTaskNumber(s.ctx, "task")
	s.Require().NoError(err)
	task, err := s.tasks.Create(s.ctx, domain.BoardTask{
		RepositoryID: s.repoID, TaskNumber: num, Title: title, TaskType: "task",
		Column: domain.TaskColumnBacklog, Priority: domain.TaskPriorityMedium, CreatedBy: "test",
	})
	s.Require().NoError(err)
	return task
}

func (s *IssueSyncSuite) link(task domain.BoardTask, key string) domain.IssueLink {
	link, err := s.links.Create(s.ctx, domain.IssueLink{
		TaskID: task.ID, TaskKey: task.Key, RepositoryID: s.repoID, Provider: domain.IssueProviderGitHub,
		ExternalKey: key, URL: "https://github.com/acme/widget/issues/1", Title: "Issue", LastColumn: "backlog",
	})
	s.Require().NoError(err)
	return link
}

func (s *IssueSyncSuite) TestOneIssueHoldsSeveralTasks() {
	key := "acme/widget#" + uuid.NewString()[:8]
	first, second := s.newTask("first"), s.newTask("second")
	s.link(first, key)
	s.link(second, key)

	links, err := s.links.ListByIssue(s.ctx, domain.IssueProviderGitHub, key)
	s.Require().NoError(err)
	s.Require().Len(links, 2)
	s.Equal(first.ID, links[0].TaskID, "oldest first")

	existing, err := s.links.ExistingKeys(s.ctx, domain.IssueProviderGitHub, []string{key, "acme/widget#none"})
	s.Require().NoError(err)
	s.Require().Len(existing, 1)
	s.Equal(first.ID, existing[key].TaskID)

	s.Require().NoError(s.links.MarkIssueClosed(s.ctx, domain.IssueProviderGitHub, key, time.Now()))
	for _, id := range []uuid.UUID{first.ID, second.ID} {
		got, err := s.links.GetByTask(s.ctx, id)
		s.Require().NoError(err)
		s.NotNil(got.ClosedAt)
	}

	s.Require().NoError(s.tasks.Delete(s.ctx, s.repoID, first.ID))
	_, err = s.links.GetByTask(s.ctx, first.ID)
	s.ErrorIs(err, port.ErrNotFound, "a deleted task takes its link with it")
	links, err = s.links.ListByIssue(s.ctx, domain.IssueProviderGitHub, key)
	s.Require().NoError(err)
	s.Len(links, 1)
}

func (s *IssueSyncSuite) TestImportIsUniquePerIssueAndOutlivesItsIntakeTask() {
	key := "PROJ-" + uuid.NewString()[:4]
	intake := s.newTask("intake")
	created, err := s.imports.Create(s.ctx, domain.IssueImport{
		Provider: domain.IssueProviderJira, ExternalKey: key, RepositoryID: s.repoID, URL: "u", Title: "t",
		ImportedBy: "user-1", IntakeTaskID: &intake.ID, ConversionStatus: domain.IssueConversionPending,
	})
	s.Require().NoError(err)

	_, err = s.imports.Create(s.ctx, domain.IssueImport{
		Provider: domain.IssueProviderJira, ExternalKey: key, RepositoryID: s.repoID, URL: "u",
	})
	s.ErrorIs(err, port.ErrIssueImportExists)

	claimed, err := s.imports.ClaimConversion(s.ctx, created.ID)
	s.Require().NoError(err)
	s.True(claimed)
	claimed, err = s.imports.ClaimConversion(s.ctx, created.ID)
	s.Require().NoError(err)
	s.False(claimed, "a converting import cannot be claimed twice")

	sess, err := s.sessions.Create(s.ctx, "PROJ conversion", "", "/tmp", &s.repoID, nil, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.imports.SetConversion(s.ctx, created.ID, domain.IssueConversionConverting, &sess.ID, ""))
	bySession, err := s.imports.GetBySession(s.ctx, sess.ID)
	s.Require().NoError(err)
	s.Equal(created.ID, bySession.ID)

	s.Require().NoError(s.imports.ResetConverting(s.ctx))
	got, err := s.imports.Get(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(domain.IssueConversionPending, got.ConversionStatus)
	s.Require().NotNil(got.ConversionSessionID, "a reset keeps the chat to continue in")

	s.Require().NoError(s.tasks.Delete(s.ctx, s.repoID, intake.ID))
	got, err = s.imports.GetByProviderKey(s.ctx, domain.IssueProviderJira, key)
	s.Require().NoError(err)
	s.Nil(got.IntakeTaskID, "deleting the intake task clears it, the import stays")

	pending, err := s.imports.ListByStatus(s.ctx, domain.IssueConversionPending)
	s.Require().NoError(err)
	s.Contains(importIDs(pending), created.ID)

	s.Require().NoError(s.imports.MarkClosed(s.ctx, created.ID, time.Now()))
	s.Require().NoError(s.imports.Delete(s.ctx, created.ID))
	_, err = s.imports.Get(s.ctx, created.ID)
	s.ErrorIs(err, port.ErrNotFound)
}

func importIDs(imps []domain.IssueImport) []uuid.UUID {
	out := make([]uuid.UUID, len(imps))
	for i, imp := range imps {
		out[i] = imp.ID
	}
	return out
}

func (s *IssueSyncSuite) TestJiraTokenIsEncryptedAtRest() {
	site, _, _, err := s.settings.JiraSettings(s.ctx)
	s.Require().NoError(err)
	s.Empty(site)

	s.Require().NoError(s.settings.SetJiraSettings(s.ctx, "https://acme.atlassian.net", "a@acme.com", "jira-secret-token"))
	site, email, token, err := s.settings.JiraSettings(s.ctx)
	s.Require().NoError(err)
	s.Equal("https://acme.atlassian.net", site)
	s.Equal("a@acme.com", email)
	s.Equal("jira-secret-token", token)

	var stored string
	s.Require().NoError(s.pool.QueryRow(s.ctx, `SELECT value FROM app_settings WHERE key = 'jira_api_token'`).Scan(&stored))
	s.NotContains(stored, "jira-secret-token")

	s.Require().NoError(s.settings.DeleteJiraSettings(s.ctx))
	site, _, token, err = s.settings.JiraSettings(s.ctx)
	s.Require().NoError(err)
	s.Empty(site)
	s.Empty(token)
}

func (s *IssueSyncSuite) TestIssueSyncSettingsDecodeOverTheDefaults() {
	got, err := s.settings.IssueSyncSettings(s.ctx)
	s.Require().NoError(err)
	s.Equal(domain.DefaultIssueSyncSettings(), got)

	_, err = s.pool.Exec(s.ctx, `
		INSERT INTO app_settings (key, value) VALUES ('issue_sync_settings', '{"label":"triage","write_back":false}')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`)
	s.Require().NoError(err)
	got, err = s.settings.IssueSyncSettings(s.ctx)
	s.Require().NoError(err)
	s.Equal("triage", got.Label)
	s.False(got.WriteBack)
	s.True(got.ConvertWithPM, "a row saved before the field existed keeps its default")

	got.ConvertWithPM = false
	s.Require().NoError(s.settings.SetIssueSyncSettings(s.ctx, got))
	again, err := s.settings.IssueSyncSettings(s.ctx)
	s.Require().NoError(err)
	s.False(again.ConvertWithPM)
}
