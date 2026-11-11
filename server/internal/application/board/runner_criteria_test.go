package board

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestCriteriaForRunListsEveryCriterionForReviewColumns(t *testing.T) {
	ticked := []domain.AcceptanceCriterion{
		{ID: uuid.New(), Text: "stage login accepts a valid password", Completed: true},
		{ID: uuid.New(), Text: "a wrong password shows the error banner", Completed: true},
	}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnCodeReview, domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
		domain.TaskColumnHumanUAT,
	} {
		r := NewRunner(RunnerDeps{})
		r.SetTaskUpdater(&criteriaUpdater{criteria: ticked})
		job := RunJob{Task: domain.BoardTask{ID: uuid.New(), Title: "t", Column: col}, RepositoryID: uuid.New()}

		got := r.criteriaForRun(context.Background(), job, taskWF)
		require.Len(t, got, len(ticked), "column %s: a reviewing run must see every criterion, ticked or not", col)

		msg := buildTriggerMessage(job, taskWF, got, nil)
		for _, c := range ticked {
			if !strings.Contains(msg, c.ID.String()) {
				t.Errorf("column %s: criterion id %s is missing, so review_criterion cannot be called:\n%s", col, c.ID, msg)
			}
			if !strings.Contains(msg, c.Text) {
				t.Errorf("column %s: criterion text is missing:\n%s", col, msg)
			}
		}
	}
}

func TestTriggerMessageTellsReviewersToRecordAVerdictOnEachID(t *testing.T) {
	criteria := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "criterion", Completed: true}}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnReadyForQA, domain.TaskColumnInQA,
	} {
		msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: col}}, taskWF, criteria, nil)
		for _, want := range []string{
			"review_criterion",
			"The forward move is refused while any id lacks your verdict on repositories that require criteria.",
			"Never move the task to need_revision just to look for these ids",
			"A verdict already on an id survives a revision round",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("column %s: trigger message is missing %q:\n%s", col, want, msg)
			}
		}
		if strings.Contains(msg, "Open acceptance criteria") {
			t.Errorf("column %s: reviewer got the implementer's open-only checklist:\n%s", col, msg)
		}
	}
}

func TestTriggerMessageNamesTheCriteriaAsTheDiffsTargetInCodeReview(t *testing.T) {
	criteria := []domain.AcceptanceCriterion{{ID: uuid.New(), Text: "criterion", Completed: true}}
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnCodeReview}}, taskWF, criteria, nil)

	require.Contains(t, msg, "Acceptance criteria the diff must satisfy (ids for reference)")
	require.NotContains(t, msg, "call set_criterion_completed", "the reviewer does not tick the developer's boxes")
}

func TestTriggerMessageShowsWhoHasSaidWhatAboutEachCriterion(t *testing.T) {
	c := domain.AcceptanceCriterion{
		ID: uuid.New(), Text: "the banner appears", Completed: true,
		Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true}},
	}
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnInQA}}, taskWF,
		[]domain.AcceptanceCriterion{c}, nil)

	want := "- [" + c.ID.String() + "] the banner appears — implementer: ticked; qa: approved"
	if !strings.Contains(msg, want) {
		t.Fatalf("criterion state line is missing %q:\n%s", want, msg)
	}
}

func TestTriggerMessageShowsARejectionWithItsNote(t *testing.T) {
	c := domain.AcceptanceCriterion{
		ID: uuid.New(), Text: "the banner appears", Completed: true,
		Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: false, Note: "banner never rendered"}},
	}
	msg := buildTriggerMessage(RunJob{Task: domain.BoardTask{Title: "t", Column: domain.TaskColumnInQA}}, taskWF,
		[]domain.AcceptanceCriterion{c}, nil)

	require.Contains(t, msg, "qa: rejected (banner never rendered)")
}

func TestCriteriaForRunListsOnlyOpenCriteriaForImplementers(t *testing.T) {
	done := domain.AcceptanceCriterion{ID: uuid.New(), Text: "already implemented", Completed: true}
	open := domain.AcceptanceCriterion{ID: uuid.New(), Text: "still to do", Completed: false}
	for _, col := range []domain.TaskColumn{
		domain.TaskColumnTodo, domain.TaskColumnInProgress, domain.TaskColumnNeedRevision,
	} {
		r := NewRunner(RunnerDeps{})
		r.SetTaskUpdater(&criteriaUpdater{criteria: []domain.AcceptanceCriterion{done, open}})
		job := RunJob{Task: domain.BoardTask{ID: uuid.New(), Title: "t", Column: col}, RepositoryID: uuid.New()}

		got := r.criteriaForRun(context.Background(), job, taskWF)
		require.Len(t, got, 1, "column %s: an implementer sees only what is still open", col)
		require.Equal(t, open.ID, got[0].ID)

		msg := buildTriggerMessage(job, taskWF, got, nil)
		if !strings.Contains(msg, "Open acceptance criteria") {
			t.Errorf("column %s: implementer lost the open-criteria checklist:\n%s", col, msg)
		}
		if strings.Contains(msg, done.ID.String()) {
			t.Errorf("column %s: a ticked criterion is back in the implementer's list:\n%s", col, msg)
		}
	}
}

