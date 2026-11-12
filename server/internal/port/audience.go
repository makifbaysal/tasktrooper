package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AudienceStore keeps audience research: audiences with their personas,
// studies with each persona's answers and the report, and the conversations
// with a persona. A missing row is domain.ErrAudienceNotFound,
// ErrStudyNotFound or ErrPersonaNotFound.
type AudienceStore interface {
	CreateAudience(ctx context.Context, a domain.Audience) (domain.Audience, error)
	GetAudience(ctx context.Context, id uuid.UUID) (domain.Audience, error)
	// ListAudiences answers newest first, with persona and study counts.
	ListAudiences(ctx context.Context) ([]domain.Audience, error)
	// SaveAudience writes the mutable columns: name, repository, status,
	// brief, target, product context, generation, error and built_at.
	SaveAudience(ctx context.Context, a domain.Audience) error
	DeleteAudience(ctx context.Context, id uuid.UUID) error
	// ReplacePersonas swaps the audience's personas for these, in one
	// transaction; studies keep their copy of who answered.
	ReplacePersonas(ctx context.Context, audienceID uuid.UUID, personas []domain.Persona) error
	ListPersonas(ctx context.Context, audienceID uuid.UUID) ([]domain.Persona, error)
	GetPersona(ctx context.Context, id uuid.UUID) (domain.Persona, error)

	CreateStudy(ctx context.Context, s domain.Study) (domain.Study, error)
	GetStudy(ctx context.Context, id uuid.UUID) (domain.Study, error)
	// ListStudies answers newest first; a nil audienceID lists every study.
	ListStudies(ctx context.Context, audienceID *uuid.UUID) ([]domain.Study, error)
	// SaveStudy writes the mutable columns, the report included.
	SaveStudy(ctx context.Context, s domain.Study) error
	DeleteStudy(ctx context.Context, id uuid.UUID) error
	// RuntimeChoice is the person's pick of provider and model for the calls.
	RuntimeChoice(ctx context.Context) (domain.AudienceRuntimeChoice, error)
	SaveRuntimeChoice(ctx context.Context, c domain.AudienceRuntimeChoice) error
	// BumpStudyProgress adds to the counters atomically and answers the totals.
	BumpStudyProgress(ctx context.Context, id uuid.UUID, done, failed int) (doneTotal, failedTotal int, err error)
	SaveAnswer(ctx context.Context, a domain.StudyAnswer) error
	ListAnswers(ctx context.Context, studyID uuid.UUID) ([]domain.StudyAnswer, error)
	// DeleteFailedAnswers removes the interviews that failed so they can be
	// asked again, and answers how many there were.
	DeleteFailedAnswers(ctx context.Context, studyID uuid.UUID) (int, error)

	AddMessages(ctx context.Context, msgs []domain.ChatMessage) error
	// ListMessages answers the persona's whole thread, oldest first.
	ListMessages(ctx context.Context, personaID uuid.UUID) ([]domain.ChatMessage, error)
	ListStudyMessages(ctx context.Context, studyID uuid.UUID) ([]domain.ChatMessage, error)
	ClearMessages(ctx context.Context, personaID uuid.UUID) error

	// FailInterrupted marks the audiences still building and the studies still
	// running or synthesizing as failed: a process that died took their worker.
	FailInterrupted(ctx context.Context, reason string) (audiences, studies int, err error)
}
