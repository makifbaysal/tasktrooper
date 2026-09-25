package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fakePackageTaskStore is a minimal in-memory BoardTaskStore shared by several
// test fixtures in this package (assignee_test.go, ready_test.go,
// workorder_test.go, workorder_wake_test.go) that embed it and override the
// handful of methods each test actually exercises.
type fakePackageTaskStore struct {
	tasks map[uuid.UUID]domain.BoardTask
}

func (f *fakePackageTaskStore) Get(_ context.Context, _ uuid.UUID, taskID uuid.UUID) (domain.BoardTask, error) {
	task, ok := f.tasks[taskID]
	if !ok {
		return domain.BoardTask{}, fmt.Errorf("task %s not found", taskID)
	}
	return task, nil
}
func (f *fakePackageTaskStore) Update(_ context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	f.tasks[task.ID] = task
	return task, nil
}
func (f *fakePackageTaskStore) Create(context.Context, domain.BoardTask) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) GetByNumber(context.Context, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) LookupByKey(context.Context, string, int) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) ListByRepository(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *fakePackageTaskStore) ListAll(context.Context) ([]domain.BoardTask, error) {
	out := make([]domain.BoardTask, 0, len(f.tasks))
	for _, task := range f.tasks {
		out = append(out, task)
	}
	return out, nil
}

func (f *fakePackageTaskStore) ListBoardVisible(ctx context.Context, _ time.Time) ([]domain.BoardTask, error) {
	return f.ListAll(ctx)
}

func (f *fakePackageTaskStore) ListReleasedArchive(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}
func (f *fakePackageTaskStore) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakePackageTaskStore) ClaimAssignee(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (domain.BoardTask, error) {
	return domain.BoardTask{}, nil
}
func (f *fakePackageTaskStore) MarkCompleted(context.Context, uuid.UUID, bool, time.Time) error {
	return nil
}
func (f *fakePackageTaskStore) SetMigrationFlag(context.Context, uuid.UUID, bool) error { return nil }
func (f *fakePackageTaskStore) SetTaskPullRequest(context.Context, uuid.UUID, string, int) error {
	return nil
}
func (f *fakePackageTaskStore) SetTaskMergeCommit(context.Context, uuid.UUID, string) error {
	return nil
}
func (f *fakePackageTaskStore) MarkStageVerified(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (f *fakePackageTaskStore) ClearStageVerification(context.Context, uuid.UUID) error { return nil }
func (f *fakePackageTaskStore) NextTaskNumber(context.Context, domain.TaskType) (int, error) {
	return 1, nil
}
func (f *fakePackageTaskStore) BlockOnQuestion(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (f *fakePackageTaskStore) TakeBlockedBySession(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
func (f *fakePackageTaskStore) BlockOnResource(context.Context, uuid.UUID, uuid.UUID, string, string) (domain.TaskColumn, error) {
	return "", nil
}

func (f *fakePackageTaskStore) MarkWorkOrderWaiting(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakePackageTaskStore) ClearWorkOrderWaiting(context.Context, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakePackageTaskStore) TakeBlockedByResource(context.Context, string) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakePackageTaskStore) TakeQuotaResumable(context.Context, time.Time) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}

func (f *fakePackageTaskStore) BlockOnCancel(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (f *fakePackageTaskStore) FindTaskByMergeCommit(context.Context, uuid.UUID, string) (domain.BoardTask, error) {
	return domain.BoardTask{}, errors.New("not found")
}

func (f *fakePackageTaskStore) ListBlockedByResource(context.Context, string, int) ([]domain.BoardTask, error) {
	return nil, nil
}

func (f *fakePackageTaskStore) TakeBlockedResourceTask(context.Context, string, uuid.UUID) (domain.BoardTask, bool, error) {
	return domain.BoardTask{}, false, nil
}
