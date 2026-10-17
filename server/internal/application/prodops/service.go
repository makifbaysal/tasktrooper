package prodops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/application/notify"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type TaskBoard interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

type RepositoryResolver interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

type DeployHistory interface {
	RecentDeploys(ctx context.Context, repositoryID uuid.UUID, limit int) ([]domain.TaskPipeline, error)
}

// Notifier is satisfied by notify.Service. Whether and how often to notify is
// decided here (severity floor, re-notify cooldown); the notifier only delivers.
type Notifier interface {
	Notify(ctx context.Context, n domain.Notification)
}

type Deps struct {
	Incidents port.IncidentStore
	Targets   port.DeployTargetStore
	Repos     RepositoryResolver
	Tasks     TaskBoard
	Deploys   DeployHistory
	Notifier  Notifier
	// AlertTokens backs the token-authenticated ingest; nil disables it.
	AlertTokens port.AlertIngestTokenStore
	AlertLimits domain.AlertIngestLimits
}

type Service struct {
	incidents port.IncidentStore
	targets   port.DeployTargetStore
	repos     RepositoryResolver
	tasks     TaskBoard
	deploys   DeployHistory
	notifier  Notifier

	alertTokens port.AlertIngestTokenStore
	alertGuard  *alertIngestGuard

	attributor ReleaseAttributor
	rollbacks  ReleaseRollbackDispatcher

	now      func() time.Time
	notifyMu sync.Mutex
	cooldown *notify.Cooldown
	scope    func(ctx context.Context) string

	workflows port.WorkflowReader
	roles     port.RoleResolver
}

func (s *Service) SetWorkflows(w port.WorkflowReader)  { s.workflows = w }
func (s *Service) SetRoleResolver(r port.RoleResolver) { s.roles = r }

func (s *Service) defectTaskType(ctx context.Context) domain.TaskType {
	if s.workflows == nil {
		return ""
	}
	t, err := s.workflows.DefectTaskType(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("prodops: defect task type unavailable, opening as the default type")
		return ""
	}
	return t
}

func NewService(deps Deps) *Service {
	return &Service{
		incidents: deps.Incidents,
		targets:   deps.Targets,
		repos:     deps.Repos,
		tasks:     deps.Tasks,
		deploys:   deps.Deploys,
		notifier:  deps.Notifier,

		alertTokens: deps.AlertTokens,
		alertGuard:  newAlertIngestGuard(deps.AlertLimits),
		now:         time.Now,
	}
}

const taskSeverityFloor = domain.IncidentSeverityHigh

func withInputDefaults(in domain.IncidentInput) domain.IncidentInput {
	if in.Env == "" {
		in.Env = domain.DeployEnvProd
	}
	if in.Source == "" {
		in.Source = domain.IncidentSourceWebhook
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = "Production alert"
	}
	if in.Fingerprint == "" {
		in.Fingerprint = domain.IncidentFingerprint(in.Title, in.Env)
	}
	if in.Severity == "" {
		in.Severity = domain.IncidentSeverityMedium
	}
	return in
}

func (s *Service) Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error) {
	incident, _, err := s.ingest(ctx, in)
	return incident, err
}

