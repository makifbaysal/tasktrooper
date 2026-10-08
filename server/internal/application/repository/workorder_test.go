package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type graphRelationStore struct {
	edges []domain.TaskRelation
	keys  map[uuid.UUID]string
	title map[uuid.UUID]string
}

func newGraphRelations() *graphRelationStore {
	return &graphRelationStore{keys: map[uuid.UUID]string{}, title: map[uuid.UUID]string{}}
}

func (g *graphRelationStore) add(source, target uuid.UUID, relType domain.TaskRelationType) {
	g.edges = append(g.edges, domain.TaskRelation{
		ID: uuid.New(), SourceTaskID: source, TargetTaskID: target, RelationType: relType,
	})
}

func (g *graphRelationStore) ReplaceForTask(_ context.Context, sourceTaskID uuid.UUID, rels []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	kept := g.edges[:0:0]
	for _, e := range g.edges {
		if e.SourceTaskID != sourceTaskID {
			kept = append(kept, e)
		}
	}
	g.edges = kept
	var out []domain.TaskRelation
	for _, rel := range rels {
		g.add(sourceTaskID, rel.TargetTaskID, rel.RelationType)
		out = append(out, g.edges[len(g.edges)-1])
	}
	return out, nil
}

func (g *graphRelationStore) ReplaceForTaskOfType(_ context.Context, sourceTaskID uuid.UUID, relType domain.TaskRelationType, rels []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	kept := g.edges[:0:0]
	for _, e := range g.edges {
		if e.SourceTaskID != sourceTaskID || e.RelationType != relType {
			kept = append(kept, e)
		}
	}
	g.edges = kept
	var out []domain.TaskRelation
	for _, rel := range rels {
		g.add(sourceTaskID, rel.TargetTaskID, relType)
		out = append(out, g.edges[len(g.edges)-1])
	}
	return out, nil
}

func (g *graphRelationStore) ListBySource(_ context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	var out []domain.TaskRelation
	for _, e := range g.edges {
		if e.SourceTaskID != sourceTaskID {
			continue
		}
		e.TargetKey = g.keys[e.TargetTaskID]
		e.TargetTitle = g.title[e.TargetTaskID]
		out = append(out, e)
	}
	return out, nil
}

func (g *graphRelationStore) ListBlockedBy(_ context.Context, targetTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	var out []domain.TaskRelation
	for _, e := range g.edges {
		if e.TargetTaskID != targetTaskID || e.RelationType != domain.TaskRelationBlocks {
			continue
		}
		e.SourceKey = g.keys[e.SourceTaskID]
		e.SourceTitle = g.title[e.SourceTaskID]
		out = append(out, e)
	}
	return out, nil
}

func (g *graphRelationStore) ListBlockingSources(context.Context, uuid.UUID) ([]domain.BoardTask, error) {
	return nil, nil
}

func (g *graphRelationStore) ListUnfinishedBlockers(context.Context) ([]domain.TaskRelation, error) {
	return nil, nil
}

func (g *graphRelationStore) AddBlockers(_ context.Context, targetTaskID uuid.UUID, sourceTaskIDs []uuid.UUID) ([]domain.TaskRelation, error) {
	var out []domain.TaskRelation
	for _, sourceID := range sourceTaskIDs {
		g.add(sourceID, targetTaskID, domain.TaskRelationBlocks)
		out = append(out, g.edges[len(g.edges)-1])
	}
	return out, nil
}

type memoryDocumentStore struct {
	byTask map[uuid.UUID][]domain.TaskDocument
}

