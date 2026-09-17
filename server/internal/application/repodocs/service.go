// Package repodocs lets a repository (or one monorepo sub-project) name which
// file documents its coding standards, test standards, architecture and local
// run instructions, and opens the board task that authors whichever of those
// four is missing.
package repodocs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TaskCreator opens the board task that authors a missing doc, and reads it
// back. It is the board's CreateTask/GetTask narrowed to what this package
// needs.
type TaskCreator interface {
	CreateTask(ctx context.Context, repositoryID uuid.UUID, req domain.CreateBoardTaskRequest) (domain.BoardTask, error)
	GetTask(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

// TaskPRMerger merges the bundle task's pull request. It is
// board.TaskPRService.MergeTaskPullRequest narrowed to one method, so this
// package depends on the use case rather than on the board service.
type TaskPRMerger interface {
	MergeTaskPullRequest(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.TaskPRMergeResult, error)
}

// RepositoryStore is the slice of the repository store this package needs:
// reading the repo to resolve its kind and sub-projects, writing back the doc
// path a generation task was just asked to produce, and remembering which task
// the outstanding doc bundle is in.
type RepositoryStore interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
	Update(ctx context.Context, id uuid.UUID, req domain.UpdateRepositoryRequest) (domain.Repository, error)
	SetDocsTaskID(ctx context.Context, id uuid.UUID, taskID string) error
}

type Service struct {
	repos  RepositoryStore
	tasks  TaskCreator
	merger TaskPRMerger
	agents func(ctx context.Context) ([]domain.Agent, error)
}

func NewService(repos RepositoryStore) *Service {
	return &Service{repos: repos}
}

// SetTaskCreator enables CreateDocTask; without it the service still exists
// but every call refuses.
func (s *Service) SetTaskCreator(tasks TaskCreator) { s.tasks = tasks }

// SetTaskPRMerger enables MergeDocsTask. Left unset on a deployment with no
// GitHub access, where the merge could only ever refuse.
func (s *Service) SetTaskPRMerger(m TaskPRMerger) { s.merger = m }

// SetAgentLister lets the doc-authoring task be assigned to the role that
// owns the repo (or sub-project) kind instead of landing unassigned.
func (s *Service) SetAgentLister(fn func(ctx context.Context) ([]domain.Agent, error)) {
	s.agents = fn
}

// CreateDocTask opens the board task that writes the missing doc — and, for
// local_run, a run script alongside it — at path, or at the kind's
// conventional .ai/ location when path is blank. The chosen path is recorded
// on the repository (or sub-project) immediately: the same convention
// InitialSetupDialog already uses for deploy addresses (see
// deploy.Service.CreateSetupTask) — a human decided where this lives before
// an agent ever wrote a word there, and the field should say so from the
// moment the task exists, not only once the PR lands.
func (s *Service) CreateDocTask(ctx context.Context, repositoryID uuid.UUID, subProjectPath, kind, path string) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	doc, err := resolveDoc(repo, DocItem{Kind: kind, SubProjectPath: subProjectPath, Path: path})
	if err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.setDocPath(ctx, repo, doc.subProjectPath, doc.kind, doc.path); err != nil {
		return domain.BoardTask{}, err
	}

	var b strings.Builder
	if doc.kind == domain.RepoDocLocalRun {
		fmt.Fprintf(&b, "Write or refresh %s for this %s repository.\n\n", doc.fullPath, doc.kindLabel)
	} else {
		fmt.Fprintf(&b, "Write or refresh %s for this %s repository. If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.\n\n", doc.fullPath, doc.kindLabel)
	}
	b.WriteString(docInstructions(doc))
	b.WriteString("\nAlso make sure this repository has a CLAUDE.md at its root with a short docs index; create a minimal one if it's missing, and add (or update) a line linking to this file so agents find it.\n")

	return s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           fmt.Sprintf("Write %s (%s)", doc.fullPath, doc.kindLabel),
		Description:     b.String(),
		TaskType:        domain.TaskTypeTask,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.roleAgent(ctx, domain.DeveloperAgentForKind(doc.kindLabel, repo.SubProjects)),
	})
}