func (s *Service) ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, bool, error) {
	if in.RepositoryID == uuid.Nil {
		return domain.Incident{}, false, errors.New("incident needs a repository")
	}
	in = withInputDefaults(in)

	if in.Resolved {
		incident, err := s.recover(ctx, in)
		return incident, false, err
	}

	incident, created, err := s.incidents.Upsert(ctx, in)
	if err != nil {
		return domain.Incident{}, false, err
	}
	if created {
		s.event(ctx, incident.ID, domain.IncidentEventDetected,
			fmt.Sprintf("%s incident detected via %s (%s)", incident.Env, incident.Source, incident.Severity))
	} else if incident.Occurrences <= 5 || incident.Occurrences%25 == 0 {

		s.event(ctx, incident.ID, domain.IncidentEventRecurred,
			fmt.Sprintf("recurred (%d occurrences)", incident.Occurrences))
	}

	policy := s.policy(ctx, incident.RepositoryID)
	remedy := s.buildRemedy(ctx, incident)

	if created || machineMayOverwriteRemedy(incident) {
		if updated, err := s.incidents.UpdateRemedy(ctx, incident.ID, remedy.Text(), remedy.Kind,
			domain.RemedyAuthorAutoTriage, remedy.Confidence); err != nil {
			log.Warn().Err(err).Str("incident_id", incident.ID.String()).Msg("persist incident remedy failed")
		} else {
			incident = updated
		}
		if created {
			s.event(ctx, incident.ID, domain.IncidentEventTriaged,
				fmt.Sprintf("first-pass remedy: %s (confidence %d)", remedy.Kind, remedy.Confidence))
		}
	}

	incident = s.attributeAndMaybeRollBack(ctx, incident)

	if policy != domain.IncidentPolicyOff && incident.TaskID == nil &&
		incident.Severity.Rank() >= taskSeverityFloor.Rank() {
		if updated, err := s.openRemediationTask(ctx, incident, remedy, policy); err != nil {
			log.Warn().Err(err).Str("incident_id", incident.ID.String()).Msg("open remediation task failed")
		} else {
			incident = updated
		}
	}

	if (created || incident.Severity == domain.IncidentSeverityCritical) && s.notifiable(incident) {
		n := heldIncidentNotice{notification: openedNotification(incident, remedy), incidentID: incident.ID, opened: true}
		if s.shouldRenotify(ctx, alertKey(incident), notifyOpened, n) {
			s.deliver(ctx, n)
		}
	}
	return incident, created, nil
}

func machineMayOverwriteRemedy(incident domain.Incident) bool {
	switch incident.RemedyAuthor {
	case domain.RemedyAuthorAutoTriage:
		return true
	case "":
		return incident.Status == domain.IncidentStatusOpen || incident.Status == domain.IncidentStatusTriaging
	default:
		return false
	}
}

const (
	notifyOpened    = "opened"
	notifyRecovered = "recovered"
)

// alertKey is the alert's identity, not the incident's: a flapping alert
// resolves and re-fires as a NEW incident every cycle. A repeat of the last
// kind told is held back (a critical incident recurring); an opening after a
// told recovery, or a recovery after a told opening, goes out, so the last
// word a person heard is never "recovered" while it is down — unless the
// alert has changed more than four times in half an hour. Then it is told
// nothing until it has stayed put for fifteen minutes, and then where it
// ended (FlushSettled).
func alertKey(incident domain.Incident) string {
	return incident.RepositoryID.String() + "\x00" + incident.Env + "\x00" + incident.Fingerprint
}

// heldIncidentNotice is an opening or recovery notification, kept by the
// cooldown while its alert flaps.
type heldIncidentNotice struct {
	notification domain.Notification
	incidentID   uuid.UUID
	opened       bool
}

func (s *Service) notifyCooldown() *notify.Cooldown {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	if s.cooldown == nil {
		s.cooldown = notify.NewCooldown(notify.DefaultCooldown).WithFlapLimit(notify.DefaultFlapLimit, notify.DefaultFlapSettle)
	}
	return s.cooldown
}

func (s *Service) shouldRenotify(ctx context.Context, key, kind string, n heldIncidentNotice) bool {
	return s.notifyCooldown().AllowTransitionHolding(s.scopeOf(ctx)+"\x00"+key, kind, s.now(), n)
}

func (s *Service) deliver(ctx context.Context, n heldIncidentNotice) {
	s.notifier.Notify(ctx, n.notification)
	if n.opened {
		s.event(ctx, n.incidentID, domain.IncidentEventNotified, "pushed to devices")
	}
}

// SetScope keys the notification cooldown by scope(ctx) as well, so a
// process serving several tenants delivers each one's held notifications
// under its own context (FlushSettled). Without it there is one scope. Call
// it before serving.
func (s *Service) SetScope(scope func(ctx context.Context) string) { s.scope = scope }

