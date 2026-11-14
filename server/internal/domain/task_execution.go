package domain

// TaskExecution is one board task handed to an executor that is NOT the
// in-process agent loop. It carries the same four things the loop is called
// with (history, model, provider, policy) plus the two an out-of-process
// executor cannot derive for itself: WorkDir (the child process has to be
// started IN the task workspace, never the shared project root) and
// ResumeSessionID (a parked run resumes the executor's own session rather than
// starting over). It is a domain value object so the adapter implementing
// port.TaskExecutor does not have to import the application layer.
type TaskExecution struct {
	// History is the run's assembled context in the order the runner built it:
	// system blocks first, the trigger as the single user message, then the
	// task's own evidence. An executor that cannot send a message list
	// flattens it (see the claudecode adapter, which folds system blocks into
	// --append-system-prompt).
	History []Message
	// Model is what the run should use, already resolved through any
	// task_type_models override; empty means "the executor's own default".
	Model    string
	Provider LLMProviderType
	// MaxTurns caps the executor session, already resolved from the agent
	// record; 0 means the executor's own default.
	MaxTurns int
	// Effort is the CLI effort level (low…max), already resolved from the agent
	// record; empty means the executor's default.
	Effort string
	// Policy is the tool policy the loop would have enforced; an executor that
	// cannot enforce it must say so rather than pretend.
	Policy ToolPolicy
	// WorkDir is the task workspace. Required: an executor must never fall back
	// to the shared project root, where it would work on another task's branch.
	WorkDir string
	// SkillsOnDisk says the caller wrote this agent's skills into WorkDir as
	// files the CLI discovers by itself, so the session must not ALSO be
	// offered the tool that fetches a skill body over MCP. It travels with the
	// request because only the caller that materialised the workspace knows it
	// happened; setting it without materialising silently takes the run's only
	// access to its skills away.
	SkillsOnDisk bool
	// ResumeSessionID continues a parked executor session; empty starts fresh.
	ResumeSessionID string
	// Prompt is the new instruction for a RESUMED session, on its own: set with
	// ResumeSessionID by a follow-up step (criteria sweep, fix round, review
	// verdict) whose History the session already holds. Empty means the generic
	// "carry on" prompt a quota-parked run is woken with. Ignored when
	// ResumeSessionID is empty.
	Prompt string
	// CompactBeforeResume asks the executor to compact a large resumed session
	// before it continues (a revision round: the session holds every earlier
	// round's screenshots and command output, re-read on every turn). The
	// executor decides from the session's own size whether it is worth it, and
	// resumes the session as it is when compaction cannot run. Ignored when
	// ResumeSessionID is empty.
	CompactBeforeResume bool
	// TaskKey is the human-readable key (tt-123 style) used in logs and in the
	// short continue prompt a resumed session is given.
	TaskKey string
	// TaskTitle is the card's title, used for the same two purposes as TaskKey.
	TaskTitle string
	// Env is extra environment for the session, resolved by whoever can see the
	// checkout — what the repository DECLARED, in the names a session's own
	// version manager reads. It is on the request rather than the context
	// because the context overlay answers a different question (which INSTALL
	// on the machine a spawned process should find). Every name is one
	// SessionEnvAllowed accepts, every value already in tool form, so it is
	// passed on UNCHANGED. Empty must never be filled with a "system"/"latest"
	// default — none of those is something the repository said.
	Env map[string]string
}
