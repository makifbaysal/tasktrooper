package release

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestProposeCutRecordsTheVersionAndShipsNothing(t *testing.T) {
	f := newCutFixture()
	draft := f.addDraft(batchComponent(domain.ExecutorGitHubActions), taskRef(domain.TaskTypeBug))
	f.git.tag = "v1.2.3"

	got, err := f.svc.ProposeCut(context.Background(), draft.ID, domain.ReleaseActorAgent, domain.ReleaseCutRequest{})
	require.NoError(t, err)
	require.NotNil(t, got.CutProposal)
	assert.Equal(t, "1.2.4", got.CutProposal.Version, "no version given takes the suggested one")
	assert.Contains(t, got.CutProposal.Notes, "### Fixes")
	assert.Equal(t, domain.ReleaseActorAgent, got.CutProposal.By)

	stored := f.store.releases[draft.ID]
	assert.Equal(t, domain.ReleaseDraft, stored.Status, "a proposal leaves the release a draft")
	assert.Empty(t, stored.Tag)
	assert.Nil(t, stored.CutAt)
	require.NotNil(t, stored.CutProposal)
}

func TestProposeCutKeepsTheAgentsOwnVersionAndNotes(t *testing.T) {
	f := newCutFixture()
	draft := f.addDraft(batchComponent(domain.ExecutorGitHubActions), taskRef(domain.TaskTypeTask))

	got, err := f.svc.ProposeCut(context.Background(), draft.ID, domain.ReleaseActorAgent,
		domain.ReleaseCutRequest{Version: " 2.0.0 ", Notes: "Big one"})
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", got.CutProposal.Version)
	assert.Equal(t, "Big one", got.CutProposal.Notes)
}

func TestProposeCutRefusesWhatAHumanCouldNotCut(t *testing.T) {
	f := newCutFixture()
	component := batchComponent(domain.ExecutorGitHubActions)

	empty := f.addDraft(component)
	_, err := f.svc.ProposeCut(context.Background(), empty.ID, domain.ReleaseActorAgent, domain.ReleaseCutRequest{Version: "1.0.0"})
	assert.ErrorIs(t, err, domain.ErrReleaseEmpty)

	cut := f.addDraft(component, taskRef(domain.TaskTypeTask))
	cut.Status = domain.ReleasePending
	f.store.releases[cut.ID] = cut
	_, err = f.svc.ProposeCut(context.Background(), cut.ID, domain.ReleaseActorAgent, domain.ReleaseCutRequest{Version: "1.0.0"})
	assert.ErrorIs(t, err, domain.ErrReleaseWrongStatus)

	draft := f.addDraft(component, taskRef(domain.TaskTypeTask))
	_, err = f.svc.ProposeCut(context.Background(), draft.ID, domain.ReleaseActorAgent, domain.ReleaseCutRequest{Version: "-bad"})
	assert.ErrorIs(t, err, domain.ErrInvalidVersion)
	assert.Nil(t, f.store.releases[draft.ID].CutProposal)
}

func TestCutPreviewOpensOnTheProposedVersionAndNotes(t *testing.T) {
	f := newCutFixture()
	draft := f.addDraft(batchComponent(domain.ExecutorGitHubActions), taskRef(domain.TaskTypeBug))
	f.git.head = "headsha0123456789012345678901234567890123"
	f.git.ancestors[mergeSHA] = true
	f.git.tag = "v1.2.3"
	_, err := f.svc.ProposeCut(context.Background(), draft.ID, domain.ReleaseActorAgent,
		domain.ReleaseCutRequest{Version: "1.5.0", Notes: "Proposed notes"})
	require.NoError(t, err)

	preview, err := f.svc.CutPreview(context.Background(), draft.ID)
	require.NoError(t, err)
	assert.Equal(t, "1.5.0", preview.SuggestedVersion)
	assert.Equal(t, "v1.5.0", preview.Tag)
	assert.Equal(t, "Proposed notes", preview.Notes)
}

func TestDismissCutProposalKeepsTheDraftOpen(t *testing.T) {
	f := newCutFixture()
	draft := f.addDraft(batchComponent(domain.ExecutorGitHubActions), taskRef(domain.TaskTypeTask))
	_, err := f.svc.ProposeCut(context.Background(), draft.ID, domain.ReleaseActorAgent, domain.ReleaseCutRequest{Version: "1.0.0"})
	require.NoError(t, err)

	got, err := f.svc.DismissCutProposal(context.Background(), draft.ID)
	require.NoError(t, err)
	assert.Nil(t, got.CutProposal)
	assert.Equal(t, domain.ReleaseDraft, f.store.releases[draft.ID].Status)

	_, err = f.svc.DismissCutProposal(context.Background(), draft.ID)
	require.NoError(t, err, "dismissing twice is answered as it is")
}