func (s *Service) scopeOf(ctx context.Context) string {
	if s.scope == nil {
		return ""
	}
	return s.scope(ctx)
}

// SettleInterval is how often Start delivers the final state of alerts that
// stopped flapping.
const SettleInterval = time.Minute

// Start runs FlushSettled every SettleInterval until ctx ends.
func (s *Service) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(SettleInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.FlushSettled(ctx)
			}
		}
	}()
}

// FlushSettled tells, once each, where the alerts of this scope whose
// flapping held their notifications back ended up.
func (s *Service) FlushSettled(ctx context.Context) {
	if s.notifier == nil {
		return
	}
	for _, h := range s.notifyCooldown().Settled(s.scopeOf(ctx)+"\x00", s.now()) {
		if n, ok := h.Payload.(heldIncidentNotice); ok {
			s.deliver(ctx, n)
		}
	}
}

func (s *Service) recover(ctx context.Context, in domain.IncidentInput) (domain.Incident, error) {
	live, err := s.incidents.FindLive(ctx, in.RepositoryID, in.Env, in.Fingerprint)
	if err != nil {
		if errors.Is(err, domain.ErrIncidentNotFound) {
			return domain.Incident{}, nil
		}
		return domain.Incident{}, err
	}
	resolved, err := s.incidents.UpdateStatus(ctx, live.ID, domain.IncidentStatusResolved)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, resolved.ID, domain.IncidentEventResolved, "recovery signal received from "+in.Source)
	if resolved.TaskID != nil && s.tasks != nil {
		if _, err := s.tasks.AddComment(ctx, resolved.RepositoryID, *resolved.TaskID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content:    prompt.Text(incidentRecoveredCommentKey),
		}); err != nil {
			log.Warn().Err(err).Msg("incident recovery comment failed")
		}
	}
	if s.notifiable(resolved) {
		n := heldIncidentNotice{notification: incidentNotification(domain.NotificationIncidentRecovered, resolved, "Recovered · "+resolved.Env, resolved.Title), incidentID: resolved.ID}
		if s.shouldRenotify(ctx, alertKey(resolved), notifyRecovered, n) {
			s.deliver(ctx, n)
		}
	}
	return resolved, nil
}

func (s *Service) IngestDeployFailure(ctx context.Context, repositoryID uuid.UUID, env, taskKey, detail string) (domain.Incident, error) {
	severity := domain.IncidentSeverityHigh
	if env == domain.DeployEnvProd {
		severity = domain.IncidentSeverityCritical
	}
	return s.Ingest(ctx, domain.IncidentInput{
		RepositoryID: repositoryID,
		Env:          env,
		Source:       domain.IncidentSourceDeploy,
		Severity:     severity,
		Title:        fmt.Sprintf("%s deploy failed", env),
		Detail:       strings.TrimSpace(taskKey + "\n" + detail),
		Fingerprint:  domain.IncidentFingerprint("deploy-failed", env),
		Payload:      map[string]any{"task": taskKey, "detail": detail},
	})
}

func (s *Service) buildRemedy(ctx context.Context, incident domain.Incident) domain.Remedy {
	rc := RemedyContext{Incident: incident}
	if s.targets != nil {

		if target, err := s.targets.Get(ctx, incident.RepositoryID, "", incident.Env); err == nil {
			rc.Target = target
			if tpl, ok := deploy.Template(firstNonEmpty(target.TemplateID, target.Provider)); ok {
				rc.Rollback = tpl.RollbackHint
			}
		}
	}
	if s.deploys != nil {
		if pipelines, err := s.deploys.RecentDeploys(ctx, incident.RepositoryID, 10); err == nil {
			for _, p := range pipelines {
				rc.Deploys = append(rc.Deploys, DeployRecord{
					Env:        deployEnvOfTrigger(p.Trigger),
					Status:     p.Status,
					FinishedAt: valueOrZero(p.FinishedAt),
					TaskKey:    p.Note,
				})
			}
		}
	}
	if s.incidents != nil {
		if history, err := s.incidents.History(ctx, incident.RepositoryID, incident.Fingerprint, 3); err == nil {
			rc.History = history
		}
	}
	return Suggest(rc)
}

