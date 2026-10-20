package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type reviewUpdater struct {
	fakeTaskUpdater
	annotations []domain.TaskDocumentAnnotation
	docs        []domain.TaskDocument
	err         error
}

func (r *reviewUpdater) ListAnnotations(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) ([]domain.TaskDocumentAnnotation, error) {
	return r.annotations, r.err
}

func (r *reviewUpdater) ListDocuments(context.Context, uuid.UUID, uuid.UUID) ([]domain.TaskDocument, error) {
	return r.docs, nil
}

func analizRevisionJob() RunJob {
	job := runJobFor(domain.BoardTask{
		ID: uuid.New(), Key: "A-7", Title: "CSV export", TaskType: "analiz", Column: domain.TaskColumnInProgress,
	}, uuid.New())
	job.EnteredFrom = domain.TaskColumnNeedRevision
	return job
}

func TestReviewAnnotationsMessageListsEverySubmittedComment(t *testing.T) {
	docID := uuid.New()
	sent1 := domain.TaskDocumentAnnotation{ID: uuid.New(), DocumentID: docID, Quote: "Use a\n  queue.", Body: "Why not a cron job?", Status: domain.AnnotationStatusSubmitted}
	sent2 := domain.TaskDocumentAnnotation{ID: uuid.New(), DocumentID: docID, Quote: "Retry 3 times", Body: "Name the backoff.", Status: domain.AnnotationStatusSubmitted}
	draft := domain.TaskDocumentAnnotation{ID: uuid.New(), DocumentID: docID, Quote: "x", Body: "unsent draft", Status: domain.AnnotationStatusOpen}
	done := domain.TaskDocumentAnnotation{ID: uuid.New(), DocumentID: docID, Quote: "y", Body: "old round", Status: domain.AnnotationStatusResolved}
	r := &Runner{taskUpdater: &reviewUpdater{
		annotations: []domain.TaskDocumentAnnotation{sent1, draft, done, sent2},
		docs:        []domain.TaskDocument{{ID: docID, Title: "analiz: 2026-09-26 export"}},
	}}

	msg := r.reviewAnnotationsMessage(context.Background(), analizRevisionJob())

	require.NotEmpty(t, msg)
	assert.True(t, strings.HasPrefix(msg, "## Review comments on your analysis document"))
	assert.Contains(t, msg, "sent back 2 comment(s)")
	assert.Contains(t, msg, sent1.ID.String())
	assert.Contains(t, msg, sent2.ID.String())
	assert.Contains(t, msg, `Passage: "Use a queue."`, "the quote is on one line")
	assert.Contains(t, msg, "Comment: Why not a cron job?")
	assert.Contains(t, msg, "Comment: Name the backoff.")
	assert.Contains(t, msg, `"analiz: 2026-09-26 export"`)
	assert.Contains(t, msg, docID.String())
	assert.NotContains(t, msg, "unsent draft", "an open comment was never sent")
	assert.NotContains(t, msg, "old round", "a resolved comment is already answered")
	assert.Contains(t, msg, "raw: true")
	assert.Contains(t, msg, "update_task_document")
	assert.Contains(t, msg, "resolve_document_annotations")
	assert.Contains(t, msg, "do not move it yourself")
	assert.NotContains(t, msg, "more comment(s) are not shown")
}

