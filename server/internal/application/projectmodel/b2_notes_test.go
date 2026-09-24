package projectmodel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestB2SaveUserNoteValidatesTopicComponentAndBody(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	comp := b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	otherRepo := b2SeedRepo(t, repos, "other")
	otherComp := b2SeedComponent(t, store, domain.Component{RepositoryID: otherRepo.ID, Path: ".", Status: domain.ComponentStatusActive})

	_, err := svc.SaveUserNote(ctx, repo.ID, domain.SaveNoteRequest{Topic: "not-a-topic", BodyMD: "hello"})
	assert.ErrorIs(t, err, ErrInvalidInput, "an invalid topic must be rejected")

	_, err = svc.SaveUserNote(ctx, repo.ID, domain.SaveNoteRequest{Topic: domain.NotePurpose, BodyMD: "   "})
	assert.ErrorIs(t, err, ErrInvalidInput, "an empty body must be rejected")

	_, err = svc.SaveUserNote(ctx, repo.ID, domain.SaveNoteRequest{ComponentID: &otherComp.ID, Topic: domain.NotePurpose, BodyMD: "hello"})
	assert.ErrorIs(t, err, ErrInvalidInput, "a component from another repository must be rejected")

	note, err := svc.SaveUserNote(ctx, repo.ID, domain.SaveNoteRequest{ComponentID: &comp.ID, Topic: domain.NotePurpose, BodyMD: "What this does.", Locked: true})
	require.NoError(t, err)
	assert.Equal(t, domain.NoteAuthorUser, note.Author)
	assert.True(t, note.Locked)
	assert.False(t, note.Stale)
}

func TestB2UpdateAndDeleteNote(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo := b2SeedRepo(t, repos, "demo")
	note, err := store.SaveNote(ctx, domain.ProjectNote{RepositoryID: repo.ID, Topic: domain.NoteGotchas, BodyMD: "old body", Author: domain.NoteAuthorUser})
	require.NoError(t, err)

	_, err = svc.UpdateNote(ctx, note.ID, domain.NotePatch{BodyMD: strPtr("   ")})
	assert.ErrorIs(t, err, ErrInvalidInput)

	locked := true
	got, err := svc.UpdateNote(ctx, note.ID, domain.NotePatch{BodyMD: strPtr("new body"), Locked: &locked})
	require.NoError(t, err)
	assert.Equal(t, "new body", got.BodyMD)
	assert.True(t, got.Locked)

	require.NoError(t, svc.DeleteNote(ctx, note.ID))
	_, err = store.GetNote(ctx, note.ID)
	assert.Error(t, err)
}

func strPtr(s string) *string { return &s }

func b2RepoWithFile(t *testing.T, repos *b2Repos, name, relPath string) (domain.Repository, string) {
	t.Helper()
	repo := b2SeedRepo(t, repos, name)
	full := filepath.Join(repo.RootPath, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte("package demo\n"), 0o600))
	return repo, full
}

func TestB2RecordAgentNotesAcceptsAWriteWithValidEvidence(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")
	b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	results, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
		{Topic: domain.NotePurpose, BodyMD: "Handles inventory sync.", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Accepted)
	assert.Empty(t, results[0].Reason)

	notes, err := store.ListNotes(ctx, repo.ID)
	require.NoError(t, err)
	require.Len(t, notes, 1)
	assert.Equal(t, domain.NoteAuthorAgent, notes[0].Author)
}

func TestB2RecordAgentNotesRejectsAWriteWithNoValidEvidence(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")
	b2SeedComponent(t, store, domain.Component{RepositoryID: repo.ID, Path: ".", Status: domain.ComponentStatusActive})

	results, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
		{Topic: domain.NoteGotchas, BodyMD: "Something surprising.", Evidence: []domain.SourceEvidence{{Path: "does/not/exist.go"}}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Accepted)
	assert.Contains(t, results[0].Reason, "does/not/exist.go")

	notes, err := store.ListNotes(ctx, repo.ID)
	require.NoError(t, err)
	assert.Empty(t, notes)
}

