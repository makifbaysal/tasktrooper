package prodops

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ReleaseAttributor interface {
	AttributeRelease(ctx context.Context, repositoryID uuid.UUID, env string, onset time.Time) (domain.ReleaseAttribution, bool)

	HealthWindow() time.Duration
}

// IncidentAttributor is a ReleaseAttributor that reads the incident itself:
// a component its payload names narrows which releases may be blamed, so a
// monorepo part's alert is never pinned on another part's deploy.
type IncidentAttributor interface {
	AttributeIncident(ctx context.Context, incident domain.Incident, onset time.Time) (domain.ReleaseAttribution, bool)
}

type ReleaseRollbackDispatcher interface {
	DispatchReleaseRollback(ctx context.Context, attribution domain.ReleaseAttribution, incident domain.Incident, autoRollback bool) error
}

func (s *Service) SetReleaseAttributor(a ReleaseAttributor) { s.attributor = a }

func (s *Service) SetReleaseRollbackDispatcher(d ReleaseRollbackDispatcher) { s.rollbacks = d }

func (s *Service) attributeAndMaybeRollBack(ctx context.Context, incident domain.Incident) domain.Incident {
	if s.attributor == nil || incident.RepositoryID == uuid.Nil {
		return incident
	}
	onset := incident.FirstSeenAt
	if onset.IsZero() {
		onset = incident.LastSeenAt
	}
	var attribution domain.ReleaseAttribution
	var ok bool
	if ia, isIncident := s.attributor.(IncidentAttributor); isIncident {
		attribution, ok = ia.AttributeIncident(ctx, incident, onset)
	} else {
		attribution, ok = s.attributor.AttributeRelease(ctx, incident.RepositoryID, incident.Env, onset)
	}
	if !ok {
		return incident
	}

	autoRollback := attribution.AutoRollback
	s.event(ctx, incident.ID, domain.IncidentEventTriaged, releaseAttributionNote(attribution, s.attributor.HealthWindow(), autoRollback))

	if s.tasks != nil {
		if _, err := s.tasks.AddComment(ctx, incident.RepositoryID, attribution.TaskID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content: releaseAttributionCommentKey.Render(releaseAttributionCommentInput{
				Note:     releaseAttributionNote(attribution, s.attributor.HealthWindow(), autoRollback),
				Title:    incident.Title,
				Env:      incident.Env,
				Severity: string(incident.Severity),
				Detail:   strings.TrimSpace(incident.Detail),
			}),
		}); err != nil {
			log.Warn().Err(err).Str("task_id", attribution.TaskID.String()).Msg("release attribution comment failed")
		}
	}

	if s.rollbacks == nil {
		return incident
	}
	if err := s.rollbacks.DispatchReleaseRollback(ctx, attribution, incident, autoRollback); err != nil {
		log.Warn().Err(err).Str("task_id", attribution.TaskID.String()).Str("incident_id", incident.ID.String()).
			Msg("dispatching the release rollback failed")
	}
	return incident
}

func releaseAttributionNote(a domain.ReleaseAttribution, window time.Duration, autoRollback bool) string {
	gap := time.Since(a.DeployedAt).Round(time.Minute)
	return releaseAttributionNoteKey.Render(releaseAttributionNoteInput{
		TaskKey: a.TaskKey, Title: a.Title, MergeSHA: domain.ShortSHA(a.MergeSHA), Env: a.Env,
		Gap: humanDuration(gap), Window: humanDuration(window), AutoRollback: autoRollback,
	})
}