func (m *memoryDocumentStore) Create(_ context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	doc.ID = uuid.New()
	if m.byTask == nil {
		m.byTask = map[uuid.UUID][]domain.TaskDocument{}
	}
	m.byTask[doc.TaskID] = append(m.byTask[doc.TaskID], doc)
	return doc, nil
}
func (m *memoryDocumentStore) Get(context.Context, uuid.UUID, uuid.UUID) (domain.TaskDocument, error) {
	return domain.TaskDocument{}, nil
}
func (m *memoryDocumentStore) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskDocument, error) {
	return m.byTask[taskID], nil
}
func (m *memoryDocumentStore) Update(_ context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	return doc, nil
}
func (m *memoryDocumentStore) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type orderFixture struct {
	svc       *Service
	repoID    uuid.UUID
	tasks     *fakePackageTaskStore
	relations *graphRelationStore
	documents *memoryDocumentStore
}

func newOrderFixture(t *testing.T, keys ...string) *orderFixture {
	t.Helper()
	repoID := uuid.New()
	tasks := &fakePackageTaskStore{tasks: map[uuid.UUID]domain.BoardTask{}}
	relations := newGraphRelations()
	documents := &memoryDocumentStore{byTask: map[uuid.UUID][]domain.TaskDocument{}}
	for _, key := range keys {
		id := uuid.New()
		taskType := domain.TaskType("task")
		if strings.HasPrefix(key, "A-") {
			taskType = "analiz"
		}
		if strings.HasPrefix(key, "D-") {
			taskType = domain.TaskTypeDesign
		}
		tasks.tasks[id] = domain.BoardTask{
			ID: id, RepositoryID: repoID, Key: key, Title: "work for " + key,
			Column: domain.TaskColumnTodo, TaskType: taskType,
		}
		relations.keys[id] = key
		relations.title[id] = "work for " + key
	}
	return &orderFixture{
		svc: &Service{
			repos:     &fakeReleaseRepoStore{repo: domain.Repository{ID: repoID}},
			tasks:     tasks,
			relations: relations,
			documents: documents,
		},
		repoID: repoID, tasks: tasks, relations: relations, documents: documents,
	}
}

func (f *orderFixture) id(t *testing.T, key string) uuid.UUID {
	t.Helper()
	for id, task := range f.tasks.tasks {
		if task.Key == key {
			return id
		}
	}
	t.Fatalf("no task with key %s", key)
	return uuid.Nil
}

func TestSetBlockersWritesTheEdgeWithTheBlockerAsSource(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")

	written, err := f.svc.SetBlockers(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})

	require.NoError(t, err)
	require.Len(t, written, 1)
	assert.Equal(t, api, written[0].SourceTaskID, "the blocker is the source")
	assert.Equal(t, web, written[0].TargetTaskID, "the blocked task is the target")
	assert.Equal(t, domain.TaskRelationBlocks, written[0].RelationType)
}

func TestSetBlockersDeduplicatesWithinOneCall(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")

	written, err := f.svc.SetBlockers(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}, {TargetTaskID: api}})

	require.NoError(t, err)
	assert.Len(t, written, 1)
}