func TestB2RecordAgentNotesAcceptsWithAPartiallyInvalidEvidenceList(t *testing.T) {
	svc, _, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")

	results, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
		{Topic: domain.NotePurpose, BodyMD: "purpose", Evidence: []domain.SourceEvidence{
			{Path: "main.go"}, {Path: "missing.go"},
		}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Accepted)
	assert.Contains(t, results[0].Reason, "missing.go")
}

func TestB2RecordAgentNotesRejectsALockedNote(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")
	_, err := store.SaveNote(ctx, domain.ProjectNote{RepositoryID: repo.ID, Topic: domain.NotePurpose, BodyMD: "human wrote this", Author: domain.NoteAuthorAgent, Locked: true})
	require.NoError(t, err)

	results, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
		{Topic: domain.NotePurpose, BodyMD: "new purpose", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Accepted)
	assert.Contains(t, results[0].Reason, "locked")
}

func TestB2RecordAgentNotesRejectsAUserAuthoredNote(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")
	_, err := store.SaveNote(ctx, domain.ProjectNote{RepositoryID: repo.ID, Topic: domain.NotePurpose, BodyMD: "human wrote this", Author: domain.NoteAuthorUser})
	require.NoError(t, err)

	results, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
		{Topic: domain.NotePurpose, BodyMD: "new purpose", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Accepted)
	assert.Contains(t, results[0].Reason, "human")
}

func TestB2RecordAgentNotesRejectsAnUnknownComponentPath(t *testing.T) {
	svc, _, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")

	results, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
		{Topic: domain.NoteGotchas, BodyMD: "surprise", ComponentPath: "does-not-exist", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Accepted)
}

func TestB2RecordAgentNotesCountsAcceptedWritesPerRepository(t *testing.T) {
	svc, _, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")

	_, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
		{Topic: domain.NotePurpose, BodyMD: "purpose", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
		{Topic: domain.NoteGotchas, BodyMD: "gotcha", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
		{Topic: domain.NoteInvariants, BodyMD: "x", Evidence: []domain.SourceEvidence{{Path: "missing.go"}}},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, svc.notesWrittenCount(repo.ID))
}

func TestB2ValidNoteEvidenceStripsLineSuffixAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o600))

	valid, invalid := validNoteEvidence(root, []domain.SourceEvidence{
		{Path: "a.go:12"},
		{Path: "../outside.go"},
		{Path: "/etc/passwd"},
	})
	require.Len(t, valid, 1)
	assert.Equal(t, "a.go:12", valid[0].Path)
	assert.Len(t, invalid, 2)
}

func TestB2NotesPassNeeded(t *testing.T) {
	tests := []struct {
		name    string
		trigger domain.ScanTrigger
		notes   []domain.ProjectNote
		want    bool
	}{
		{"import always runs", domain.ScanTriggerImport, nil, true},
		{"manual always runs", domain.ScanTriggerManual, []domain.ProjectNote{{Topic: domain.NotePurpose, Author: domain.NoteAuthorAgent}}, true},
		{"migrate always runs", domain.ScanTriggerMigrate, []domain.ProjectNote{{Topic: domain.NotePurpose, Author: domain.NoteAuthorAgent}}, true},
		{"push with no purpose note runs", domain.ScanTriggerPush, nil, true},
		{"push with an agent purpose note skips", domain.ScanTriggerPush, []domain.ProjectNote{{Topic: domain.NotePurpose, Author: domain.NoteAuthorAgent}}, false},
		{"push with only a user purpose note still runs", domain.ScanTriggerPush, []domain.ProjectNote{{Topic: domain.NotePurpose, Author: domain.NoteAuthorUser}}, true},
		{"push with a stale note runs even with a purpose note", domain.ScanTriggerPush, []domain.ProjectNote{
			{Topic: domain.NotePurpose, Author: domain.NoteAuthorAgent},
			{Topic: domain.NoteGotchas, Author: domain.NoteAuthorAgent, Stale: true},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, notesPassNeeded(tt.trigger, tt.notes))
		})
	}
}