// DocItem is one requested reference doc: which kind, whose sub-project (""
// for the repository itself), and where it should land ("" for the kind's
// conventional path).
type DocItem struct {
	Kind           string `json:"kind"`
	SubProjectPath string `json:"sub_project_path,omitempty"`
	Path           string `json:"path,omitempty"`
}

// resolvedDoc is a DocItem with everything settled: the path as it is
// persisted (relative to whatever the doc is scoped to), the path as the agent
// must write it (relative to the repository root), and the kind whose role
// owns the work.
type resolvedDoc struct {
	kind           string
	subProjectPath string
	path           string
	fullPath       string
	kindLabel      string
}

// resolveDoc validates one requested doc against the repository and fills in
// the defaults. It is the single definition CreateDocTask and
// CreateDocsBundleTask share, so a doc's path can never depend on which
// endpoint asked for it.
func resolveDoc(repo domain.Repository, item DocItem) (resolvedDoc, error) {
	kind := strings.TrimSpace(item.Kind)
	if !domain.ValidRepoDocKind(kind) {
		return resolvedDoc{}, fmt.Errorf("invalid doc kind %q", kind)
	}
	out := resolvedDoc{kind: kind, kindLabel: repo.Kind, subProjectPath: strings.TrimSpace(item.SubProjectPath)}
	dir := ""
	if out.subProjectPath != "" {
		out.kindLabel = domain.RepoKindBackend
		for _, sp := range repo.SubProjects {
			if sp.Path == out.subProjectPath {
				out.kindLabel = sp.Kind
				break
			}
		}
		dir = out.subProjectPath + "/"
	}
	out.path = strings.TrimSpace(item.Path)
	if out.path == "" {
		out.path = domain.DefaultRepoDocPath(kind)
	}
	out.fullPath = dir + out.path
	return out, nil
}

// docInstructions is what the agent is told to put in one doc. local_run is
// the odd one out and deliberately so: it asks for a script, not prose,
// because what someone on a fresh machine needs is something they can run.
func docInstructions(doc resolvedDoc) string {
	switch doc.kind {
	case domain.RepoDocCodingStandards:
		return "Cover: the formatting/lint tooling actually configured, naming and file-layout conventions, error-handling style, and anything this codebase does differently from a generic style guide. Read the existing code before writing — describe what it does, don't prescribe a generic standard.\n"
	case domain.RepoDocTestStandards:
		return "Cover: the test runner and how to invoke it, the testing pyramid this repo actually follows (unit/integration/e2e — only the layers that exist), coverage expectations if any, and how a new feature's tests should be structured here.\n"
	case domain.RepoDocArchitecture:
		return "Cover: the major components/layers and how they depend on each other, the data flow for a typical request or task, and the boundaries that must not be crossed (e.g. hexagonal layering, module isolation).\n"
	case domain.RepoDocLocalRun:
		var b strings.Builder
		fmt.Fprintf(&b, "This one is NOT a markdown guide: %s must be a COMPLETE, executable local bootstrap script.\n", doc.fullPath)
		b.WriteString("Running it on a fresh machine must leave the project running, with no other step:\n")
		b.WriteString("- install every dependency and toolchain the project needs (check first, install only what is missing)\n")
		b.WriteString("- prepare env/config: create the .env (or equivalent) from the example, fill in the local defaults, run whatever migrations and seeds a first run needs\n")
		b.WriteString("- start every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process\n")
		b.WriteString("- idempotent: running it a second time must be safe and must not duplicate anything\n")
		b.WriteString("- executable (`chmod +x`), with a `#!/usr/bin/env bash` shebang and `set -euo pipefail`\n")
		fmt.Fprintf(&b, "- a short usage header comment at the top: what it does, how to run it, and the port/URL it comes up on\n")
		fmt.Fprintf(&b, "- accepts an optional port argument (`%s [port]`) overriding the default, so it can be rerun when the default port is already in use\n", doc.fullPath)
		b.WriteString("Everything in it must match what this repository actually needs today — its real package manager, build tool and ports — not a generic template. Do not write a markdown guide instead of, or alongside, the script.\n")
		return b.String()
	}
	return ""
}

