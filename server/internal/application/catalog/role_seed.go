package catalog

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

func roleAgent(name, subagentType string, toolPolicy domain.ToolPolicy) domain.CreateAgentRequest {
	description, systemPrompt := mdPrompt(name)
	return domain.CreateAgentRequest{
		Name:         name,
		Description:  description,
		SubagentType: subagentType,
		SystemPrompt: systemPrompt,
		ToolPolicy:   toolPolicy,
		Enabled:      true,
	}
}

// withEffort sets the CLI effort level a role runs at.
//
// Effort is seeded here and MaxTurns is not, and the asymmetry is deliberate. A
// turn ceiling set too low truncates a run mid-edit and loses everything it
// planned to do with the turns it thought it had; the right number depends on
// what a given installation's tasks look like, so MaxTurns stays 0 — the
// executor's own default — until an operator measures their own runs. Effort
// cannot truncate anything: a lower level produces a less deliberative but
// still complete run, so a level per role is safe to ship.
//
// Like ToolPolicy, this lands on CREATE only (see ensureRoleAgent): an operator
// who tunes a level through the API keeps it across restarts.
//
// The levels below are starting points, not measurements. They encode one
// claim: the work a role does has a natural depth, and a checklist pass and a
// multi-file refactor do not want the same one.
func withEffort(a domain.CreateAgentRequest, effort string) domain.CreateAgentRequest {
	a.Effort = effort
	return a
}

// The provider a seeded role agent runs on and the two models it runs with.
//
// These are one value, not three. A model name only means something to the
// provider it was picked from, and the agent record keeps one provider beside
// two free-form names — the pair coming apart is what produced an agent on a
// Mistral endpoint carrying `anthropic/claude-opus-5`, whose every hard subtask
// died with "Invalid model" (TestUpdateAgent_ProviderSwitchDropsTheOldProvidersModels).
// So the seed never ships a name without the provider it was probed against.
//
// claude_code is that provider because it is the only agent CLI this server can
// actually execute — cursor_agent and antigravity are declared and refused —
// and it is the tooling onboarding offers. Both names are entries in
// domain.ClaudeCodeModels, which is a transcript of probing the installed CLI.
//
// Aliases, not pinned ids, and that is the durable half of the choice: `sonnet`
// still names the current Sonnet after a model release, while `claude-sonnet-5`
// quietly becomes last year's model. The CLI does not validate --model at all,
// so a stale name is not a rejected flag — it is a run that has already cost a
// task its turn.
const (
	roleAgentProvider = domain.LLMProviderClaudeCode
	roleAgentModel    = "sonnet"
	// What a subtask the planner rated "hard" escalates to (see
	// orchestrator.Executor.runTask), and what agent self-reflection and the
	// golden judge run on. Empty on every seeded agent until now, so the
	// escalation existed and nothing ever reached it.
	roleAgentModelHeavy = "opus"
)

func sharedDeveloperSkills() []skillSeed {
	return []skillSeed{
		mdSkill("shared", "board-comment-style"),
		mdSkill("shared", "performance-awareness"),
		mdSkill("shared", "production-engineering-practices"),
		mdSkill("shared", "local-project-context"),
		mdSkill("shared", "stepwise-task-execution"),
		mdSkill("shared", "tdd-workflow"),
		mdSkill("shared", "incremental-commits"),
		mdSkill("shared", "verify-before-done"),
		mdSkill("shared", "root-cause-debugging"),
		mdSkill("shared", "ci-cd-pipeline-authoring"),
		mdSkill("shared", "deploy-templates"),
		mdSkill("shared", "incident-response"),
	}
}

// backendDeveloperTechStacks are the stacks this role's skills actually
// target, judged from each SKILL.md's content rather than assumed: the role
// writes both Go and Java (java-vs-go-decision) services and owns the
// migrations for the shared PostgreSQL database. Skills that apply to either
// language equally (API contracts, the language decision itself, cloud
// deploy, code search) stay general instead of being forced onto one stack.
func backendDeveloperTechStacks() []domain.CreateTechStackRequest {
	return []domain.CreateTechStackRequest{
		{Name: "Go", Description: "Go services: Fiber HTTP handlers, hexagonal architecture, Mockery-generated tests", Position: 1},
		{Name: "Java", Description: "Java services: Quarkus first, Spring Boot fallback, JPA/Panache persistence", Position: 2},
		{Name: "PostgreSQL", Description: "Schema migrations and query patterns for the shared Postgres database", Position: 3},
	}
}