func TestB2NotesToolPolicyIncludesRecordNoteAndMemoryTools(t *testing.T) {
	policy := notesToolPolicy()
	assert.Contains(t, policy.AllowTools, RecordNoteToolName)
	assert.Contains(t, policy.AllowTools, "save_memory")
	assert.Contains(t, policy.AllowTools, "search_memory")
	for _, tool := range domain.CodeExplorationTools {
		assert.Contains(t, policy.AllowTools, tool)
	}
}

func TestB2MaybeRunNotesPassSkipsWhenNoAgentLoopIsWired(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")
	scan, err := store.CreateScan(ctx, domain.ProjectScan{ID: uuid.New(), RepositoryID: repo.ID, Trigger: domain.ScanTriggerImport, Status: domain.ScanSucceeded, StartedAt: time.Now()})
	require.NoError(t, err)

	svc.maybeRunNotesPass(ctx, repo, scan)

	got, err := store.GetScan(ctx, scan.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Events, "with no agent loop wired, the notes stage must never even start")
}

func TestB2MaybeRunNotesPassRetriesOnceThenRecordsTheOutcomeOnTheScan(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")
	scan, err := store.CreateScan(ctx, domain.ProjectScan{ID: uuid.New(), RepositoryID: repo.ID, Trigger: domain.ScanTriggerImport, Status: domain.ScanSucceeded, StartedAt: time.Now()})
	require.NoError(t, err)

	loop := &b2Loop{}
	loop.fn = func(ctx context.Context, _ []domain.Message) (domain.AgentResponse, error) {
		// b2Loop.Run records the call before invoking fn, so the first pass
		// through here sees callCount() == 1 and writes nothing; only the
		// reminder retry (callCount() == 2) records a note.
		if loop.callCount() == 2 {
			_, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
				{Topic: domain.NotePurpose, BodyMD: "purpose", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
			})
			require.NoError(t, err)
		}
		return domain.AgentResponse{}, nil
	}
	svc.SetAgentLoop(loop, &b2Agents{}, &b2Roles{})

	svc.maybeRunNotesPass(ctx, repo, scan)

	assert.Equal(t, 2, loop.callCount(), "nothing landed on the first pass, so a reminder retry must follow")
	retry := loop.calls[1]
	last := retry[len(retry)-1]
	assert.Equal(t, domain.RoleUser, last.Role)
	assert.Contains(t, last.Content, RecordNoteToolName)

	got, err := store.GetScan(ctx, scan.ID)
	require.NoError(t, err)
	require.Len(t, got.Events, 2)
	assert.Equal(t, domain.ScanStageNotes, got.Events[0].Stage)
	assert.False(t, got.Events[0].Done)
	assert.True(t, got.Events[1].Done)
	assert.Equal(t, "1 notes written", got.Events[1].Summary, "the note landed on the retry pass and must still be counted")
}

func TestB2MaybeRunNotesPassSucceedsOnTheFirstPassWithNoRetry(t *testing.T) {
	svc, store, repos, _, _, _ := newB2Service(t)
	ctx := context.Background()
	repo, _ := b2RepoWithFile(t, repos, "demo", "main.go")
	scan, err := store.CreateScan(ctx, domain.ProjectScan{ID: uuid.New(), RepositoryID: repo.ID, Trigger: domain.ScanTriggerImport, Status: domain.ScanSucceeded, StartedAt: time.Now()})
	require.NoError(t, err)

	loop := &b2Loop{}
	loop.fn = func(ctx context.Context, _ []domain.Message) (domain.AgentResponse, error) {
		_, err := svc.RecordAgentNotes(ctx, repo.ID, []NoteWrite{
			{Topic: domain.NotePurpose, BodyMD: "purpose", Evidence: []domain.SourceEvidence{{Path: "main.go"}}},
		})
		require.NoError(t, err)
		return domain.AgentResponse{}, nil
	}
	svc.SetAgentLoop(loop, &b2Agents{}, &b2Roles{})

	svc.maybeRunNotesPass(ctx, repo, scan)

	assert.Equal(t, 1, loop.callCount(), "a write on the first pass must not trigger the reminder retry")
	got, err := store.GetScan(ctx, scan.ID)
	require.NoError(t, err)
	require.Len(t, got.Events, 2)
	assert.Equal(t, "1 notes written", got.Events[1].Summary)
}