// CreateDocsBundleTask asks for several reference docs at once, in ONE board
// task, so all of them land in one branch and therefore one pull request.
//
// The alternative — one task per doc, which CreateDocTask still serves — puts
// four pull requests on a repository for what a human thinks of as a single
// piece of setup, each with its own review, its own CI run and its own merge.
// Every requested path is recorded on the repository (or sub-project) up front
// for the same reason CreateDocTask records its one: a human decided where
// these live before an agent wrote a word there.
func (s *Service) CreateDocsBundleTask(ctx context.Context, repositoryID uuid.UUID, items []DocItem) (domain.BoardTask, error) {
	if s.tasks == nil {
		return domain.BoardTask{}, fmt.Errorf("board is not available")
	}
	if len(items) == 0 {
		return domain.BoardTask{}, fmt.Errorf("no docs requested")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.BoardTask{}, err
	}
	docs := make([]resolvedDoc, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		doc, err := resolveDoc(repo, item)
		if err != nil {
			return domain.BoardTask{}, err
		}
		key := doc.subProjectPath + "\x00" + doc.kind
		if seen[key] {
			return domain.BoardTask{}, fmt.Errorf("duplicate doc kind %q for %s", doc.kind, docScopeLabel(doc))
		}
		seen[key] = true
		docs = append(docs, doc)
	}
	// Persisted before the task exists: a task that opens against paths the
	// repository does not yet claim would have the agent and the settings
	// screen disagreeing about where a doc lives for as long as the task runs.
	for _, doc := range docs {
		// Re-read each time: setDocPath writes the whole sub-project list, so
		// two docs on two sub-projects would otherwise each overwrite the
		// other's row from the same stale snapshot.
		current, err := s.repos.Get(ctx, repositoryID)
		if err != nil {
			return domain.BoardTask{}, err
		}
		if err := s.setDocPath(ctx, current, doc.subProjectPath, doc.kind, doc.path); err != nil {
			return domain.BoardTask{}, err
		}
	}

	task, err := s.tasks.CreateTask(ctx, repositoryID, domain.CreateBoardTaskRequest{
		Title:           "Generate reference docs",
		Description:     bundleDescription(repo, docs),
		TaskType:        domain.TaskTypeTask,
		Priority:        domain.TaskPriorityMedium,
		Column:          domain.TaskColumnTodo,
		CreatedBy:       "system",
		AssigneeAgentID: s.roleAgent(ctx, bundleRole(repo, docs)),
	})
	if err != nil {
		return domain.BoardTask{}, err
	}
	if err := s.repos.SetDocsTaskID(ctx, repositoryID, task.ID.String()); err != nil {
		return domain.BoardTask{}, err
	}
	return task, nil
}

// bundleDescription enumerates every requested doc with its exact target path
// and says, in as many words, that all of them belong to one branch.
func bundleDescription(repo domain.Repository, docs []resolvedDoc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Author the reference docs listed below for this %s repository, ALL of them in a single branch so exactly one pull request contains every file.\n\n", repo.Kind)
	b.WriteString("Do not open a pull request per document, and do not stop after the first one — the task is finished when every path below exists and is correct.\n\n")
	for i, doc := range docs {
		fmt.Fprintf(&b, "%d. `%s` — %s for %s\n", i+1, doc.fullPath, docKindLabel(doc.kind), docScopeLabel(doc))
	}
	for _, doc := range docs {
		fmt.Fprintf(&b, "\n---\n\n## `%s`\n\n", doc.fullPath)
		if doc.kind != domain.RepoDocLocalRun {
			b.WriteString("If it already exists, read it first and update whatever is stale or wrong against the current codebase rather than starting over; otherwise write it from scratch.\n")
		}
		b.WriteString(docInstructions(doc))
	}
	b.WriteString("\n---\n\nAlso make sure this repository has a CLAUDE.md at its root with a short docs index, and that it links to every file above so agents find them; create a minimal CLAUDE.md if it is missing.\n")
	return b.String()
}

