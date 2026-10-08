package board

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type analysisUpdater struct {
	fakeTaskUpdater
	refs  []domain.AnalysisReference
	err   error
	calls int
}

func (a *analysisUpdater) AnalysisReferences(context.Context, uuid.UUID) ([]domain.AnalysisReference, error) {
	a.calls++
	if a.err != nil {
		return nil, a.err
	}
	return a.refs, nil
}

func implementationJob() RunJob {
	return runJobFor(domain.BoardTask{
		ID: uuid.New(), Key: "T-20", Title: "web export button",
		Column: domain.TaskColumnTodo, TaskType: "task",
	}, uuid.New())
}

func TestAnalysisContextCarriesTheSpecAndPlanIntoTheRun(t *testing.T) {
	updater := &analysisUpdater{refs: []domain.AnalysisReference{{
		TaskID: uuid.New(), Key: "A-12", Title: "CSV export",
		Documents: []domain.TaskDocument{
			{Title: "spec: 2026-08-17 export", Content: "Component: TaskExporter (application service)"},
			{Title: "plan: 2026-08-17 export", Content: "Task 2: TaskExporter service"},
		},
	}}}
	r := &Runner{taskUpdater: updater}

	msg := r.analysisContext(context.Background(), implementationJob())

	require.NotEmpty(t, msg)
	assert.Contains(t, msg, "A-12 (CSV export)", "the analiz task is named by its key")
	assert.Contains(t, msg, "spec: 2026-08-17 export")
	assert.Contains(t, msg, "Component: TaskExporter (application service)",
		"the document CONTENT reaches the run, not just its title")
	assert.Contains(t, msg, "Task 2: TaskExporter service")
	assert.Contains(t, msg, "list_task_documents", "and the tool that re-reads them is named")
	assert.Contains(t, msg, "NOT committed anywhere in the repository",
		"the agent is told where the spec is, so it does not go looking in docs/")
}

func TestAnalysisContextIsEmptyWithoutADerivedFromRelation(t *testing.T) {
	r := &Runner{taskUpdater: &analysisUpdater{}}
	assert.Empty(t, r.analysisContext(context.Background(), implementationJob()))
}

func TestAnalysisContextIsEmptyWhenTheServiceCannotAnswer(t *testing.T) {
	r := &Runner{taskUpdater: &fakeTaskUpdater{}}
	assert.Empty(t, r.analysisContext(context.Background(), implementationJob()))
}

func TestAnalysisContextSwallowsALookupFailure(t *testing.T) {
	r := &Runner{taskUpdater: &analysisUpdater{err: errors.New("db down")}}
	assert.Empty(t, r.analysisContext(context.Background(), implementationJob()))
}

func TestAnalysisContextSaysSoWhenTheAnalysisHasNoDocuments(t *testing.T) {
	updater := &analysisUpdater{refs: []domain.AnalysisReference{{
		TaskID: uuid.New(), Key: "A-12", Title: "CSV export",
	}}}
	r := &Runner{taskUpdater: updater}

	msg := r.analysisContext(context.Background(), implementationJob())

	assert.Contains(t, msg, "no documents attached")
	assert.Contains(t, msg, "A-12")
}

func TestAnalysisContextTruncatesAnEnormousDocument(t *testing.T) {
	updater := &analysisUpdater{refs: []domain.AnalysisReference{{
		TaskID: uuid.New(), Key: "A-12",
		Documents: []domain.TaskDocument{{
			Title:   "plan: huge",
			Content: strings.Repeat("x", analysisContextLimit*2),
		}},
	}}}
	r := &Runner{taskUpdater: updater}

	msg := r.analysisContext(context.Background(), implementationJob())

	assert.Less(t, len(msg), analysisContextLimit*2)
	assert.Contains(t, msg, "truncated")
	assert.Contains(t, msg, "list_task_documents with task_id A-12")
}

func TestAnalysisContextHandsTheApprovedDesignToTheTaskItBlocks(t *testing.T) {
	updater := &analysisUpdater{refs: []domain.AnalysisReference{
		{
			TaskID: uuid.New(), Key: "A-12", Title: "CSV export",
			Documents: []domain.TaskDocument{{Title: "spec: export", Content: "Component: TaskExporter"}},
		},
		{
			TaskID: uuid.New(), Key: "D-3", Title: "Export dialog", TaskType: domain.TaskTypeDesign, Column: domain.TaskColumnReleased,
			Documents: []domain.TaskDocument{
				{Title: "design: export dialog · A", Format: domain.DocumentFormatHTML, Content: "<html><body><h1>Export tasks</h1><p>Empty: No tasks to export yet.</p></body></html>"},
				{Title: "handoff: export dialog", Content: "Primary action: Button variant=primary, copy \"Export CSV\""},
			},
		},
	}}
	r := &Runner{taskUpdater: updater}

	msg := r.analysisContext(context.Background(), implementationJob())

	require.Contains(t, msg, "## The analysis this task came out of")
	require.Contains(t, msg, "## The approved design this task builds")
	assert.Contains(t, msg, "D-3 (Export dialog) (design task")
	assert.Less(t, strings.Index(msg, "## The analysis"), strings.Index(msg, "## The approved design"))
	assert.Less(t, strings.Index(msg, "handoff: export dialog"), strings.Index(msg, "design: export dialog · A"),
		"the hand-off spec leads: it is the implementer's contract")
	assert.Contains(t, msg, "No tasks to export yet.", "an HTML mockup reaches the run as text")
	assert.NotContains(t, msg, "<h1>")
}