func TestReviewAnnotationsMessageSaysHowManyItLeftOut(t *testing.T) {
	docID := uuid.New()
	var items []domain.TaskDocumentAnnotation
	for i := 0; i < 12; i++ {
		items = append(items, domain.TaskDocumentAnnotation{
			ID: uuid.New(), DocumentID: docID, Status: domain.AnnotationStatusSubmitted,
			Quote: fmt.Sprintf("passage %d ", i) + strings.Repeat("q", 2000),
			Body:  fmt.Sprintf("comment %d ", i) + strings.Repeat("b", domain.MaxAnnotationBodyChars-20),
		})
	}
	r := &Runner{taskUpdater: &reviewUpdater{annotations: items}}

	msg := r.reviewAnnotationsMessage(context.Background(), analizRevisionJob())

	assert.LessOrEqual(t, len(msg), reviewAnnotationsLimit+600, "the message stays near its cap")
	shown := strings.Count(msg, "### Comment ")
	require.Greater(t, shown, 0)
	require.Less(t, shown, len(items))
	assert.True(t, strings.Contains(msg, fmt.Sprintf("…%d more comment(s) are not shown", len(items)-shown)))
	assert.True(t, strings.Contains(msg, "list_document_annotations with task_id A-7 and status \"submitted\""))
	assert.True(t, strings.Contains(msg, "comment 0 "), "the list is cut from the end, not the start")
	assert.True(t, strings.Contains(msg, "…\"\nComment:"), "a long quote is visibly shortened")
}

func TestReviewAnnotationsMessageIsEmptyWithoutSubmittedComments(t *testing.T) {
	r := &Runner{taskUpdater: &reviewUpdater{annotations: []domain.TaskDocumentAnnotation{
		{ID: uuid.New(), Status: domain.AnnotationStatusResolved, Quote: "q", Body: "b"},
	}}}
	assert.Empty(t, r.reviewAnnotationsMessage(context.Background(), analizRevisionJob()))

	assert.Empty(t, (&Runner{taskUpdater: &fakeTaskUpdater{}}).reviewAnnotationsMessage(context.Background(), analizRevisionJob()),
		"a build whose service cannot list annotations injects nothing")
	assert.Empty(t, (&Runner{taskUpdater: &reviewUpdater{err: errors.New("db down")}}).reviewAnnotationsMessage(context.Background(), analizRevisionJob()))
}

func TestAnalysisContextRendersAnHTMLReportAsText(t *testing.T) {
	report := "<!DOCTYPE html><html><head><style>.callout{border:1px solid}</style></head><body>" +
		"<h1 id=\"summary\">Summary</h1><p>Export tasks as CSV.</p>" +
		"<h2 id=\"plan\">Implementation plan</h2><ol><li>Add <code>TaskExporter</code></li></ol></body></html>"
	updater := &analysisUpdater{refs: []domain.AnalysisReference{{
		TaskID: uuid.New(), Key: "A-12", Title: "CSV export",
		Documents: []domain.TaskDocument{{Title: "analiz: 2026-09-26 export", Content: report, Format: domain.DocumentFormatHTML}},
	}}}
	r := &Runner{taskUpdater: updater}

	msg := r.analysisContext(context.Background(), implementationJob())

	assert.Contains(t, msg, "#### analiz: 2026-09-26 export")
	assert.Contains(t, msg, "# Summary")
	assert.Contains(t, msg, "1. Add `TaskExporter`")
	assert.NotContains(t, msg, "<h1")
	assert.NotContains(t, msg, "border:1px")
}

func TestAnalysisContextGivesHTMLReportsTheLargerBudget(t *testing.T) {
	paragraph := "<p>" + strings.Repeat("word ", 200) + "</p>"
	report := "<html><body>" + strings.Repeat(paragraph, htmlAnalysisContextLimit/len(paragraph)+40) + "</body></html>"
	updater := &analysisUpdater{refs: []domain.AnalysisReference{{
		TaskID: uuid.New(), Key: "A-12",
		Documents: []domain.TaskDocument{{Title: "analiz: big", Content: report, Format: domain.DocumentFormatHTML}},
	}}}
	r := &Runner{taskUpdater: updater}

	msg := r.analysisContext(context.Background(), implementationJob())

	assert.Greater(t, len(msg), analysisContextLimit, "an html report is not held to the markdown budget")
	assert.Less(t, len(msg), htmlAnalysisContextLimit+2000)
	assert.Contains(t, msg, "list_task_documents with task_id A-12")
}