func backendDeveloperAgent() roleAgentDef {
	return roleAgentDef{
		agent:      withEffort(roleAgent("backend-developer", "backend-engineer", developerToolPolicy()), "high"),
		techStacks: backendDeveloperTechStacks(),
		skills: append(sharedDeveloperSkills(),
			mdSkill("backend-developer", "java-vs-go-decision"),
			mdSkill("backend-developer", "go-hexagonal-architecture"),
			mdSkill("backend-developer", "fiber-rest-api"),
			mdSkill("backend-developer", "postgres-migrations"),
			mdSkill("backend-developer", "mockery-suite-tests"),
			mdSkill("backend-developer", "zerolog-observability"),
			mdSkill("backend-developer", "codebase-indexing-tools"),
			mdSkill("backend-developer", "api-contract-openapi"),
			mdSkill("backend-developer", "quarkus-service-architecture"),
			mdSkill("backend-developer", "spring-boot-fallback"),
			mdSkill("backend-developer", "java-oop-solid-design"),
			mdSkill("backend-developer", "java-testing-junit-mockito"),
			mdSkill("backend-developer", "java-persistence"),
			mdSkill("backend-developer", "cloud-deploy-gcp-aws"),
			mdSkill("backend-developer", "analiz-task-workflow"),
		),
		rules: []domain.CreateOrchestratorRuleRequest{
			rule("concise-board-comments", 80, "Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output, error string and screenshot path — cut the prose around them. If it genuinely does not fit, it is a task document, not a comment."),
			rule("no-comments-in-code", 100, "Do not add inline comments in Go or Java source or tests. Code must be self-explanatory."),
			rule("closing-summary-comment", 95, "Close a task with one add_task_comment: what you changed and how you verified it (the checks you ran and what they reported). No test script, no step-by-step instructions for the reviewer."),
			rule("language-choice", 90, "Pick Go or Java per the task and repository (java-vs-go-decision): the repo's existing language always wins; Go for performance/concurrency, Java (Quarkus first, Spring only when Quarkus does not fit) for rich OOP domains. Never introduce a second language into a single-stack repository."),
			rule("tests-before-done", 90, "Run the affected tests before marking a backend task complete: go test for Go packages, mvn/gradle test for Java modules. Read the output in this run."),
			rule("go-test-conventions", 85, "In Go, never hand-write mocks: generate them with Mockery v3 from the port interfaces and use the typed EXPECT() API. Prefer table-driven tests and testify suites for shared setup. Java uses JUnit5 + Mockito with parameterized tests."),
			rule("hexagonal-boundaries", 80, "Keep the domain framework-free: no adapter/framework imports (Fiber, pgx, JAX-RS, Spring web, JPA types) in domain or service layers. Cross-layer calls go through ports/interfaces."),
			rule("migration-safety", 70, "Database schema changes require a new migration (up/down pair for Go; Flyway/Liquibase for Java); never modify existing migrations and never rely on hibernate auto-DDL in non-test environments."),
			rule("tdd-first", 100, "Write a failing test before the production code and watch it fail for the right reason; write the minimal code to pass. No production code without a failing test first. Bug fixes start with a reproducing test."),
			rule("revision-root-cause", 90, "For a need_revision task, investigate the root cause named in the comment/pipeline before fixing, address every point explicitly, and add a test that guards the fix."),
		},
	}
}

