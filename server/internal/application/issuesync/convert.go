package issuesync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ConversionSessions is session.Service: the conversion is an ordinary chat
// turn with the product manager, so an agent CLI provider runs it on this
// machine exactly like any other chat.
type ConversionSessions interface {
	Create(ctx context.Context, req domain.CreateSessionRequest) (domain.Session, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Session, []domain.SessionMessage, error)
	SendMessage(ctx context.Context, sessionID uuid.UUID, req domain.SessionMessageRequest, policy domain.ToolPolicy) (domain.AgentResponse, error)
}

type ConversionRoles interface {
	RoleByKey(ctx context.Context, key string) (domain.AgentRole, error)
	AgentForRole(ctx context.Context, roleID uuid.UUID, area string) (*uuid.UUID, error)
}

type ConversionAgents interface {
	GetAgent(ctx context.Context, id uuid.UUID) (domain.Agent, error)
}

type ConversionDeps struct {
	Sessions ConversionSessions
	Roles    ConversionRoles
	Agents   ConversionAgents
	// Policy is the server's default tool policy, the same base an HTTP chat
	// turn starts from before the agent's own policy is merged in.
	Policy domain.ToolPolicy
}

// conversionSweepInterval catches a pending import whose wake was missed.
const conversionSweepInterval = time.Minute

const maxConversionErrorLen = 500

type convertRequestInput struct {
	TaskKey        string
	RepositoryName string
	Provider       string
	IssueKey       string
	IssueURL       string
}

var convertRequestKey = prompt.Define("issuesync.convert_request", convertRequestInput{
	TaskKey: "T-1", RepositoryName: "acme-web", Provider: "GitHub",
	IssueKey: "acme/web#42", IssueURL: "https://github.com/acme/web/issues/42",
})

// StartConversions turns on the product manager conversion and starts its
// worker. One worker: a first poll over a backlog of labelled issues must not
// start one agent run per issue at once.
func (s *Service) StartConversions(ctx context.Context, deps ConversionDeps) {
	if s == nil || s.imports == nil || s.links == nil || s.tasks == nil ||
		deps.Sessions == nil || deps.Roles == nil {
		return
	}
	if err := s.imports.ResetConverting(ctx); err != nil {
		log.Warn().Err(err).Msg("issuesync: resetting interrupted conversions failed")
	}
	s.conv.Store(&deps)
	go s.conversionLoop(ctx)
}

func (s *Service) conversionWanted(ctx context.Context) bool {
	if s.conv.Load() == nil {
		return false
	}
	if s.syncSettings == nil {
		return domain.DefaultIssueSyncSettings().ConvertWithPM
	}
	settings, err := s.syncSettings.IssueSyncSettings(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("issuesync: reading issue-sync settings failed, conversion skipped")
		return false
	}
	return settings.ConvertWithPM
}

func (s *Service) wakeConversions() {
	select {
	case s.convWake <- struct{}{}:
	default:
	}
}

func (s *Service) conversionLoop(ctx context.Context) {
	ticker := time.NewTicker(conversionSweepInterval)
	defer ticker.Stop()
	for {
		s.RunConversionsOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.convWake:
		case <-ticker.C:
		}
	}
}

// RunConversionsOnce converts every pending import, one after another.
func (s *Service) RunConversionsOnce(ctx context.Context) {
	for ctx.Err() == nil {
		pending, err := s.imports.ListByStatus(ctx, domain.IssueConversionPending)
		if err != nil {
			log.Warn().Err(err).Msg("issuesync: listing pending conversions failed")
			return
		}
		claimed := false
		for _, imp := range pending {
			ok, err := s.imports.ClaimConversion(ctx, imp.ID)
			if err != nil {
				log.Warn().Err(err).Str("issue", imp.ExternalKey).Msg("issuesync: claiming a conversion failed")
				return
			}
			if !ok {
				continue
			}
			claimed = true
			imp.ConversionStatus = domain.IssueConversionConverting
			s.convert(ctx, imp)
		}
		if !claimed {
			return
		}
	}
}

