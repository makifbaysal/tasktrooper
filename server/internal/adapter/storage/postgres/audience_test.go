package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestAudienceStoreKeepsAudiencesPersonasStudiesAnswersAndThreads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pg, err := newTestDatabase(ctx)
	require.NoError(t, err)
	defer pg.Stop()
	pool, err := pgxpool.New(ctx, pg.DSN())
	require.NoError(t, err)
	defer pool.Close()
	store := postgres.NewAudienceStore(postgres.NewDB(pool))

	a, err := store.CreateAudience(ctx, domain.Audience{
		Name: "Designers", PersonaTarget: 6,
		Brief: domain.AudienceBrief{Product: "A kanban for agents", Segments: []domain.AudienceSegment{{Name: "Freelancers", Count: 4}}, Language: "Turkish"},
	})
	require.NoError(t, err)
	assert.Equal(t, domain.AudienceDraft, a.Status)
	assert.Equal(t, "Turkish", a.Brief.Language)

	a.Status, a.ProductContext, a.Generation = domain.AudienceReady, "Go API", 1
	require.NoError(t, store.SaveAudience(ctx, a))
	require.NoError(t, store.ReplacePersonas(ctx, a.ID, []domain.Persona{
		{Name: "Ayse", Segment: "Freelancers", Generation: 1, PersonaProfile: domain.PersonaProfile{Age: 31, Role: "UX", Goals: []string{"clients"}}},
		{Name: "Can", Segment: "Freelancers", Generation: 1},
	}))
	got, err := store.GetAudience(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.PersonaCount)
	assert.Equal(t, "Go API", got.ProductContext)

	personas, err := store.ListPersonas(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, personas, 2)
	assert.Equal(t, "Ayse", personas[0].Name)
	assert.Equal(t, 1, personas[0].Ordinal)
	assert.Equal(t, []string{"clients"}, personas[0].Goals)

	st, err := store.CreateStudy(ctx, domain.Study{AudienceID: a.ID, Title: "Would they pay?", Questions: []string{"why?"}, PricePoints: []float64{9, 19.5}, Currency: "USD", PersonaTotal: 2, CallsEstimate: 3})
	require.NoError(t, err)
	assert.Equal(t, []float64{9, 19.5}, st.PricePoints)

	pid := personas[0].ID
	require.NoError(t, store.SaveAnswer(ctx, domain.StudyAnswer{StudyID: st.ID, PersonaID: &pid, Persona: domain.PersonaSnapshot{Name: "Ayse", Ordinal: 1}, Answers: domain.StudyAnswers{IntentScore: 7, Verdict: "yes"}}))
	done, failed, err := store.BumpStudyProgress(ctx, st.ID, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, [2]int{1, 0}, [2]int{done, failed})

	st.Status = domain.StudyDone
	st.Report = &domain.Report{Summary: "ok", Metrics: domain.ReportMetrics{Respondents: 1, IntentDistribution: make([]int, 11)}}
	require.NoError(t, store.SaveStudy(ctx, st))
	st, err = store.GetStudy(ctx, st.ID)
	require.NoError(t, err)
	require.NotNil(t, st.Report)
	assert.Equal(t, "ok", st.Report.Summary)

	listed, err := store.ListStudies(ctx, &a.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "Designers", listed[0].AudienceName)
	all, err := store.ListStudies(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	group := st.ID
	require.NoError(t, store.AddMessages(ctx, []domain.ChatMessage{
		{PersonaID: pid, Role: "user", Content: "hello"},
		{PersonaID: pid, StudyID: &st.ID, GroupID: &group, Role: "persona", Content: "hi"},
	}))
	thread, err := store.ListMessages(ctx, pid)
	require.NoError(t, err)
	assert.Len(t, thread, 2)
	studyThread, err := store.ListStudyMessages(ctx, st.ID)
	require.NoError(t, err)
	assert.Len(t, studyThread, 1)

	// regenerating the people erases their threads but not what the study found
	require.NoError(t, store.ReplacePersonas(ctx, a.ID, []domain.Persona{{Name: "Deniz", Generation: 2}}))
	answers, err := store.ListAnswers(ctx, st.ID)
	require.NoError(t, err)
	require.Len(t, answers, 1)
	assert.Nil(t, answers[0].PersonaID)
	assert.Equal(t, "Ayse", answers[0].Persona.Name)
	assert.Equal(t, 7, answers[0].Answers.IntentScore)
	_, err = store.GetPersona(ctx, pid)
	assert.ErrorIs(t, err, domain.ErrPersonaNotFound)

	// a dead process leaves nothing running
	running, err := store.CreateStudy(ctx, domain.Study{AudienceID: a.ID, Title: "again", Status: domain.StudyRunning, Currency: "USD"})
	require.NoError(t, err)
	a.Status = domain.AudienceBuilding
	require.NoError(t, store.SaveAudience(ctx, a))
	na, ns, err := store.FailInterrupted(ctx, "stopped")
	require.NoError(t, err)
	assert.Equal(t, [2]int{1, 1}, [2]int{na, ns})
	r, _ := store.GetStudy(ctx, running.ID)
	assert.Equal(t, domain.StudyFailed, r.Status)

	require.NoError(t, store.DeleteAudience(ctx, a.ID))
	_, err = store.GetStudy(ctx, st.ID)
	assert.ErrorIs(t, err, domain.ErrStudyNotFound, "the studies go with the audience")
	_, err = store.GetAudience(ctx, a.ID)
	assert.ErrorIs(t, err, domain.ErrAudienceNotFound)
}