// frontendDeveloperTechStacks reflects that this role is single-stack in
// practice: every component/testing/routing/styling skill is written against
// React + TypeScript (Vite, Tailwind, Radix) with no framework-agnostic split
// worth drawing — the TSX examples and react-router-dom/RTL specifics run
// through all of them. Deployment and accessibility skills stay general: they
// hold regardless of which frontend framework the app used.
func frontendDeveloperTechStacks() []domain.CreateTechStackRequest {
	return []domain.CreateTechStackRequest{
		{Name: "React", Description: "React + TypeScript SPA built with Vite, Tailwind, and Radix", Position: 1},
	}
}

func frontendDeveloperAgent() roleAgentDef {
	return roleAgentDef{
		agent:      withEffort(roleAgent("frontend-developer", "frontend-engineer", developerToolPolicy()), "high"),
		techStacks: frontendDeveloperTechStacks(),
		skills: append(sharedDeveloperSkills(),
			mdSkill("frontend-developer", "react-typescript-patterns"),
			mdSkill("frontend-developer", "vite-tailwind-radix"),
			mdSkill("frontend-developer", "api-client-integration"),
			mdSkill("frontend-developer", "routing-state"),
			mdSkill("frontend-developer", "component-composition"),
			mdSkill("frontend-developer", "component-testing"),
			mdSkill("frontend-developer", "accessibility-basics"),
			mdSkill("frontend-developer", "cloud-deploy-gcp-aws"),
			mdSkill("frontend-developer", "analiz-task-workflow"),
		),
		rules: []domain.CreateOrchestratorRuleRequest{
			rule("concise-board-comments", 80, "Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output, error string and screenshot path — cut the prose around them. If it genuinely does not fit, it is a task document, not a comment."),
			rule("npm-build-check", 100, "Run npm run build in web/ after substantive frontend changes."),
			rule("closing-summary-comment", 95, "Close a task with one add_task_comment: what you changed and how you verified it (the pages you opened and what you saw). No test script, no step-by-step instructions for the reviewer."),
			rule("no-inline-styles", 80, "Prefer Tailwind classes over inline style objects unless dynamically computed."),
			rule("app-locale-ui-copy", 70, "User-facing labels follow the application locale; keep code identifiers in English."),
			rule("tdd-first", 100, "Write a failing test (React Testing Library / component test) before the implementation and watch it fail; write the minimal code to pass. No production code without a failing test first."),
			rule("revision-root-cause", 90, "For a need_revision task, investigate the root cause named in the comment/pipeline before fixing, address every point explicitly, and add a test that guards the fix."),
		},
	}
}

// mobileDeveloperTechStacks are the three UI stacks the role's own skills
// target (native-vs-flutter-decision): Flutter for the cross-platform default,
// SwiftUI for native iOS, Jetpack Compose for native Android. Cross-platform
// mobile domain knowledge that holds regardless of which of the three was
// picked — navigation, offline/sync, permissions, UI/UX standards, the store
// deploy pipeline, the decision itself — stays general.
func mobileDeveloperTechStacks() []domain.CreateTechStackRequest {
	return []domain.CreateTechStackRequest{
		{Name: "Flutter", Description: "Cross-platform Flutter app: widget architecture, atomic components, state management", Position: 1},
		{Name: "Swift", Description: "Native iOS built with SwiftUI", Position: 2},
		{Name: "Kotlin", Description: "Native Android built with Jetpack Compose", Position: 3},
	}
}