func TestSetBlockersRefusesSelfBlocking(t *testing.T) {
	f := newOrderFixture(t, "T-1")
	api := f.id(t, "T-1")

	_, err := f.svc.SetBlockers(context.Background(), api, []domain.TaskRelationInput{{TargetTaskID: api}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by itself")
}

func TestWorkOrderCycleIsRefusedWithThePathThatCausesIt(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2", "T-3")
	one, two, three := f.id(t, "T-1"), f.id(t, "T-2"), f.id(t, "T-3")

	require.NoError(t, mustBlock(f, two, one))
	require.NoError(t, mustBlock(f, three, two))

	_, err := f.svc.SetBlockers(context.Background(), one, []domain.TaskRelationInput{{TargetTaskID: three}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "work-order cycle refused")
	assert.Contains(t, err.Error(), "T-1", "the error names the task being blocked")
	assert.Contains(t, err.Error(), "T-3", "and the blocker it cannot wait for")
	assert.Contains(t, err.Error(), "→", "and the chain that closes the loop")
}

func TestWorkOrderCycleRefusalIsDirect(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	one, two := f.id(t, "T-1"), f.id(t, "T-2")
	require.NoError(t, mustBlock(f, two, one))

	_, err := f.svc.SetBlockers(context.Background(), one, []domain.TaskRelationInput{{TargetTaskID: two}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "work-order cycle refused")
}

func TestWorkOrderDiamondIsAllowed(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2", "T-3", "T-4")
	one, two, three, four := f.id(t, "T-1"), f.id(t, "T-2"), f.id(t, "T-3"), f.id(t, "T-4")
	require.NoError(t, mustBlock(f, two, one))
	require.NoError(t, mustBlock(f, three, one))

	_, err := f.svc.SetBlockers(context.Background(), four,
		[]domain.TaskRelationInput{{TargetTaskID: two}, {TargetTaskID: three}})
	require.NoError(t, err)
}

func TestDeployOrderCycleIsRefused(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")

	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	_, err = f.svc.ReplaceDeployDependencies(context.Background(), api,
		[]domain.TaskRelationInput{{TargetTaskID: web}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deploy-order cycle refused")
	assert.Contains(t, err.Error(), "T-1")
	assert.Contains(t, err.Error(), "T-2")
}

func mustBlock(f *orderFixture, blocked, blocker uuid.UUID) error {
	_, err := f.svc.SetBlockers(context.Background(), blocked,
		[]domain.TaskRelationInput{{TargetTaskID: blocker}})
	return err
}

func TestOrderNoteIsGeneratedAndKeepsTheAgentsOwnRunbook(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	agentText := "Warm the CDN cache for /export before flipping the flag."
	task := f.tasks.tasks[web]
	task.BeforeDeploy = &agentText
	f.tasks.tasks[web] = task

	updated := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])

	require.NotNil(t, updated.BeforeDeploy)
	got := *updated.BeforeDeploy
	assert.Contains(t, got, "Ships after: T-1 (work for T-1)")
	assert.Contains(t, got, agentText, "the agent's own runbook survives verbatim")
	assert.Contains(t, got, domain.OrderNoteOpen)
	assert.Contains(t, got, domain.OrderNoteClose)
	assert.True(t, strings.Index(got, domain.OrderNoteOpen) < strings.Index(got, agentText),
		"the precondition goes first, ahead of the checklist it applies to")
}

func TestOrderNoteIsReplacedNotAppendedWhenTheOrderChanges(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2", "T-3")
	api, web, other := f.id(t, "T-1"), f.id(t, "T-2"), f.id(t, "T-3")

	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)
	first := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])
	f.tasks.tasks[web] = first

	_, err = f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: other}})
	require.NoError(t, err)
	second := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])

	require.NotNil(t, second.BeforeDeploy)
	got := *second.BeforeDeploy
	assert.Contains(t, got, "T-3")
	assert.NotContains(t, got, "T-1", "the superseded ordering must not survive as a second block")
	assert.Equal(t, 1, strings.Count(got, domain.OrderNoteOpen))
}

func TestOrderNoteStatesTheWorkOrderAsWellAsTheDeployOrder(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	require.NoError(t, mustBlock(f, web, api))

	updated := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web])

	require.NotNil(t, updated.BeforeDeploy)
	assert.Contains(t, *updated.BeforeDeploy, "Built after: T-1 (work for T-1)")
}

func TestOrderNoteIsSilentWhenThereIsNoOrder(t *testing.T) {
	f := newOrderFixture(t, "T-1")
	updated := f.svc.syncOrderNote(context.Background(), f.tasks.tasks[f.id(t, "T-1")])
	assert.Nil(t, updated.BeforeDeploy)
}