func docKindLabel(kind string) string {
	switch kind {
	case domain.RepoDocCodingStandards:
		return "coding standards"
	case domain.RepoDocTestStandards:
		return "test standards"
	case domain.RepoDocArchitecture:
		return "architecture"
	case domain.RepoDocLocalRun:
		return "the local bootstrap script"
	}
	return kind
}

func docScopeLabel(doc resolvedDoc) string {
	if doc.subProjectPath == "" {
		return "the repository (" + doc.kindLabel + ")"
	}
	return doc.subProjectPath + " (" + doc.kindLabel + ")"
}

// DocsTaskStatus is what the docs screen shows for the outstanding bundle: the
// task, where it has got to, and the pull request it is being reviewed in. All
// fields empty means no bundle is outstanding.
type DocsTaskStatus struct {
	TaskID   string `json:"task_id"`
	TaskKey  string `json:"task_key,omitempty"`
	Title    string `json:"title,omitempty"`
	Column   string `json:"column"`
	PRURL    string `json:"pr_url"`
	PRNumber int    `json:"pr_number,omitempty"`
	Merged   bool   `json:"merged"`
}

// DocsTask reports the repository's outstanding doc-bundle task. A recorded id
// that no longer resolves to a task (it was deleted from the board) reads as
// "none outstanding" rather than as an error: the card is gone, and the screen
// asking should offer to open a new bundle.
func (s *Service) DocsTask(ctx context.Context, repositoryID uuid.UUID) (DocsTaskStatus, error) {
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return DocsTaskStatus{}, err
	}
	task, ok, err := s.docsTask(ctx, repo)
	if err != nil || !ok {
		return DocsTaskStatus{}, err
	}
	return DocsTaskStatus{
		TaskID:   task.ID.String(),
		TaskKey:  task.Key,
		Title:    task.Title,
		Column:   string(task.Column),
		PRURL:    task.PRURL,
		PRNumber: task.PRNumber,
		Merged:   strings.TrimSpace(task.MergeCommitSHA) != "",
	}, nil
}

func (s *Service) docsTask(ctx context.Context, repo domain.Repository) (domain.BoardTask, bool, error) {
	id := strings.TrimSpace(repo.DocsTaskID)
	if id == "" || s.tasks == nil {
		return domain.BoardTask{}, false, nil
	}
	taskID, err := uuid.Parse(id)
	if err != nil {
		return domain.BoardTask{}, false, nil
	}
	task, err := s.tasks.GetTask(ctx, repo.ID, taskID)
	if err != nil {
		return domain.BoardTask{}, false, nil
	}
	return task, true, nil
}

// MergeDocsTask merges the doc bundle's pull request into the default branch
// and forgets the task.
//
// It is the same merge every other task gets — board.TaskPRService applies its
// whole ladder of refusals, including the board's own pipeline verdict, which
// already tolerates a repository with no CI mapped at all (only a FAILED
// pipeline blocks; "no pipeline" and "skipped" do not). A docs-only PR on a
// repo without CI therefore merges, and nothing here has to weaken a gate to
// let it.
func (s *Service) MergeDocsTask(ctx context.Context, repositoryID uuid.UUID) (domain.TaskPRMergeResult, error) {
	if s.merger == nil {
		return domain.TaskPRMergeResult{}, fmt.Errorf("this deployment has no GitHub pull-request access wired up")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.TaskPRMergeResult{}, err
	}
	task, ok, err := s.docsTask(ctx, repo)
	if err != nil {
		return domain.TaskPRMergeResult{}, err
	}
	if !ok {
		return domain.TaskPRMergeResult{}, fmt.Errorf("no reference-doc task is outstanding for this repository")
	}
	result, err := s.merger.MergeTaskPullRequest(ctx, repositoryID, task.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrMergeAlreadyMerged) {
			return domain.TaskPRMergeResult{}, err
		}
		// The gate refuses a second merge attempt on purpose — merging is
		// irreversible and must never be retried — but from this screen's
		// point of view the PR is exactly as landed as one this call merged
		// itself: whoever merged it (an earlier call here, the QA agent's own
		// merge_task_pull_request, or a person on GitHub) already did the
		// work. Reporting that as a failure is what sent the user back to a
		// "save" button for a PR that no longer exists.
		result, err = s.alreadyMergedResult(ctx, repositoryID, task)
		if err != nil {
			return domain.TaskPRMergeResult{}, err
		}
	}
	// Cleared whenever the PR is settled, merged now or merged earlier: a
	// failure to forget the task is worth reporting, but it must not read as
	// a failed merge — the docs are on the default branch either way.
	if clearErr := s.repos.SetDocsTaskID(ctx, repositoryID, ""); clearErr != nil {
		result.Message = strings.TrimSpace(result.Message + " WARNING: the docs task could not be cleared from the repository (" + clearErr.Error() + ").")
	}
	return result, nil
}