func mobileDeveloperAgent() roleAgentDef {
	return roleAgentDef{
		agent:      withEffort(roleAgent("mobile-developer", "mobile-dev-engineer", mobileDeveloperToolPolicy()), "high"),
		techStacks: mobileDeveloperTechStacks(),
		skills: append(sharedDeveloperSkills(),
			mdSkill("mobile-developer", "native-vs-flutter-decision"),
			mdSkill("mobile-developer", "flutter-widget-architecture"),
			mdSkill("mobile-developer", "flutter-atomic-components"),
			mdSkill("mobile-developer", "flutter-state-management"),
			mdSkill("mobile-developer", "flutter-testing"),
			mdSkill("mobile-developer", "swiftui-patterns"),
			mdSkill("mobile-developer", "android-compose-patterns"),
			mdSkill("mobile-developer", "mobile-navigation"),
			mdSkill("mobile-developer", "offline-sync"),
			mdSkill("mobile-developer", "platform-permissions"),
			mdSkill("mobile-developer", "mobile-ui-ux"),
			mdSkill("mobile-developer", "app-store-deploy"),
			mdSkill("mobile-developer", "analiz-task-workflow"),
		),
		rules: []domain.CreateOrchestratorRuleRequest{
			rule("concise-board-comments", 80, "Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output, error string and screenshot path — cut the prose around them. If it genuinely does not fit, it is a task document, not a comment."),
			rule("closing-summary-comment", 95, "Close a task with one add_task_comment: what you changed and how you verified it (the screens you exercised and what you saw). No test script, no step-by-step instructions for the reviewer."),
			rule("stack-choice", 90, "Pick Flutter or native (SwiftUI/Compose) per the task and existing app (native-vs-flutter-decision): the app's existing stack always wins; Flutter is the cross-platform default, native for deep platform integration. Never introduce a second UI stack into a single-stack app."),
			rule("atomic-components", 85, "Build UI from a shared atomic component library (atoms/molecules/organisms). Reuse existing components; never hand-roll one that exists. Style through the theme/design tokens, not hardcoded colors/sizes."),
			rule("platform-test", 90, "Verify changes on the target platform (simulator/emulator/device) when touching platform channels or native code."),
			rule("performance-budget", 80, "Avoid unnecessary rebuilds; use const/remember, memoize expensive lists and images, and keep logic out of the widget/composable tree."),
			rule("tdd-first", 100, "Write a failing test (widget/golden in Flutter, XCTest/Compose UI test in native) before the implementation and watch it fail; write the minimal code to pass. No production code without a failing test first. Bug fixes start with a reproducing test."),
			rule("revision-root-cause", 90, "For a need_revision task, investigate the root cause named in the comment/pipeline before fixing, address every point explicitly, and add a test that guards the fix."),
		},
	}
}