func (s *Service) openRemediationTask(ctx context.Context, incident domain.Incident, remedy domain.Remedy, policy domain.IncidentPolicy) (domain.Incident, error) {
	if s.tasks == nil {
		return incident, errors.New("board is not available")
	}
	priority := domain.TaskPriorityHigh
	if incident.Severity == domain.IncidentSeverityCritical {
		priority = domain.TaskPriorityCritical
	}

	detail := strings.TrimSpace(incident.Detail)
	if detail != "" {
		detail = truncate(detail, 2000)
	}
	description := remediationTaskKey.Render(remediationTaskInput{
		Env:         incident.Env,
		Severity:    string(incident.Severity),
		Source:      string(incident.Source),
		Occurrences: incident.Occurrences,
		Title:       incident.Title,
		Detail:      detail,
		RemedyKind:  remedy.Kind,
		Confidence:  remedy.Confidence,
		RemedyText:  remedy.Text(),
		IncidentID:  incident.ID.String(),
		AutoFix:     policy == domain.IncidentPolicyAutoFix,
	})

	task, err := s.tasks.CreateTask(ctx, incident.RepositoryID, domain.CreateBoardTaskRequest{
		Title:           "Incident: " + truncate(incident.Title, 120),
		Description:     description,
		TaskType:        s.defectTaskType(ctx),
		Priority:        priority,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.systemTaskAssignee(ctx, incident.RepositoryID),
	})
	if err != nil {
		return incident, err
	}
	updated, err := s.incidents.AttachTask(ctx, incident.ID, task.ID)
	if err != nil {
		return incident, err
	}
	s.event(ctx, incident.ID, domain.IncidentEventTaskCreated, "remediation task "+task.Key+" opened ("+string(policy)+")")
	if triaging, err := s.incidents.UpdateStatus(ctx, incident.ID, domain.IncidentStatusTriaging); err == nil {
		triaging.TaskID = updated.TaskID
		updated = triaging
	}
	return updated, nil
}

func (s *Service) ProposeRemedy(ctx context.Context, incidentID uuid.UUID, remedy domain.Remedy, author string) (domain.Incident, error) {
	if remedy.Kind == "" {
		remedy.Kind = domain.RemedyKindUnknown
	}
	if strings.TrimSpace(remedy.Summary) == "" {
		return domain.Incident{}, errors.New("remedy needs a summary")
	}

	if strings.TrimSpace(author) == "" {
		author = domain.RemedyAuthorHuman
	}
	incident, err := s.incidents.UpdateRemedy(ctx, incidentID, remedy.Text(), remedy.Kind, author, remedy.Confidence)
	if err != nil {
		return domain.Incident{}, err
	}
	status := domain.IncidentStatusProposed
	if s.policy(ctx, incident.RepositoryID) == domain.IncidentPolicyAutoFix {
		status = domain.IncidentStatusFixing
	}
	if updated, err := s.incidents.UpdateStatus(ctx, incidentID, status); err == nil {
		incident = updated
	}
	s.event(ctx, incidentID, domain.IncidentEventProposed, author+" proposed a "+remedy.Kind+" remedy")
	// Whoever typed the proposal by hand does not need telling about it.
	if author != domain.RemedyAuthorHuman && s.notifiable(incident) {
		s.notifier.Notify(ctx, incidentNotification(domain.NotificationRemedyProposed, incident, "Fix proposed · "+incident.Env, truncate(remedy.Summary, 200)))
	}
	return incident, nil
}