// alreadyMergedResult re-reads the task after a refused "already merged"
// attempt, since the gate may have just recorded the commit GitHub reports
// rather than the stale snapshot this call started with.
func (s *Service) alreadyMergedResult(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) (domain.TaskPRMergeResult, error) {
	current, err := s.tasks.GetTask(ctx, repositoryID, task.ID)
	if err != nil {
		current = task
	}
	msg := "Pull request was already merged; nothing further was needed."
	if current.PRNumber > 0 {
		msg = "Pull request #" + strconv.Itoa(current.PRNumber) + " was already merged; nothing further was needed."
	}
	return domain.TaskPRMergeResult{
		Merged:         true,
		PRNumber:       current.PRNumber,
		PRURL:          current.PRURL,
		MergeCommitSHA: current.MergeCommitSHA,
		Message:        msg,
	}, nil
}

// setDocPath records where a doc kind lives, scoped to the repository itself
// ("") or to one of its sub-projects.
func (s *Service) setDocPath(ctx context.Context, repo domain.Repository, subProjectPath, kind, path string) error {
	if subProjectPath == "" {
		docs := repo.Docs
		setDocKind(&docs, kind, path)
		_, err := s.repos.Update(ctx, repo.ID, domain.UpdateRepositoryRequest{Docs: &docs})
		return err
	}
	subs := make([]domain.RepoSubProject, len(repo.SubProjects))
	copy(subs, repo.SubProjects)
	found := false
	for i := range subs {
		if subs[i].Path == subProjectPath {
			setDocKind(&subs[i].Docs, kind, path)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("sub-project not found: %s", subProjectPath)
	}
	_, err := s.repos.Update(ctx, repo.ID, domain.UpdateRepositoryRequest{SubProjects: &subs})
	return err
}

func setDocKind(docs *domain.RepositoryDocs, kind, path string) {
	switch kind {
	case domain.RepoDocCodingStandards:
		docs.CodingStandards = path
	case domain.RepoDocTestStandards:
		docs.TestStandards = path
	case domain.RepoDocArchitecture:
		docs.Architecture = path
	case domain.RepoDocLocalRun:
		docs.LocalRun = path
	}
}

// roleAgent resolves the agent that owns doc-authoring work for a repo kind.
// Doc work is repo-wide, so a monorepo goes to the architect rather than to
// one of its sub-project developers — same rule as deploy.Service.roleAgent.
func (s *Service) roleAgent(ctx context.Context, role string) *uuid.UUID {
	if s.agents == nil {
		return nil
	}
	agents, err := s.agents(ctx)
	if err != nil {
		return nil
	}
	want := role
	for i := range agents {
		if agents[i].Name == want {
			id := agents[i].ID
			return &id
		}
	}
	return nil
}

// bundleRole picks who authors a docs bundle: the developer owning most of the
// docs in it. A sub-project doc counts as that sub-project's kind and a
// monorepo-level doc counts as every sub-project, so a bundle of backend docs
// goes to backend-developer. It used to go to the system architect for any
// monorepo, and the architect refuses to author files into a branch.
func bundleRole(repo domain.Repository, docs []resolvedDoc) string {
	owners := make([]domain.RepoSubProject, 0, len(docs))
	for _, doc := range docs {
		if doc.kindLabel == domain.RepoKindMonorepo {
			owners = append(owners, repo.SubProjects...)
			continue
		}
		owners = append(owners, domain.RepoSubProject{Kind: doc.kindLabel})
	}
	return domain.DeveloperAgentForKind(domain.RepoKindMonorepo, owners)
}