// RetryConversion queues a failed or skipped conversion again, for instance
// after a product manager agent was added.
func (s *Service) RetryConversion(ctx context.Context, importID uuid.UUID) (domain.IssueImport, error) {
	if s.conv.Load() == nil || s.imports == nil {
		return domain.IssueImport{}, ErrConversionUnavailable
	}
	imp, err := s.imports.Get(ctx, importID)
	if err != nil {
		return domain.IssueImport{}, err
	}
	if imp.ConversionStatus != domain.IssueConversionFailed && imp.ConversionStatus != domain.IssueConversionSkipped &&
		imp.ConversionStatus != domain.IssueConversionNone {
		return domain.IssueImport{}, ErrConversionNotRetryable
	}
	if imp.IntakeTaskID == nil {
		return domain.IssueImport{}, ErrConversionNotRetryable
	}
	if _, err := s.tasks.GetTask(ctx, imp.RepositoryID, *imp.IntakeTaskID); err != nil {
		return domain.IssueImport{}, ErrConversionNotRetryable
	}
	if err := s.imports.SetConversion(ctx, imp.ID, domain.IssueConversionPending, nil, ""); err != nil {
		return domain.IssueImport{}, err
	}
	s.wakeConversions()
	imp.ConversionStatus = domain.IssueConversionPending
	imp.ConversionError = ""
	return imp, nil
}

func (s *Service) convert(ctx context.Context, imp domain.IssueImport) {
	deps := s.conv.Load()
	if deps == nil {
		s.failConversion(ctx, imp, domain.IssueConversionFailed, ErrConversionUnavailable.Error())
		return
	}
	if imp.IntakeTaskID == nil {
		s.failConversion(ctx, imp, domain.IssueConversionFailed, "the imported task is gone")
		return
	}
	intake, err := s.tasks.GetTask(ctx, imp.RepositoryID, *imp.IntakeTaskID)
	if err != nil {
		s.failConversion(ctx, imp, domain.IssueConversionFailed, "the imported task is gone")
		return
	}
	// A conversion cut short by a restart may already have opened its tasks;
	// running the product manager again would open them twice.
	if converted := s.convertedLinks(ctx, imp); len(converted) > 0 {
		s.completeConversion(ctx, imp.ID, converted)
		return
	}
	repo, err := s.repos.Get(ctx, imp.RepositoryID)
	if err != nil {
		s.failConversion(ctx, imp, domain.IssueConversionFailed, fmt.Sprintf("read repository: %v", err))
		return
	}
	pmID, err := s.productManager(ctx, deps, repo)
	if err != nil {
		status := domain.IssueConversionFailed
		if errors.Is(err, ErrNoProductManager) {
			status = domain.IssueConversionSkipped
		}
		s.failConversion(ctx, imp, status, err.Error())
		return
	}
	sessionID, err := s.conversionSession(ctx, deps, imp, repo.ID, pmID)
	if err != nil {
		s.failConversion(ctx, imp, domain.IssueConversionFailed, fmt.Sprintf("open the product manager chat: %v", err))
		return
	}
	if err := s.imports.SetConversion(ctx, imp.ID, domain.IssueConversionConverting, &sessionID, ""); err != nil {
		log.Warn().Err(err).Str("issue", imp.ExternalKey).Msg("issuesync: recording the conversion session failed")
		return
	}

	message := strings.TrimSpace(convertRequestKey.Render(convertRequestInput{
		TaskKey:        intake.Key,
		RepositoryName: repo.Name,
		Provider:       providerName(imp.Provider),
		IssueKey:       imp.ExternalKey,
		IssueURL:       imp.URL,
	}))
	runCtx := ctx
	if imp.ImportedBy != "" {
		runCtx = registry.ContextWithActorUserID(runCtx, imp.ImportedBy)
	}
	noOrchestration := false
	resp, runErr := deps.Sessions.SendMessage(runCtx, sessionID, domain.SessionMessageRequest{
		Role:        domain.RoleUser,
		Content:     message,
		Orchestrate: &noOrchestration,
	}, deps.Policy)

	if converted := s.convertedLinks(ctx, imp); len(converted) > 0 {
		s.completeConversion(ctx, imp.ID, converted)
		return
	}
	switch {
	case runErr != nil:
		if _, parked := domain.QuotaBlockOf(runErr); parked {
			s.setConversion(ctx, imp.ID, domain.IssueConversionNeedsInput, truncateError(runErr.Error()))
			return
		}
		s.failConversion(ctx, imp, domain.IssueConversionFailed, runErr.Error())
	case resp.Clarification != nil:
		s.setConversion(ctx, imp.ID, domain.IssueConversionNeedsInput, "")
	default:
		reason := "the product manager opened no task"
		if reply := strings.TrimSpace(resp.Message.Content); reply != "" {
			reason += ": " + reply
		}
		s.failConversion(ctx, imp, domain.IssueConversionFailed, reason)
	}
}