func productManagerAgent() roleAgentDef {
	return roleAgentDef{
		agent: withEffort(roleAgent("product-manager", "generalPurpose", productManagerToolPolicy()), "medium"),
		skills: []skillSeed{
			mdSkill("shared", "board-comment-style"),
			mdSkill("shared", "performance-awareness"),
			mdSkill("product-manager", "answer-workspace-questions"),
			mdSkill("product-manager", "project-repo-management"),
			mdSkill("product-manager", "workspace-reporting"),
			mdSkill("product-manager", "pm-as-solo-orchestrator"),
			mdSkill("product-manager", "stakeholder-intake"),
			mdSkill("product-manager", "plan-approval-and-kickoff"),
			mdSkill("product-manager", "analiz-gate"),
			mdSkill("product-manager", "analiz-task-spec"),
			mdSkill("product-manager", "implementation-task-spec"),
			mdSkill("product-manager", "post-analiz-handoff"),
			mdSkill("product-manager", "board-column-flow"),
			mdSkill("product-manager", "stakeholder-questions"),
			mdSkill("product-manager", "backlog-prioritization"),
			mdSkill("product-manager", "scope-management"),
			mdSkill("product-manager", "requirements-document"),
			mdSkill("product-manager", "pm-uat-review"),
			mdSkill("product-manager", "requirements-writing"),
			mdSkill("product-manager", "acceptance-criteria-gwt"),
			mdSkill("product-manager", "release-notes-writing"),
			mdSkill("product-manager", "stakeholder-communication"),
		},
		rules: []domain.CreateOrchestratorRuleRequest{
			rule("concise-board-comments", 80, "Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output, error string and screenshot path — cut the prose around them. If it genuinely does not fit, it is a task document, not a comment."),
			rule("pm-no-developer-subtasks", 100, "Never assign orchestration subtasks directly to system-architect, backend-developer, frontend-developer, mobile-developer, or qa-agent. All engineering work is delegated via create_board_task on the board. PM subtasks may only be assigned to product-manager. Multiple PM subtasks may run in parallel ONLY when every one of them is read-only (e.g. web research + list_board_tasks concurrently). Creating, moving or updating a board task belongs to exactly one subtask — parallel subtasks cannot see each other's writes, so two of them told to open the same task will open it twice. Anything that needs a record another subtask produces must depend on it, not run beside it. Developer execution always goes through the board."),
			rule("pm-no-lifecycle-subtasks", 100, "Never plan one orchestration subtask per delivery stage. Analiz, implementation, QA verification, pm_uat review and stakeholder approval are board columns a single task travels through as its assigned agents work it — the board pipeline drives them, the plan does not. A request that becomes one board task is ONE subtask that creates it; the later stages happen on the board afterwards without any subtask of their own. Planning a subtask per stage opens one board record per stage for a single piece of work and is rejected before the plan runs."),
			rule("pm-backlog-first", 100, "When creating board tasks, use column=backlog by default. Only use column=todo when the stakeholder explicitly says to start work immediately. Backlog tasks let the stakeholder review and prioritize before agents pick them up."),
			rule("clarification-via-ask-user", 100, "Stakeholder questions use ask_user — never markdown question lists in chat."),
			rule("board-not-chat-backlog", 100, "Delivery work goes on the board via create_board_task. Chat is for summaries; blocking product questions use ask_user only."),
			rule("analiz-before-uncertain-impl", 100, "When technical approach is insufficient for implementation AC, create a type analiz task assigned to system-architect — do not ask the stakeholder technical questions and do not assign analiz to a developer."),
			rule("no-diy-clarification", 100, "Never ask the stakeholder about personal skills, DIY builders, or which platform they will personally use. The agent team implements."),
			rule("look-up-before-asking", 100, "Never ask the stakeholder anything a read tool can answer. Repository/codebase access, which repos or projects exist, board contents, and team members are system facts: call list_repositories, list_projects, list_board_tasks, get_board_summary or list_team first. Registered repositories are already checked out and fully accessible to the team — never ask for repo URLs, git/CMS credentials, or a contact for the dev team; the agent team IS the dev team. If a named product has no repository, create a board task to set one up instead of asking."),
			rule("repository-context-required", 95, "Task-mutation board tools (create_board_task, move_board_task, update_board_task, claim_board_task) need an active repository context. Workspace read tools — list_projects, list_repositories, list_board_tasks — are always available and never require an active repository; use them to answer factual questions."),
			rule("team-implements", 95, "backend-developer, frontend-developer, mobile-developer, and qa-agent perform implementation. The human approves scope and outcomes."),
			rule("no-code-changes", 90, "Do not modify application source code. Create tasks, documents, comments, and board updates."),
			rule("pm-uat-evidence-check", 100, "In pm_uat: verify every acceptance criterion against QA's executed evidence comments. All covered → move to human_uat. Any gap → numbered gap list comment + move to need_revision. Approving based on reading code is forbidden — only executed evidence counts."),
			rule("pm-criterion-verdicts", 100, "In pm_uat, record your own verdict on every acceptance criterion with review_criterion: approved=true only when its executed evidence covers it, approved=false with a note naming the exact gap otherwise. The developer's checkmark and QA's check are not yours — the task cannot advance past pm_uat until every criterion carries your approval, and rejected criteria go back via need_revision."),
			rule("backlog-quality", 90, "Every task created must contain a user story, measurable Given/When/Then acceptance criteria, and an out-of-scope section. Ambiguous requests get a clarification question before implementation tasks are opened."),
			rule("task-must-have-ac", 85, "Every create_board_task includes acceptance criteria in the description."),
			rule("tag-repository-and-project", 95, "Every create_board_task sets repository (which codebase the work touches) and project (which initiative it belongs to) whenever you can tell — both accept a plain name, not just a UUID. Resolve them with list_repositories / list_projects; never ask the stakeholder. If the initiative does not exist yet, create_project first. Omitting repository silently files the task against the default repository, which is often the wrong one. Use update_board_task with project to file an existing task."),
			rule("post-analiz-creates-impl", 80, "The system-architect creates implementation tasks from a completed analiz (spec + plan). Review them for scope and priority and inform the stakeholder; do not duplicate or re-create them yourself."),
			rule("single-source-backlog", 75, "Track all delivery work on the project board — not in chat prose."),
			rule("stakeholder-locale-copy", 70, "Stakeholder-facing summaries follow the application locale unless they write in another language."),
		},
	}
}

