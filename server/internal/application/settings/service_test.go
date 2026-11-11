package settings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeSettingsStore struct {
	got        domain.AppSettings
	updateReq  domain.UpdateSettingsRequest
	updateCall int
}

func (f *fakeSettingsStore) Get(context.Context) (domain.AppSettings, error) { return f.got, nil }

func (f *fakeSettingsStore) Update(_ context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	f.updateCall++
	f.updateReq = req
	return f.got, nil
}

func TestGetReturnsStoreSettings(t *testing.T) {
	store := &fakeSettingsStore{got: domain.AppSettings{WorkspaceRoot: "/tmp/ws"}}
	svc := settings.NewService(store)

	out, err := svc.Get(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "/tmp/ws", out.WorkspaceRoot)
}

func TestUpdateDelegatesToStore(t *testing.T) {
	store := &fakeSettingsStore{}
	svc := settings.NewService(store)

	_, err := svc.Update(context.Background(), domain.UpdateSettingsRequest{DefaultLanguage: "tr"})

	require.NoError(t, err)
	assert.Equal(t, 1, store.updateCall)
	assert.Equal(t, "tr", store.updateReq.DefaultLanguage)
}

type fakeSwitch struct {
	enabled *bool
	wf      domain.Workflow
}

func (f *fakeSwitch) SetPMUATEnabled(e bool) { f.enabled = &e }
func (f *fakeSwitch) StoredWorkflow(domain.TaskType) (domain.Workflow, error) {
	return f.wf, nil
}

type fakeMover struct {
	tasks []domain.BoardTask
	moved map[uuid.UUID]domain.TaskColumn
	swIn  *fakeSwitch
	offAt bool
}

func (f *fakeMover) ListAllTasks(context.Context) ([]domain.BoardTask, error) { return f.tasks, nil }
func (f *fakeMover) UpdateTask(_ context.Context, _, id uuid.UUID, req domain.UpdateBoardTaskRequest) (domain.BoardTask, error) {
	f.offAt = f.swIn.enabled != nil && !*f.swIn.enabled
	if f.moved == nil {
		f.moved = map[uuid.UUID]domain.TaskColumn{}
	}
	f.moved[id] = *req.Column
	return domain.BoardTask{}, nil
}

func TestTurningPMUATOffMovesStrandedTasksToTheStageExit(t *testing.T) {
	inUAT := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Column: domain.TaskColumnPMUAT, TaskType: "task"}
	other := domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Column: domain.TaskColumnInQA, TaskType: "task"}
	sw := &fakeSwitch{wf: domain.Workflow{Stages: []domain.WorkflowStage{{Column: domain.TaskColumnPMUAT, Behaviours: []domain.BehaviourRef{
		{Key: domain.BehaviourReviewVerdictSweep, Params: map[string]string{"pass_to": "human_uat"}}}}}}}
	mv := &fakeMover{tasks: []domain.BoardTask{inUAT, other}, swIn: sw}
	off := false
	svc := settings.NewService(&fakeSettingsStore{got: domain.AppSettings{PMUATEnabled: false}})
	svc.SetStageSwitch(sw)
	svc.SetTaskMover(mv)

	_, err := svc.Update(context.Background(), domain.UpdateSettingsRequest{PMUATEnabled: &off})

	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]domain.TaskColumn{inUAT.ID: domain.TaskColumnHumanUAT}, mv.moved)
	assert.True(t, mv.offAt, "switch flipped before the move")
}

func TestTurningPMUATOnMovesNothing(t *testing.T) {
	sw := &fakeSwitch{}
	mv := &fakeMover{tasks: []domain.BoardTask{{ID: uuid.New(), Column: domain.TaskColumnPMUAT}}, swIn: sw}
	svc := settings.NewService(&fakeSettingsStore{got: domain.AppSettings{PMUATEnabled: true}})
	svc.SetStageSwitch(sw)
	svc.SetTaskMover(mv)
	on := true

	_, err := svc.Update(context.Background(), domain.UpdateSettingsRequest{PMUATEnabled: &on})

	require.NoError(t, err)
	assert.Empty(t, mv.moved)
	require.NotNil(t, sw.enabled)
	assert.True(t, *sw.enabled)
}