func TestReleaseChecklistCarriesTheGeneratedOrderingAndTheAgentsText(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	comments := &fakeReleaseComments{}
	f.svc.comments = comments
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	agentText := "Confirm the export feature flag is off in prod."
	rollback := "Revert the deploy and re-run the previous image."
	task := f.tasks.tasks[web]
	task.BeforeDeploy = &agentText
	task.RollbackPlan = &rollback
	f.tasks.tasks[web] = task

	f.svc.postPreDeployChecklist(context.Background(), f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web]))

	require.Len(t, comments.comments, 1)
	body := comments.comments[0].Content
	assert.Contains(t, body, "Ships after: T-1 (work for T-1)")
	assert.Contains(t, body, agentText)
	assert.Contains(t, body, rollback)
	assert.NotContains(t, body, domain.OrderNoteOpen)
	assert.NotContains(t, body, domain.OrderNoteClose)
}

func TestReleaseChecklistIsPostedForOrderingAlone(t *testing.T) {
	f := newOrderFixture(t, "T-1", "T-2")
	comments := &fakeReleaseComments{}
	f.svc.comments = comments
	api, web := f.id(t, "T-1"), f.id(t, "T-2")
	_, err := f.svc.ReplaceDeployDependencies(context.Background(), web,
		[]domain.TaskRelationInput{{TargetTaskID: api}})
	require.NoError(t, err)

	f.svc.postPreDeployChecklist(context.Background(), f.svc.syncOrderNote(context.Background(), f.tasks.tasks[web]))

	require.Len(t, comments.comments, 1)
	assert.Contains(t, comments.comments[0].Content, "Ships after: T-1")
}

func TestAnalysisReferencesReturnsTheAnalizTasksDocuments(t *testing.T) {
	f := newOrderFixture(t, "A-12", "T-2")
	analysis, impl := f.id(t, "A-12"), f.id(t, "T-2")
	_, err := f.documents.Create(context.Background(), domain.TaskDocument{
		TaskID: analysis, Title: "spec: 2026-08-17 export", Content: "## Context\nCSV export.",
	})
	require.NoError(t, err)
	_, err = f.documents.Create(context.Background(), domain.TaskDocument{
		TaskID: analysis, Title: "plan: 2026-08-17 export", Content: "### Task 1\nTaskExporter.",
	})
	require.NoError(t, err)
	f.relations.add(impl, analysis, domain.TaskRelationDerivedFrom)

	refs, err := f.svc.AnalysisReferences(context.Background(), impl)

	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "A-12", refs[0].Key)
	require.Len(t, refs[0].Documents, 2)
	assert.Equal(t, "spec: 2026-08-17 export", refs[0].Documents[0].Title)
	assert.Contains(t, refs[0].Documents[1].Content, "TaskExporter")
}

func TestAnalysisReferencesHandsAnApprovedDesignToTheTasksItBlocks(t *testing.T) {
	f := newOrderFixture(t, "D-3", "D-4", "T-1", "T-2")
	approved, pending, blocker, impl := f.id(t, "D-3"), f.id(t, "D-4"), f.id(t, "T-1"), f.id(t, "T-2")
	design := f.tasks.tasks[approved]
	design.Column = domain.TaskColumnReleased
	f.tasks.tasks[approved] = design
	_, err := f.documents.Create(context.Background(), domain.TaskDocument{
		TaskID: approved, Title: "handoff: export dialog", Content: "Primary action: Export CSV",
	})
	require.NoError(t, err)
	f.relations.add(approved, impl, domain.TaskRelationBlocks)
	f.relations.add(pending, impl, domain.TaskRelationBlocks)
	f.relations.add(blocker, impl, domain.TaskRelationBlocks)

	refs, err := f.svc.AnalysisReferences(context.Background(), impl)

	require.NoError(t, err)
	require.Len(t, refs, 1, "only the approved design: an unapproved one and an ordinary blocker carry no contract")
	assert.Equal(t, "D-3", refs[0].Key)
	assert.True(t, refs[0].IsDesign())
	assert.Equal(t, f.repoID, refs[0].RepositoryID)
	require.Len(t, refs[0].Documents, 1)
	assert.Equal(t, "handoff: export dialog", refs[0].Documents[0].Title)
}