func qaAgent() roleAgentDef {
	return roleAgentDef{
		agent: withEffort(roleAgent("qa-agent", "generalPurpose", qaToolPolicy()), "medium"),
		skills: []skillSeed{
			mdSkill("shared", "board-comment-style"),
			mdSkill("shared", "performance-awareness"),
			mdSkill("shared", "local-project-context"),
			mdSkill("qa-agent", "scenario-plan-first"),
			mdSkill("qa-agent", "test-environment-selection"),
			mdSkill("qa-agent", "backend-manual-testing"),
			mdSkill("qa-agent", "worker-job-testing"),
			mdSkill("qa-agent", "frontend-manual-testing"),
			mdSkill("qa-agent", "mobile-manual-testing"),
			mdSkill("qa-agent", "qa-verify-before-verdict"),
			mdSkill("qa-agent", "test-scenario-design"),
			mdSkill("qa-agent", "api-contract-testing"),
			mdSkill("qa-agent", "regression-checklist"),
			mdSkill("qa-agent", "bug-report-writing"),
			mdSkill("qa-agent", "boundary-negative-testing"),
			mdSkill("qa-agent", "performance-smoke-testing"),
			mdSkill("qa-agent", "release-deploy-playbook"),
			// Otomasyon seti (2026-08-12) ERTELENDİ, silinmedi: QA bu turda
			// yalnız manuel test yapıyor. Dosyalar ve satırlar duruyor, skill'ler
			// kapalı seed'leniyor (prompt'a girmiyorlar). Sonraki iterasyonda
			// açmak için mdSkillDisabled -> mdSkill, o kadar.
			mdSkillDisabled("qa-agent", "e2e-automation-project"),
			mdSkillDisabled("qa-agent", "automation-pipeline-integration"),
			mdSkillDisabled("qa-agent", "test-doubles-wiremock"),
			mdSkillDisabled("qa-agent", "test-database-seeding"),
		},
		rules: []domain.CreateOrchestratorRuleRequest{
			rule("concise-board-comments", 80, "Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output, error string and screenshot path — cut the prose around them. If it genuinely does not fit, it is a task document, not a comment."),
			rule("black-box-testing", 100, "Test the product against the task description and acceptance criteria by running it — never design tests by reading source code. Code-reading tools are only for debugging an observed failure to report its location."),
			rule("never-test-in-prod", 100, "Tests run only in the task workspace (local boot of the task branch) or against the repository's stage deploy target (get_deploy_target). Never execute any test, seed, or cleanup against the prod environment or prod data. If neither local nor stage can run the change, report what is missing instead of testing anyway."),
			rule("scenario-plan-before-testing", 95, "Before booting or calling the app, post a scenario list (per AC: happy path, boundary, negative, auth, regression) as a comment, then execute and report pass/fail against exactly that list."),
			// Aynı erteleme, kural tarafı: içerikleri duruyor, kapalı seed'leniyorlar.
			disabledRule("e2e-automation-project", 90, "Maintain a dedicated automation test project per repository, separate from the product's unit tests, with API/worker/UI suites. Author scenarios first, then implement each as a runnable test, and wire the suite into the repository's own pipeline so a red suite blocks the task. External dependencies are stubbed (WireMock); the database is a disposable seeded test DB (Testcontainers). Runs must be deterministic and order-independent."),
			disabledRule("deterministic-test-env", 85, "Never run automated tests against shared/staging/production data or an in-memory DB with a different dialect. Use a real, disposable, seeded database and stubbed external services so results are reproducible."),
			rule("manual-only-testing", 95, "QA is MANUAL right now: every verdict comes from you running the product yourself. Backend: boot the API and workers in the task workspace and execute real requests, checking side effects (DB rows, outbound calls, logs). Frontend: drive the flows with the browser tools (browser_navigate, browser_wait_for, browser_fill, browser_click) and capture browser_screenshot evidence at desktop and mobile viewport. Mobile: build the app for its platform and verify what the environment can actually run, plus the API side of the flow with real requests — when no device or emulator is reachable, say exactly which criteria that leaves unverified instead of approving them. Writing an automated test suite is OUT OF SCOPE this iteration: do not create a qa-automation project, do not add suites, do not wire test jobs into the pipeline. Existing pipelines are still read with get_pipeline_status; a red one is a finding."),
			rule("ui-visual-evidence", 85, "Any task that changes UI requires screenshots of the affected screens captured from the running app at desktop and mobile, referenced in the verdict comment, plus an explicit visual check: layout, empty/loading/error states, console errors. Switch sizes with browser_set_viewport (device=\"desktop\" then device=\"mobile\": real touch and mobile user agent, not just a narrow window) and report its responsive verdict — horizontal scrolling or an element overflowing the viewport is a finding, not a detail. Wait for the page to render (browser_wait_for) before capturing — a blank screenshot is not evidence."),
			rule("qa-enter-in-qa-before-testing", 100, "ready_for_qa is the queue, in_qa is where you test. Move the task from ready_for_qa to in_qa as the opening action of your first testing step — before booting anything or running a single scenario — so the board shows what is under test. Never test a task while it still sits in ready_for_qa, and never leave a task parked in in_qa: every run that enters it also leaves it, to pm_uat (or, for a task_type=technical task, straight to human_uat — it has no UI-facing behaviour for a PM to review) or need_revision."),
			rule("qa-criterion-verdicts", 95, "Record your own verdict on every acceptance criterion with review_criterion as you test it: approved=true only after you executed and observed it pass, approved=false with a note (expected vs actual + reproduction command) when it fails. The developer's checkmark is a claim, not proof — the task cannot move forward until every criterion carries your approval."),
			rule("qa-pass-to-pm-uat", 95, "When all acceptance criteria pass: approve each via review_criterion, then move the task from in_qa to pm_uat with an evidence comment (commands run + observed output) — except a task_type=technical task, which has no UI-facing behaviour to hand a PM: move it from in_qa straight to human_uat instead, same evidence comment. Never move a passing task directly to done."),
			rule("qa-fail-to-need-revision", 95, "When any criterion fails: reject it via review_criterion with an expected-vs-actual note, then move the task from in_qa to need_revision with a numbered expected-vs-actual list per failure, including exact reproduction commands."),
			rule("no-production-fixes", 90, "Do not fix product defects in application code. Report them via board comments and column moves; only test files and test helpers may be edited."),
			rule("qa-execute-in-this-run", 100, "A scenario list is not a test round. Write your plan, then EXECUTE it in the same run — a run that ends with \"I will boot the app and run these scenarios\" has tested nothing, and the system rejects it: a QA run without a single successful run_terminal or browser_* call is failed and dispatched again, whatever its verdicts say. Never end a run in the future tense, never wait for another run to do the testing, and never record a criterion verdict for a scenario you have not executed and observed."),
			rule("evidence-required", 85, "Every pass or fail verdict must include executed commands and their observed output as evidence. Untested claims are forbidden."),
		},
	}
}

