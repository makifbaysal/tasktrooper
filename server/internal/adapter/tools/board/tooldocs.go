package board

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// Every board tool's description now lives in catalog/system/tools/<name>.md
// (application/registry.Register fills it in — see tooldocs.go there) rather
// than in this package's Definition() literals. prompt.Define is what lets
// internal/platform/runtime's completeness test catch a tools/ file with no
// Go-side counterpart, or a Go tool whose catalog doc went missing — one
// entry per tool, all in this file so a new tool cannot be added without
// this list (and therefore that test) noticing it.
func init() {
	for _, name := range []string{
		listBoardTasksToolName,
		listReadyTasksToolName,
		createBoardTaskToolName,
		moveBoardTaskToolName,
		updateBoardTaskToolName,
		deleteBoardTaskToolName,
		claimBoardTaskToolName,
		addTaskCommentToolName,
		listTaskCommentsToolName,
		addTaskDocumentToolName,
		updateTaskDocumentToolName,
		listTaskDocumentsToolName,
		listDocumentAnnotationsToolName,
		resolveDocumentAnnotationsToolName,
		recordOpenQuestionsToolName,
		listOpenQuestionsToolName,
		listCriteriaToolName,
		setCriterionToolName,
		cancelCriterionToolName,
		reviewCriterionToolName,
		listTestCasesToolName,
		recordTestCasesToolName,
		setTestCaseResultToolName,
		getBoardSummaryToolName,
		getPipelineStatusToolName,
		getTaskPreviewToolName,
		startTaskPreviewToolName,
		declareEnvVarsToolName,
		listProjectsToolName,
		listRepositoriesToolName,
		createProjectToolName,
		updateProjectToolName,
		setRepositoryProjectsToolName,
		listTeamToolName,
		attachTaskFileToolName,
		getTaskPullRequestToolName,
		commitTaskChangesToolName,
		commentOnPullRequestToolName,
		mergeTaskPullRequestToolName,
		deployLogsToolName,
		getReleaseToolName,
		deployReleaseToolName,
		watchReleaseToolName,
		runSmokeChecksToolName,
		finishReleaseToolName,
		rollbackReleaseToolName,
	} {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}