func (s *Service) productManager(ctx context.Context, deps *ConversionDeps, repo domain.Repository) (uuid.UUID, error) {
	role, err := deps.Roles.RoleByKey(ctx, domain.RoleKeyProductManager)
	if err != nil {
		return uuid.Nil, ErrNoProductManager
	}
	id, err := deps.Roles.AgentForRole(ctx, role.ID, domain.RepoArea(repo.Kind, repo.SubProjects))
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve the product manager: %w", err)
	}
	if id == nil {
		return uuid.Nil, ErrNoProductManager
	}
	if deps.Agents != nil {
		agent, err := deps.Agents.GetAgent(ctx, *id)
		if err != nil || !agent.Enabled {
			return uuid.Nil, ErrNoProductManager
		}
	}
	return *id, nil
}

// conversionSession reuses the import's chat when it still exists, so a retry
// continues the conversation the person may already have read.
func (s *Service) conversionSession(ctx context.Context, deps *ConversionDeps, imp domain.IssueImport, repositoryID, pmID uuid.UUID) (uuid.UUID, error) {
	if imp.ConversionSessionID != nil {
		if sess, _, err := deps.Sessions.Get(ctx, *imp.ConversionSessionID); err == nil {
			return sess.ID, nil
		}
	}
	title := domain.TruncateHead(strings.TrimSpace(imp.ExternalKey+" "+imp.Title), 120)
	sess, err := deps.Sessions.Create(ctx, domain.CreateSessionRequest{
		Title:     title,
		ProjectID: &repositoryID,
		AgentID:   &pmID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return sess.ID, nil
}

// convertedLinks are the issue's links other than the intake task's: the
// tasks the product manager opened in the conversion chat.
func (s *Service) convertedLinks(ctx context.Context, imp domain.IssueImport) []domain.IssueLink {
	links, err := s.links.ListByIssue(ctx, imp.Provider, imp.ExternalKey)
	if err != nil {
		log.Warn().Err(err).Str("issue", imp.ExternalKey).Msg("issuesync: listing converted tasks failed")
		return nil
	}
	out := make([]domain.IssueLink, 0, len(links))
	for _, l := range links {
		if imp.IntakeTaskID != nil && l.TaskID == *imp.IntakeTaskID {
			continue
		}
		out = append(out, l)
	}
	return out
}

// TaskCreated implements repository.TaskCreatedObserver: a task opened from
// an import's conversion chat is linked to that issue. Keyed on the chat, not
// on the worker's turn, so tasks the product manager opens after the person
// answers a question in the same chat are linked too.
func (s *Service) TaskCreated(ctx context.Context, task domain.BoardTask) {
	if s.imports == nil || s.links == nil {
		return
	}
	sessionID := registry.SessionIDFromContext(ctx)
	if sessionID == uuid.Nil {
		return
	}
	imp, err := s.imports.GetBySession(ctx, sessionID)
	if err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			log.Warn().Err(err).Str("session_id", sessionID.String()).Msg("issuesync: reading the chat's import failed")
		}
		return
	}
	if imp.IntakeTaskID != nil && *imp.IntakeTaskID == task.ID {
		return
	}
	link, err := s.links.Create(ctx, domain.IssueLink{
		TaskID:       task.ID,
		TaskKey:      task.Key,
		ImportedBy:   imp.ImportedBy,
		RepositoryID: task.RepositoryID,
		Provider:     imp.Provider,
		ExternalKey:  imp.ExternalKey,
		URL:          imp.URL,
		Title:        imp.Title,
		LastColumn:   string(task.Column),
	})
	if err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Str("issue", imp.ExternalKey).Msg("issuesync: linking a converted task failed")
		return
	}
	if imp.ConversionStatus == domain.IssueConversionConverting {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.writeBackTimeout)
		defer cancel()
		s.completeConversion(ctx, imp.ID, []domain.IssueLink{link})
	}()
}

