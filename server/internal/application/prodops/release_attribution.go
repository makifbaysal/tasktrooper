package prodops

import (
	"context"
	"fmt"
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
	attribution, ok := s.attributor.AttributeRelease(ctx, incident.RepositoryID, incident.Env, onset)
	if !ok {
		return incident
	}

	autoRollback := attribution.AutoRollback
	s.event(ctx, incident.ID, domain.IncidentEventTriaged, releaseAttributionNote(attribution, s.attributor.HealthWindow(), autoRollback))

	if s.tasks != nil {
		if _, err := s.tasks.AddComment(ctx, incident.RepositoryID, attribution.TaskID, domain.CreateTaskCommentRequest{
			AuthorType: "system",
			Content: fmt.Sprintf("Production incident inside this release's health window.\n\n%s\n\nIncident: %s (%s, %s)\n%s",
				releaseAttributionNote(attribution, s.attributor.HealthWindow(), autoRollback),
				incident.Title, incident.Env, incident.Severity, strings.TrimSpace(incident.Detail)),
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
	note := fmt.Sprintf("Attributed to release %s (%s): its merge commit %s is what %s is running, deployed %s ago — inside the %s post-release window.",
		a.TaskKey, a.Title, domain.ShortSHA(a.MergeSHA), a.Env, humanDuration(gap), humanDuration(window))
	if autoRollback {
		return note + " auto_rollback is ON in this release's delivery profile: the rollback is being executed."
	}
	return note + " auto_rollback is OFF in this release's delivery profile: the rollback is proposed and needs a human to confirm it."
}