func (s *Service) Triage(ctx context.Context, incidentID uuid.UUID) (domain.Incident, error) {
	incident, err := s.incidents.Get(ctx, incidentID)
	if err != nil {
		return domain.Incident{}, err
	}
	remedy := s.buildRemedy(ctx, incident)
	updated, err := s.incidents.UpdateRemedy(ctx, incidentID, remedy.Text(), remedy.Kind,
		domain.RemedyAuthorAutoTriage, remedy.Confidence)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, incidentID, domain.IncidentEventTriaged, fmt.Sprintf("re-triaged: %s (confidence %d)", remedy.Kind, remedy.Confidence))
	return updated, nil
}

func (s *Service) Resolve(ctx context.Context, incidentID uuid.UUID, note string) (domain.Incident, error) {
	incident, err := s.incidents.UpdateStatus(ctx, incidentID, domain.IncidentStatusResolved)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, incidentID, domain.IncidentEventResolved, firstNonEmpty(note, "resolved"))
	return incident, nil
}

func (s *Service) Ignore(ctx context.Context, incidentID uuid.UUID, note string) (domain.Incident, error) {
	incident, err := s.incidents.UpdateStatus(ctx, incidentID, domain.IncidentStatusIgnored)
	if err != nil {
		return domain.Incident{}, err
	}
	s.event(ctx, incidentID, domain.IncidentEventIgnored, firstNonEmpty(note, "muted"))
	return incident, nil
}

func (s *Service) List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error) {
	return s.incidents.List(ctx, filter)
}

func (s *Service) Get(ctx context.Context, incidentID uuid.UUID) (domain.Incident, error) {
	return s.incidents.Get(ctx, incidentID)
}

func (s *Service) ByTask(ctx context.Context, taskID uuid.UUID) (domain.Incident, error) {
	return s.incidents.ByTask(ctx, taskID)
}

func openedNotification(incident domain.Incident, remedy domain.Remedy) domain.Notification {
	title := fmt.Sprintf("%s · %s", strings.ToUpper(incident.Env), truncate(incident.Title, 80))
	return incidentNotification(domain.NotificationIncidentOpened, incident, title, truncate(remedy.Summary, 200))
}

// notifiable keeps the follow-ups (recovered, fix proposed) to the incidents
// whose opening was loud enough to notify in the first place.
func (s *Service) notifiable(incident domain.Incident) bool {
	return s.notifier != nil && incident.Severity.Rank() >= taskSeverityFloor.Rank()
}

func incidentNotification(kind domain.NotificationKind, incident domain.Incident, title, body string) domain.Notification {
	return domain.Notification{
		Kind:     kind,
		Title:    title,
		Body:     body,
		Severity: incident.Severity,
		Link:     domain.IncidentLink(incident.ID),
		Source:   domain.NotificationSourceIncident,
		SourceID: incident.ID.String(),
	}
}

func (s *Service) policy(ctx context.Context, repositoryID uuid.UUID) domain.IncidentPolicy {
	if s.repos == nil {
		return domain.IncidentPolicySuggest
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil || !domain.ValidIncidentPolicy(repo.IncidentPolicy) {
		return domain.IncidentPolicySuggest
	}
	return repo.IncidentPolicy
}

func (s *Service) systemTaskAssignee(ctx context.Context, repositoryID uuid.UUID) *uuid.UUID {
	if s.roles == nil || s.repos == nil {
		return nil
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return nil
	}
	area := domain.RepoArea(repo.Kind, repo.SubProjects)
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeSystemTaskAssignee, area)
	if err != nil {
		return nil
	}
	return id
}

func (s *Service) event(ctx context.Context, incidentID uuid.UUID, kind, message string) {
	if err := s.incidents.AppendEvent(ctx, incidentID, kind, message); err != nil {
		log.Warn().Err(err).Str("incident_id", incidentID.String()).Str("kind", kind).Msg("append incident event failed")
	}
}

func deployEnvOfTrigger(trigger domain.PipelineTrigger) string {
	switch trigger {
	case domain.PipelineTriggerStageDeploy:
		return domain.DeployEnvStage
	case domain.PipelineTriggerPreProdDeploy:
		return domain.DeployEnvPreProd
	case domain.PipelineTriggerProdDeploy:
		return domain.DeployEnvProd
	}
	return ""
}

func valueOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