func TestCriteriaForRunKeepsAnalizBehaviourUnchanged(t *testing.T) {
	done := domain.AcceptanceCriterion{ID: uuid.New(), Text: "answered", Completed: true}
	open := domain.AcceptanceCriterion{ID: uuid.New(), Text: "unanswered", Completed: false}
	r := NewRunner(RunnerDeps{})
	r.SetTaskUpdater(&criteriaUpdater{criteria: []domain.AcceptanceCriterion{done, open}})
	job := RunJob{Task: domain.BoardTask{
		ID: uuid.New(), Title: "t", Column: domain.TaskColumnCodeReview, TaskType: "analiz",
	}, RepositoryID: uuid.New()}

	require.False(t, listsEveryCriterion(analizWF, job.Task),
		"code_review is listed, so only the analiz guard can hold this false")

	got := r.criteriaForRun(context.Background(), job, analizWF)
	require.Len(t, got, 1)
	require.Equal(t, open.ID, got[0].ID)

	msg := buildTriggerMessage(job, analizWF, got, nil)
	require.Contains(t, msg, "Open acceptance criteria", "an analiz run keeps the implementer's open-only checklist")
	require.NotContains(t, msg, done.ID.String())
	require.NotContains(t, msg, "Acceptance criteria the diff must satisfy")
}

type changedFilesSinceGit struct {
	port.GitClient
	bySHA map[string][]string
	calls []string
}

func (g *changedFilesSinceGit) ChangedFilesSince(_ context.Context, _ string, sha string) ([]string, error) {
	g.calls = append(g.calls, sha)
	return g.bySHA[sha], nil
}

func TestChangedSinceByVerifiedSHAReportsWhatMovedSinceEachVerdict(t *testing.T) {
	sha := "1111111111112222222222223333333333334444"
	criteria := []domain.AcceptanceCriterion{
		{
			ID: uuid.New(), Text: "a",
			Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true, VerifiedSHA: sha}},
		},
		{
			ID: uuid.New(), Text: "b",
			Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true, VerifiedSHA: sha}},
		},
	}
	g := &changedFilesSinceGit{bySHA: map[string][]string{sha: {"server/foo.go"}}}
	r := &Runner{git: g}

	got := r.changedSinceByVerifiedSHA(context.Background(), "/workspace", criteria)

	require.Equal(t, []string{"server/foo.go"}, got[sha])
	require.Len(t, g.calls, 1, "one review round shares one SHA, so this must be one git call, not one per criterion")
}

func TestCriterionStateLineNotesFilesChangedSinceAnApprovedVerdict(t *testing.T) {
	sha := "1111111111112222222222223333333333334444"
	touched := domain.AcceptanceCriterion{
		ID: uuid.New(), Text: "touched by the fix", Completed: true,
		Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true, VerifiedSHA: sha}},
	}
	untouched := domain.AcceptanceCriterion{
		ID: uuid.New(), Text: "untouched by the fix", Completed: true,
		Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true, VerifiedSHA: sha}},
	}
	noHistory := domain.AcceptanceCriterion{
		ID: uuid.New(), Text: "never reviewed against a commit", Completed: true,
		Checks: []domain.CriterionCheck{{Role: domain.CriterionReviewRoleQA, Approved: true}},
	}
	changedSince := map[string][]string{sha: {"server/foo.go"}}

	touchedLine := criterionStateLine(touched, changedSince)
	require.Contains(t, touchedLine, "approved (as of "+domain.ShortSHA(sha)+", changed since: server/foo.go)")

	unrelatedSince := map[string][]string{sha: nil}
	untouchedLine := criterionStateLine(untouched, unrelatedSince)
	require.Contains(t, untouchedLine, "approved (as of "+domain.ShortSHA(sha)+", nothing changed since)")

	plainLine := criterionStateLine(noHistory, changedSince)
	require.Contains(t, plainLine, "qa: approved")
	require.NotContains(t, plainLine, "as of", "a verdict with no recorded SHA gets no changed-since note")
}

func TestTriggerMessageSnapshotCarriesTheRepositoryID(t *testing.T) {
	repoID := uuid.New()
	msg := buildTriggerMessage(RunJob{
		Task:         domain.BoardTask{Title: "t", Column: domain.TaskColumnInProgress},
		RepositoryID: repoID,
	}, taskWF, nil, nil)

	if !strings.Contains(msg, `"repository_id":"`+repoID.String()+`"`) {
		t.Fatalf("task snapshot does not carry the repository id:\n%s", msg)
	}
	if !strings.Contains(msg, "want the repository_id UUID from the snapshot below — never the repository name") {
		t.Fatalf("trigger message does not tell the run which repository identifier to pass:\n%s", msg)
	}
}