// completeConversion retires the intake task and tells the issue which tasks
// carry it now. Once converted, a later task from the same chat only adds its
// own key.
func (s *Service) completeConversion(ctx context.Context, importID uuid.UUID, converted []domain.IssueLink) {
	if len(converted) == 0 {
		return
	}
	s.convMu.Lock()
	defer s.convMu.Unlock()
	imp, err := s.imports.Get(ctx, importID)
	if err != nil {
		log.Warn().Err(err).Str("import_id", importID.String()).Msg("issuesync: reading the import failed")
		return
	}
	if imp.ConversionStatus != domain.IssueConversionConverted {
		if imp.IntakeTaskID != nil {
			s.dropTask(ctx, imp.RepositoryID, *imp.IntakeTaskID)
			if err := s.imports.ClearIntakeTask(ctx, imp.ID); err != nil {
				log.Warn().Err(err).Str("issue", imp.ExternalKey).Msg("issuesync: clearing the intake task failed")
			}
		}
		s.setConversion(ctx, imp.ID, domain.IssueConversionConverted, "")
	}
	keys := make([]string, 0, len(converted))
	for _, l := range converted {
		keys = append(keys, l.TaskKey)
	}
	s.writeBackTracked(keys, converted[0])
}

// failConversion leaves the intake task as the issue's one task, and only now
// tells the issue about it: the tracked comment was held back for the
// product manager's tasks.
func (s *Service) failConversion(ctx context.Context, imp domain.IssueImport, status domain.IssueConversionStatus, reason string) {
	log.Warn().Str("issue", imp.ExternalKey).Str("status", string(status)).Str("reason", reason).
		Msg("issuesync: the product manager did not convert an imported issue")
	s.setConversion(ctx, imp.ID, status, truncateError(reason))
	if imp.IntakeTaskID == nil {
		return
	}
	link, err := s.links.GetByTask(ctx, *imp.IntakeTaskID)
	if err != nil {
		return
	}
	s.writeBackTracked([]string{link.TaskKey}, link)
}

func (s *Service) setConversion(ctx context.Context, importID uuid.UUID, status domain.IssueConversionStatus, errMsg string) {
	if err := s.imports.SetConversion(ctx, importID, status, nil, errMsg); err != nil {
		log.Warn().Err(err).Str("import_id", importID.String()).Msg("issuesync: recording conversion status failed")
	}
}

func truncateError(msg string) string {
	return domain.TruncateHead(strings.TrimSpace(msg), maxConversionErrorLen)
}

func providerName(p domain.IssueProvider) string {
	switch p {
	case domain.IssueProviderGitHub:
		return "GitHub"
	case domain.IssueProviderJira:
		return "Jira"
	default:
		return string(p)
	}
}