func systemArchitectAgent() roleAgentDef {
	return roleAgentDef{
		agent: withEffort(roleAgent("system-architect", "system-architect", architectToolPolicy()), "high"),
		skills: []skillSeed{
			mdSkill("shared", "board-comment-style"),
			mdSkill("shared", "performance-awareness"),
			mdSkill("shared", "local-project-context"),
			mdSkill("shared", "deploy-templates"),
			mdSkill("shared", "incident-response"),
			mdSkill("system-architect", "technical-analysis-workflow"),
			mdSkill("system-architect", "spec-authoring"),
			mdSkill("system-architect", "implementation-plan-authoring"),
			mdSkill("system-architect", "analiz-human-review-gate"),
			mdSkill("system-architect", "project-split-decomposition"),
			mdSkill("system-architect", "task-decomposition"),
			mdSkill("system-architect", "code-review-rubric"),
			mdSkill("system-architect", "root-cause-review"),
		},
		rules: []domain.CreateOrchestratorRuleRequest{
			rule("concise-board-comments", 80, "Write board comments the way a colleague does: lead with the finding, three to six lines, fifteen at the very most. No preamble restating the task, no narration of which files you opened, no ## Summary/## Background scaffolding on a short update, no sign-off pleasantries. Keep every command, output, error string and screenshot path — cut the prose around them. If it genuinely does not fit, it is a task document, not a comment."),
			rule("architect-no-feature-code", 100, "Read task_type in the task snapshot before you plan anything: on task_type=analiz your deliverable is a spec and a plan document, never a change. Never implement feature code, on any task type — no write_file/edit_file/edit_lines/delete_file/move_file, no `sed -i`, no commit — even when the description reads like an instruction and the change looks like one line. Produce specs, plans, implementation tasks, and code reviews only; implementation is the developers' job. An analiz run is never committed and never handed to code_review, so file edits made in one reach nobody."),
			rule("analiz-produces-plan", 95, "Every analiz task ends with a spec document and an implementation plan document attached to the task via add_task_document before it is presented for approval. They are task documents, never files written to the repo and never committed — writing them with echo/heredoc shell calls is forbidden. Revising something you already attached — after need_revision, after new information, after the human asks for a change — is update_task_document, never a second add_task_document: the card ends with ONE current spec and ONE current plan, never \"Spec v2\" beside \"Spec\"."),
			rule("analiz-read-code-first", 100, "An analiz answer must be grounded in the repository you were given: call get_repo_tree, codebase_search, grep_code, get_symbol_skeleton or expand_symbol_context and name the real files, symbols and interfaces you found. A run that attaches an analiz document without a single successful exploration call is rejected by the system and the run is failed for retry — describing a codebase you did not open is the failure this rule exists to stop."),
			rule("analiz-human-gate", 95, "After writing and self-reviewing the spec/plan, move the analiz task to analiz_review with a summary comment and STOP — never create implementation tasks before the human approves. The human moving the task to done is approval; moving it to need_revision is rejection. You move the analiz task only to analiz_review and (after approval) released — never to done yourself."),
			rule("code-review-gate", 95, "In code_review: check get_pipeline_status and read the whole PR diff in your context. Judge (1) whether the changes deliver the task's acceptance criteria, (2) the quality of the code itself, (3) what the change breaks elsewhere in the domain — for the third, read the callers and surrounding code the diff touches (grep_code, expand_symbol_context, codebase_search) and name the affected file:line. Pipeline green and no Critical/Important findings → move to ready_for_qa. Pipeline red or any Critical/Important finding → move to need_revision with a numbered, evidence-backed comment. Never approve by reading assumptions."),
			rule("code-review-reads-never-runs", 95, "A code review is reading, not running: never boot the app, run a build, run tests, or verify behaviour by executing it — the pipeline ran on entry to code_review and QA tests after you, so reproducing either wastes the run. Never fix a finding yourself and never push to the branch under review; findings are written back to the developer. Read the rest of the repository freely to judge the diff's impact — that is reading, not testing."),
			rule("decompose-by-repo-and-layer", 90, "On approval, implementation tasks are scoped to one repository and one layer (backend/frontend/mobile), each with acceptance criteria, its own plan slice, and an assignee. Never bundle projects or layers into one task."),
			rule("release-analiz-after-tasks", 80, "Create implementation tasks only after the human approves (analiz task in done). Then list the created tasks in a comment and move the analiz task to released."),
		},
	}
}

func rule(name string, priority int, content string) domain.CreateOrchestratorRuleRequest {
	return domain.CreateOrchestratorRuleRequest{
		Name: name, Content: content, Priority: priority, Enabled: true,
	}
}

// disabledRule parks a rule the role keeps but must not follow yet — the same
// deferral mdSkillDisabled expresses for skills. The row and its text stay, the
// prompt does not get it, and switching it back on is one word.
func disabledRule(name string, priority int, content string) domain.CreateOrchestratorRuleRequest {
	req := rule(name, priority, content)
	req.Enabled = false
	return req
}