func TestAnalysisReferencesDoesNotRepeatADesignNamedTwice(t *testing.T) {
	f := newOrderFixture(t, "D-3", "T-2")
	design, impl := f.id(t, "D-3"), f.id(t, "T-2")
	task := f.tasks.tasks[design]
	task.Column = domain.TaskColumnDone
	f.tasks.tasks[design] = task
	f.relations.add(impl, design, domain.TaskRelationDerivedFrom)
	f.relations.add(design, impl, domain.TaskRelationBlocks)

	refs, err := f.svc.AnalysisReferences(context.Background(), impl)

	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, domain.TaskTypeDesign, refs[0].TaskType)
}

type designChoiceComments struct{ byTask map[uuid.UUID][]domain.TaskComment }

func (d designChoiceComments) Create(_ context.Context, c domain.TaskComment) (domain.TaskComment, error) {
	return c, nil
}

func (d designChoiceComments) ListByTask(_ context.Context, taskID uuid.UUID) ([]domain.TaskComment, error) {
	return d.byTask[taskID], nil
}

func TestAnalysisReferencesDropTheVariantsTheHumanDidNotChoose(t *testing.T) {
	f := newOrderFixture(t, "D-3", "T-2")
	design, impl := f.id(t, "D-3"), f.id(t, "T-2")
	task := f.tasks.tasks[design]
	task.Column = domain.TaskColumnReleased
	f.tasks.tasks[design] = task
	for _, title := range []string{"design: export · A", "design: export · B", "handoff: export"} {
		_, err := f.documents.Create(context.Background(), domain.TaskDocument{TaskID: design, Title: title, Content: title})
		require.NoError(t, err)
	}
	f.svc.comments = designChoiceComments{byTask: map[uuid.UUID][]domain.TaskComment{
		design: {{AuthorType: "user", Content: "Chosen variant: design: export · B"}},
	}}
	f.relations.add(design, impl, domain.TaskRelationBlocks)

	refs, err := f.svc.AnalysisReferences(context.Background(), impl)

	require.NoError(t, err)
	require.Len(t, refs, 1)
	titles := []string{}
	for _, d := range refs[0].Documents {
		titles = append(titles, d.Title)
	}
	assert.ElementsMatch(t, []string{"design: export · B", "handoff: export"}, titles)
}

func TestAnalysisReferencesIgnoresOrderingRelations(t *testing.T) {
	f := newOrderFixture(t, "A-12", "T-1", "T-2")
	impl := f.id(t, "T-2")
	f.relations.add(impl, f.id(t, "T-1"), domain.TaskRelationDeployDependsOn)

	refs, err := f.svc.AnalysisReferences(context.Background(), impl)

	require.NoError(t, err)
	assert.Empty(t, refs)
}

func TestOrderNoteFenceRoundTrips(t *testing.T) {
	note := renderOrderNote([]string{"T-1 (API)"}, nil)
	require.NotEmpty(t, note)

	body := "Flip the feature flag.\nWarm the cache."
	combined := domain.ApplyOrderNote(body, note)
	assert.Equal(t, body, domain.StripOrderNote(combined), "stripping the block gives the agent's text back exactly")

	again := domain.ApplyOrderNote(combined, renderOrderNote([]string{"T-9 (other)"}, nil))
	assert.Equal(t, 1, strings.Count(again, domain.OrderNoteOpen))
	assert.Contains(t, again, "T-9")
	assert.NotContains(t, again, "T-1 (API)")
	assert.Contains(t, again, body)
}

func TestOrderNoteFenceSurvivesATruncatedBlock(t *testing.T) {
	broken := "Flip the flag.\n" + domain.OrderNoteOpen + "\n- Ships after: T-1"
	out := domain.ApplyOrderNote(broken, renderOrderNote([]string{"T-9 (other)"}, nil))
	assert.Equal(t, 1, strings.Count(out, domain.OrderNoteOpen))
	assert.Contains(t, out, "Flip the flag.")
}
