package projectmodel

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const notesReminderMessage = "You have not called record_project_note yet. The pass only counts once at least one note is accepted — " +
	"write the notes you can back with real evidence paths now. If a note was rejected, fix what it complained about and resend it."

const notesSystemPromptTemplate = `You are documenting repository %s so every other agent that later works on it inherits your judgment instead of rediscovering it.

The platform already knows the facts below — they came from a scan of the tree, not from you. Do not restate the stack, the commands, the CI checks or what talks to what; a note that only repeats the brief wastes the reader's time and will not be accepted as an improvement over having no note at all.

Your job is the part a scan cannot produce: judgment. Explore the code with the read-only tools, then call %s for what you find, one call per note:

- purpose: what this repository is for and who consumes it. Repository-level only (no component_path), one paragraph.
- entrypoints: where execution starts — main functions, route tables, job registrations, screen roots.
- conventions: the rules this codebase follows that a newcomer would otherwise break.
- invariants: things that break if violated — an ordering, a required field, a check every handler must pass through.
- danger_zones: the code that is expensive to get wrong.
- change_recipes: for the 2-4 changes this repo actually receives, the files to touch and in what order.
- gotchas: surprises that cost someone an hour.

In a monorepo, write component-scoped notes by passing component_path (the component's own path, "." for the root component); a fact true of only one component belongs on that component, not on the repository.

Every note needs at least one evidence path that exists in the working copy — a claim nobody can check is worse than no note. Update any note listed below as stale; leave the rest alone unless you have something to correct.`

// maybeRunNotesPass runs the agent that writes judgment notes, after a
// successful scan, when notes are missing or stale.
func (s *Service) maybeRunNotesPass(ctx context.Context, repo domain.Repository, scan domain.ProjectScan) {
	if s.loop == nil {
		return
	}
	existing, err := s.store.ListNotes(ctx, repo.ID)
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("notes pass: list notes failed")
		return
	}
	if !notesPassNeeded(scan.Trigger, existing) {
		return
	}

	s.mu.Lock()
	s.notesWritten[repo.ID] = 0
	s.mu.Unlock()

	s.appendScanStage(ctx, scan.ID, domain.ScanEvent{Stage: domain.ScanStageNotes, Done: false, At: s.now()})

	components, err := s.store.ListComponents(ctx, repo.ID)
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("notes pass: list components failed")
	}

	brief, err := s.Brief(ctx, repo.ID, BriefScope{})
	if err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("notes pass: brief generation failed")
	}

	architect := s.notesAgent(ctx)
	runCtx := registry.ContextWithWorkspaceDir(ctx, repo.RootPath)
	runCtx = registry.ContextWithRepositoryID(runCtx, repo.ID)
	if architect.ID != uuid.Nil {
		runCtx = registry.ContextWithAgentID(runCtx, architect.ID)
	}
	policy := notesToolPolicy()
	label := agent.WithCLILabel("notes:"+repo.Name, "repository notes")

	messages := buildNotesMessages(repo, brief, existing, components)
	if _, err := s.loop.Run(runCtx, messages, architect.Model, architect.ProviderType, policy, label); err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("notes pass: first pass errored")
	}

	if s.notesWrittenCount(repo.ID) == 0 {
		messages = append(messages, domain.Message{Role: domain.RoleUser, Content: notesReminderMessage})
		if _, err := s.loop.Run(runCtx, messages, architect.Model, architect.ProviderType, policy, label); err != nil {
			log.Warn().Err(err).Str("repository", repo.Name).Msg("notes pass: retry errored")
		}
	}

	written := s.notesWrittenCount(repo.ID)
	summary := fmt.Sprintf("%d notes written", written)
	if written == 0 {
		summary = "no notes written"
	}
	s.appendScanStage(ctx, scan.ID, domain.ScanEvent{Stage: domain.ScanStageNotes, Done: true, Summary: summary, At: s.now()})
}

// notesPassNeeded is true on every import/manual/migrate scan (the human or a
// fresh clone asked for a rewrite), and otherwise only when nothing has
// documented the repository's purpose yet or something on disk moved out from
// under an existing note.
func notesPassNeeded(trigger domain.ScanTrigger, notes []domain.ProjectNote) bool {
	switch trigger {
	case domain.ScanTriggerImport, domain.ScanTriggerManual, domain.ScanTriggerMigrate:
		return true
	}
	hasPurpose := false
	for _, n := range notes {
		if n.Stale {
			return true
		}
		if n.ComponentID == nil && n.Topic == domain.NotePurpose && n.Author == domain.NoteAuthorAgent {
			hasPurpose = true
		}
	}
	return !hasPurpose
}

func (s *Service) notesAgent(ctx context.Context) domain.Agent {
	if s.agents == nil || s.roles == nil {
		return domain.Agent{}
	}
	id, err := s.roles.AgentForPurpose(ctx, domain.PurposeRepoProfiler, "")
	if err != nil || id == nil {
		return domain.Agent{}
	}
	a, err := s.agents.GetAgent(ctx, *id)
	if err != nil {
		return domain.Agent{}
	}
	return a
}

func notesToolPolicy() domain.ToolPolicy {
	tools := append([]string(nil), domain.CodeExplorationTools...)
	tools = append(tools, RecordNoteToolName, "save_memory", "search_memory")
	return domain.ToolPolicy{AllowTools: tools}
}

func (s *Service) notesWrittenCount(repoID uuid.UUID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notesWritten[repoID]
}

// appendScanStage loads the scan fresh, appends one event and rewrites it,
// mirroring the deterministic stages' own read-append-write so the notes
// stage never clobbers events another goroutine wrote in between.
func (s *Service) appendScanStage(ctx context.Context, scanID uuid.UUID, ev domain.ScanEvent) {
	current, err := s.store.GetScan(ctx, scanID)
	if err != nil {
		log.Warn().Err(err).Str("scan_id", scanID.String()).Msg("notes pass: load scan failed")
		return
	}
	current.Events = append(current.Events, ev)
	if err := s.store.UpdateScan(ctx, current); err != nil {
		log.Warn().Err(err).Str("scan_id", scanID.String()).Msg("notes pass: update scan failed")
	}
}

func buildNotesMessages(repo domain.Repository, brief string, existing []domain.ProjectNote, components []domain.Component) []domain.Message {
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: fmt.Sprintf(notesSystemPromptTemplate, repo.Name, RecordNoteToolName)},
	}
	if brief != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: "Verified repository facts (do not restate):\n\n" + brief})
	}
	if list := renderExistingNotesList(existing, components); list != "" {
		messages = append(messages, domain.Message{Role: domain.RoleSystem, Content: "Existing notes:\n\n" + list})
	}
	messages = append(messages, domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("Analyze %s and record your notes now.", repo.Name)})
	return messages
}

func renderExistingNotesList(notes []domain.ProjectNote, components []domain.Component) string {
	var b strings.Builder
	for _, n := range notes {
		scope := "repository"
		if n.ComponentID != nil {
			scope = n.ComponentID.String()
			if comp, ok := componentByID(components, *n.ComponentID); ok {
				scope = comp.Path
			}
		}
		state := ""
		if n.Stale {
			state = " (STALE)"
		}
		fmt.Fprintf(&b, "- %s [%s]%s\n", n.Topic, scope, state)
	}
	return b.String()
}

func componentByID(components []domain.Component, id uuid.UUID) (domain.Component, bool) {
	for _, c := range components {
		if c.ID == id {
			return c, true
		}
	}
	return domain.Component{}, false
}
