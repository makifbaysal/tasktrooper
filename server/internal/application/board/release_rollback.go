package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ReleaseRollbackDispatcher struct {
	dispatcher *Dispatcher
	tasks      ReleaseRollbackBoard
}

type ReleaseRollbackBoard interface {
	TaskRunbookReader
	AddComment(ctx context.Context, repositoryID, taskID uuid.UUID, req domain.CreateTaskCommentRequest) (domain.TaskComment, error)
}

func NewReleaseRollbackDispatcher(dispatcher *Dispatcher, tasks ReleaseRollbackBoard) *ReleaseRollbackDispatcher {
	return &ReleaseRollbackDispatcher{dispatcher: dispatcher, tasks: tasks}
}

func (d *ReleaseRollbackDispatcher) DispatchReleaseRollback(ctx context.Context, attribution domain.ReleaseAttribution, incident domain.Incident, autoRollback bool) error {
	if d == nil || d.dispatcher == nil || d.tasks == nil {
		return nil
	}
	task, err := d.tasks.GetTask(ctx, incident.RepositoryID, attribution.TaskID)
	if err != nil {
		return fmt.Errorf("release rollback: reading the attributed task: %w", err)
	}
	// Only a released card can roll back; anything else is refused.
	if task.Column != domain.TaskColumnDone && task.Column != domain.TaskColumnReleased {
		log.Info().Str("task_id", task.ID.String()).Str("column", string(task.Column)).
			Msg("release rollback: attributed task is no longer in a released column, not dispatching")
		return nil
	}

	if _, err := d.tasks.AddComment(ctx, incident.RepositoryID, task.ID, domain.CreateTaskCommentRequest{
		AuthorType: "system",
		Content:    releaseRollbackRunbook(task, incident, autoRollback),
	}); err != nil {
		log.Warn().Err(err).Str("task_id", task.ID.String()).Msg("release rollback: runbook comment failed")
	}

	return d.dispatcher.Dispatch(ctx, DispatchInput{
		RepositoryID: incident.RepositoryID,
		Task:         task,
		EventType:    domain.BoardEventTaskMoved,
		Payload: map[string]interface{}{
			"release_rollback":                 true,
			"incident_id":                      incident.ID.String(),
			"auto_rollback":                    autoRollback,
			domain.EventPayloadResumedResource: domain.ResourceDeployWatch,
			domain.EventPayloadActor:           domain.EventActorSystem,
			domain.EventPayloadReason:          domain.MoveReasonResourceFree,
		},
	})
}

func releaseRollbackRunbook(task domain.BoardTask, incident domain.Incident, autoRollback bool) string {
	var sb strings.Builder
	sb.WriteString("ROLLBACK REQUIRED — this task's release is what production is running, and production is unhealthy.\n\n")
	sb.WriteString(fmt.Sprintf("Incident: %s (%s, severity %s)\n", incident.Title, incident.Env, incident.Severity))
	if detail := strings.TrimSpace(incident.Detail); detail != "" {
		sb.WriteString(detail + "\n")
	}
	sb.WriteString(fmt.Sprintf("Released commit: %s\n\n", domain.ShortSHA(task.MergeCommitSHA)))

	if runbook := domain.TaskRollbackRunbook(task); runbook != "" {
		sb.WriteString(runbook)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("This task recorded NO rollback plan. Say so explicitly when you report — the absence is itself a finding for the next release.\n\n")
	}

	if autoRollback {
		sb.WriteString("auto_rollback is ON in this release's delivery profile: call rollback_release with reason=health_incident. " +
			"It will undo the code — by re-deploying the last good commit where a deploy workflow exists, or by reverting the merge commit on the default branch where the host deploys on push. ")
	} else {
		sb.WriteString("auto_rollback is OFF in this release's delivery profile: call rollback_release with reason=health_incident anyway — it will execute NOTHING and return the written-up proposal (`proposed: true`). " +
			"That is the correct outcome here. Post what it returns on this task, say plainly that a human has to confirm it, and stop. Do not look for another way to roll production back. ")
	}
	sb.WriteString("Then work through the plan above yourself and report every step you performed AND every step you could not — a schema change, a feature flag, anything with a human on the other end. " +
		"Do not report the rollback as complete unless it is.")
	return sb.String()
}
