// English dictionary for the settings sub-pages (LLM / Usage / Board).
// Source of truth; tr/settingsPages.ts must mirror these keys exactly.
// Interpolation uses {name} placeholders.
export const settingsPages = {
  integrations: {
    title: "Integrations",
    description: "The outside accounts TaskTrooper signs in to on your behalf. Saved once, used by every project.",
    loadFailed: "Could not read the store credentials",

    // Rendered on a repository's deploy page, where the credential vault used
    // to live, so the #store-credentials link into it still lands somewhere real.
    movedTitle: "App store credentials",
    movedBody: "They are saved once for the whole workspace and now live under Settings → Integrations.",
    movedLink: "Open Integrations",

    asc: {
      title: "App Store Connect",
      description: "An API key from App Store Connect → Users and Access → Integrations. It signs and uploads every iOS release.",
    },
    play: {
      title: "Google Play Console",
      description: "A service account key from the Google Cloud project linked to your Play Console. It uploads and promotes every Android release.",
    },

    stores: {
      connectFirst: "Save a credential first to see the apps it can reach.",
      listApps: "List apps",
      appsTitle: "Apps in this account",
      retry: "Try listing again",
      loading: "Reading the store console…",
      loadFailed: "Could not list the apps",
      notConnectedTitle: "This store is not connected yet",
      notConnectedBody:
        "Connect the store account on the Integrations page before listing its apps.",
      notConnectedAction: "Go to Integrations",
      emptyTitle: "No apps yet",
      emptyBody: "This credential works, but the account holds no apps to pick from.",
      unavailableTitle: "This account cannot be listed",
      unavailableBody:
        "The Play Developer API has no endpoint that lists apps. That list comes only from the separate Reporting API, which this service account may not be allowed to reach. Everything else still works; the app is named by hand instead.",
    },

    picker: {
      title: "Pick the store app",
      description: "Bind this repository to one app in the connected store account.",
      appsLabel: "Apps in this account",
      manualLabel: "App identifier",
      manualPlaceholder: "com.example.app",
      manualHint: "The bundle ID or package name exactly as the store console shows it.",
      link: "Link app",
      linking: "Linking…",
      linked: "App linked",
      linkFailed: "Could not link the app",
    },

  },
  llm: {
    loadFailed: "Failed to load LLM providers",
    updateFailed: "Update failed",

    // OpenAI-compatible endpoints section
    endpointsTitle: "OpenAI-compatible Endpoints",
    endpointsDesc:
      "Your own IP, Ollama, LM Studio, vLLM, OpenRouter, Groq… Add as many named endpoints as you like.",
    addEndpoint: "Add endpoint",
    endpointsEmpty: "No endpoints yet. Start with “Add endpoint”.",

    // Card badges & field labels (shared by endpoint and native cards)
    badgeDefault: "Default",
    badgeConnected: "Connected",
    badgeNotConnected: "Not connected",
    urlLabel: "URL:",
    modelLabel: "Model:",
    apiKeyLabel: "API key:",
    apiKeyStored: "stored",
    timeoutLabel: "Timeout:",
    secondsValue: "{seconds} s",
    edit: "Edit",
    makeDefault: "Make default",
    delete: "Delete",

    // Native providers section
    nativeTitle: "Native Providers (Gemini · Anthropic)",
    reconnect: "Reconnect",
    connect: "Connect",
    disconnectShort: "Disconnect",

    // Local agent CLI section (host-executed providers)
    cliTitle: "Local Agent CLIs",
    cliDesc:
      "These providers hand the task to a CLI session on your runner machine instead of to a server. They ask for no API key and no address. “Connect” verifies the binary is installed and signed in, then writes every enabled agent's role, rules and skills to disk in the layout that CLI reads.",
    badgeComingSoon: "Coming soon",
    cliComingSoonHint:
      "The executor that would run this CLI has not been built yet, so it cannot be selected on an agent.",
    cliConnecting: "Installing…",
    cliConnectedToast: "CLI connected and the agent catalog installed",
    cliDisconnectedToast: "CLI disconnected",
    cliBinaryLabel: "Binary:",
    cliInstalledLabel: "Installed:",
    cliInstalledValue: "{agents} agents · {skills} skills",
    cliCatalogLabel: "Catalog:",
    cliCatalogHint:
      "A snapshot for you to inspect. Each run writes its own agent's rules and skills into its task workspace from the database, so nothing here goes stale on a board run.",
    cliSwapHint: "Only one local CLI can be connected at a time. Connecting this one disconnects the other.",

    // The Claude Code card absorbed the old Settings → Local Runner page.
    // Everything here is about the machine behind the CLI: which one is
    // answering, what one press of Connect is doing on it right now, and where
    // to go when the answer is "no machine at all".
    claudeCode: {
      stepInstalling: "Verifying claude and installing the agent catalog…",
      stepDisconnectingCli: "Disconnecting the CLI…",
      // The environment preflight checklist (EnvironmentPreflight), reported
      // over IPC by the desktop shell: the bundled server, Postgres, git, the
      // Claude binary and its account.
      preflight: {
        title: "Environment",
        refresh: "Refresh",
        loadFailed: "Could not load the environment checklist",
        empty: "Nothing to check yet",
        blocking: "Connect is disabled until \"{item}\" is fixed",
        copyCommand: "Copy command",
        copied: "Copied",
      },
    },

    // Toasts (native + endpoint)
    testSuccess: "Connection successful",
    testFailed: "Connection test failed",
    connectedToast: "{provider} connected",
    connectFailed: "Could not connect",
    defaultUpdated: "Default provider updated",
    activateFailed: "Activation failed",
    disconnectedToast: "Connection removed",
    disconnectFailed: "Could not disconnect",
    nameUrlRequired: "Name and Base URL are required",
    endpointUpdated: "Endpoint updated",
    endpointAdded: "Endpoint added",
    saveFailed: "Could not save",
    endpointDeleted: "Endpoint deleted",
    deleteFailed: "Could not delete",

    // Delete endpoint confirm dialog
    deleteEndpointTitle: "Delete endpoint",
    deleteEndpointDesc:
      "The “{name}” endpoint will be deleted. Agents using this endpoint fall back to the session default. Continue?",

    // Native connect dialog
    connectDialogTitle: "{provider} connection",
    baseUrlLabel: "Base URL",
    apiKeyFieldLabel: "API key",
    apiKeyChangePlaceholder: "Enter a new key to change it",
    apiKeyBlankHint: "If left blank, the stored key is used.",
    timeoutFieldLabel: "Request timeout (seconds)",
    timeoutHint:
      "300–600 s is recommended for local models. Large requests like planner and intake can take longer.",
    test: "Test",

    // Endpoint add/edit dialog
    endpointDialogEditTitle: "Edit endpoint",
    endpointDialogAddTitle: "Add endpoint",
    presetLabel: "Preset",
    nameLabel: "Name",
    namePlaceholder: "e.g. Home server / Ollama",
    defaultModelLabel: "Default model (optional)",
    defaultModelPlaceholder: "Can also be chosen per agent",
    apiKeyOptionalLabel: "API key (optional)",
    add: "Add",

    // Endpoint presets
    presetLmStudio: "LM Studio",
    presetOllama: "Ollama",
    presetOpenAI: "OpenAI",
    presetGroq: "Groq",
    presetOpenRouter: "OpenRouter",
    presetGeminiOpenAI: "Gemini (OpenAI-compatible)",
    presetCustom: "Custom / own IP",
  },
  usage: {

    inputColumn: "Input",
    outputColumn: "Output",
    modelColumn: "Model",
    callsColumn: "Calls",

    // Usage summary page
    usageLoadFailed: "Failed to load usage data",
    lastNDays: "Last {days} days",
    llmCalls: "LLM Calls",
    inputTokens: "Input Tokens",
    outputTokens: "Output Tokens",
    byModel: "By Model",
    noUsage: "No recorded usage in this range.",
    daily: "Daily",
  },
  board: {
    loadFailed: "Failed to load",
    invalidName: "Invalid name",
    duplicateName: "A column with this name already exists",
    savedToast: "Board workflow saved",
    saveFailed: "Failed to save",

    title: "Board Workflow",
    subtitle:
      "Workflow columns and the transition rules between them. Backlog is fixed.",
    addColumn: "Add Column",

    columnsTitle: "Columns",
    columnsSubtitle:
      "Workflow columns. The technical value (slug) is generated automatically from the name.",
    backlogLabel: "Backlog",
    fixedTag: "fixed",
    deleteColumn: "Delete column",
    noColumns: "No workflow columns yet.",

    transitionsTitle: "Transition rules",
    transitionsSubtitle:
      "For each column, choose the target columns it can move to. If none are selected, that column can move anywhere.",
    freeToAnywhere: "anywhere allowed",

    newColumnTitle: "New Column",
    columnNameLabel: "Column name",
    columnNamePlaceholder: "e.g. Code Review",
    slugPrefix: "slug:",
    add: "Add",
  },

  roles: {
    title: "Roles",
    subtitle:
      "Named jobs (developer, analyst, architect, QA, product manager, …) that agents are assigned to, and which system duties currently answer to which role.",
    loadFailed: "Failed to load",
    saveFailed: "Failed to save",
    savedToast: "Role saved",
    createdToast: "Role created",
    deletedToast: "Role deleted",
    deleteFailed: "Failed to delete",
    assignmentsSavedToast: "Assignments saved",
    purposeSavedToast: "Duty saved",

    newRole: "New role",
    empty: "No roles yet.",
    agentCount: "{count} agents",
    selectRole: "Select a role to edit it.",

    detailsTitle: "Details",
    nameLabel: "Name",
    descriptionLabel: "Description",
    toolsLabel: "Required tools",
    toolsPlaceholder: "one tool per line",
    toolsHelp:
      "An agent assigned to this role must have every one of these tools in its policy — assigning one that doesn't asks to grant them first.",
    keyLabel: "Key",
    keyPlaceholder: "e.g. developer",
    keyHelp: "Lowercase, letters/digits/underscore. Cannot be changed after creation.",
    create: "Create",
    deleteRole: "Delete role",
    deleteRoleDescription: "“{name}” will be permanently deleted. Agents holding it lose the role.",
    removeAssignment: "Remove",

    assignmentsTitle: "Agent assignments",
    assignmentsSubtitle:
      "Which agents may be picked for this role, in which area(s), and in what priority order when more than one qualifies.",
    saveAssignments: "Save assignments",
    noAssignments: "No agents assigned yet.",
    addAgent: "Add an agent…",
    priority: "Priority",
    areaAny: "Any area",
    area: {
      backend: "Backend",
      frontend: "Frontend",
      mobile: "Mobile",
    },
    areaCustomPlaceholder: "Add area…",
    addArea: "Add",
    removeArea: "Remove area",

    dutiesTitle: "System duties",
    dutiesSubtitle:
      "Hook names, not roles: which role currently answers a system-created task or a repository profiling run.",
    dutyNone: "— none —",
    duty: {
      system_task_assignee: "System-created tasks",
      repo_profiler: "Repository profiling",
    },

    missingToolsTitle: "Missing tools",
    missingToolsBody:
      "One or more agents are missing tools this role requires. Add them and save the assignment?",
    grantTools: "Yes, add them",
  },

  workflows: {
    title: "Workflows",
    subtitle: "Task types and the ordered, per-column workflow stages each one moves through.",
    loadFailed: "Failed to load",
    saveFailed: "Failed to save",
    savedToast: "Task type saved",
    createdToast: "Task type created",
    deletedToast: "Task type deleted",
    deleteFailed: "Failed to delete",
    stagesSavedToast: "Stages saved",
    stagesInvalid: "Some stages need attention before this can be saved",

    newType: "New task type",
    empty: "No task types yet.",
    defaultTag: "default",
    selectType: "Select a task type to edit it.",

    detailsTitle: "Details",
    labelLabel: "Label",
    prefixLabel: "Key prefix",
    prefixLocked: "The prefix is locked once this type has tasks.",
    prefixPlaceholder: "e.g. T",
    isDefaultLabel: "Default type for new tasks",
    isDefectLabel: "Counted as a defect (bugs_assigned KPI)",
    deleteType: "Delete task type",
    deleteTypeDescription: "“{label}” will be permanently deleted. This only works while it has no tasks.",

    assigneeModeLabel: "Assignee mode",
    assigneeMode: {
      none: "None",
      default: "Default",
      override: "Override",
    },
    assigneeModeHelp: {
      none: "New tasks of this type keep whatever assignee was requested, or none.",
      default: "The role's agent fills the assignee only when none was requested.",
      override: "The role's agent always fills the assignee, replacing anything requested.",
    },
    assigneeRoleLabel: "Assignee role",

    typeBehavioursLabel: "Type-wide behaviours",

    keyLabel: "Key",
    cloneFromLabel: "Clone from",
    cloneFromNone: "— blank —",
    create: "Create",

    stagesTitle: "Stages",
    stagesSubtitle: "One row per board column this type visits, in order. Move rows with the arrows.",
    saveStages: "Save stages",
    addStage: "Add a stage for…",
    removeStage: "Remove stage",
    moveUp: "Move up",
    moveDown: "Move down",
    orphanedStage: "Column no longer exists",
    offPath: "Off path",
    kindLabel: "Kind",
    onPathLabel: "On the happy path",
    behavioursLabel: "Behaviours",
    behaviourGroup: {
      entry: "On entering this column",
      exit: "Required to move on",
      other: "Other",
    },
    behaviour: {
      "dispatch_suspended": { label: "Dispatch suspended", description: "The dispatcher never starts a run for a task sitting in this stage." },
      "route_to_subscribers": { label: "Route to subscribers", description: "A task entering this stage wakes every agent subscribed to the column instead of only its assignee." },
      "merge_pr_on_enter": { label: "Merge PR on enter", description: "Entering this stage wakes the merge flow for the task's pull request." },
      "watch_deploy_on_resume": { label: "Watch deploy on resume", description: "A task resuming into this stage is watched for its production deploy." },
      "block_on_dependencies": { label: "Block on dependencies", description: "The task parks here while an unfinished blocker (`blocks`) exists." },
      "wait_for_ci": { label: "Wait for CI", description: "Dispatch into this stage waits for the pipeline gate to open." },
      "ensure_pr_on_enter": { label: "Ensure PR on enter", description: "Entering this stage opens the task's pull request if it does not exist yet." },
      "detect_migration_on_enter": { label: "Detect migration on enter", description: "Entering this stage checks the branch diff for a schema migration." },
      "stage_deploy_on_enter": { label: "Stage deploy on enter", description: "Entering this stage triggers a stage deploy, per the repository's test strategy." },
      "auto_enter": { label: "Auto-enter", description: "The dispatcher moves the task straight into the named column on assignment/wake." },
      "advance_on_diff": { label: "Advance on diff", description: "A run that ends with a green build and a real diff is moved to the named column automatically." },
      "advance_on_document": { label: "Advance on document", description: "A run that ends with a document attached is moved to the named column automatically." },
      "build_verify": { label: "Build verify", description: "A run in this stage runs the build-gate fix round before finishing." },
      "commit_on_finish": { label: "Commit on finish", description: "A run in this stage commits its diff on the way out." },
      "require_pr_for_review": { label: "Require PR for review", description: "A run here refuses without an open pull request to review." },
      "require_criteria_complete": { label: "Require criteria complete", description: "Entering this stage is refused while an acceptance criterion is still open." },
      "criterion_verdict": { label: "Criterion verdict", description: "A verdict recorded in this stage is attributed to the named review channel." },
      "forward_exit": { label: "Forward exit", description: "A move out of this stage counts as a forward review exit for scoring." },
      "require_test_cases": { label: "Require test cases", description: "This stage requires the task's generated test cases to be scored." },
      "review_verdict_sweep": { label: "Review verdict sweep", description: "A finished review round in this stage is swept to a verdict and passed on." },
      "require_execution_evidence": { label: "Require execution evidence", description: "A QA round in this stage is rejected as ungrounded without evidence the product was actually run." },
      "require_product_check": { label: "Require product check", description: "A UAT round in this stage is rejected without evidence the product was checked." },
      "review_chain_stage": { label: "Review chain stage", description: "This stage is a mandatory step of the type's review chain, required before done/released." },
      "require_release_deploy": { label: "Require release deploy", description: "Entering this stage is refused without a recorded successful production deploy." },
      "strip_writers": { label: "Strip writers", description: "Write/verdict tools are stripped from the policy in this stage, except any named in allow." },
      "no_code_reading": { label: "No code reading", description: "Code-reading tools are stripped from the policy in this stage." },
      "no_workspace_writes": { label: "No workspace writes", description: "Runs on this task type never get workspace write tools." },
      "require_repo_grounding": { label: "Require repo grounding", description: "A run on this task type is rejected as ungrounded without evidence the repository was actually read." },
    },
    noBehaviours: "No behaviours available at this scope.",
    instructionsLabel: "Instructions",
    participantsLabel: "Participants",
    participantMode: {
      worker: "Worker",
      approver: "Approver",
    },
    addParticipant: {
      worker: "Add worker",
      approver: "Add approver",
    },
    removeParticipant: "Remove",
    participantInstructionsPlaceholder: "Extra instructions for this participant at this stage (optional)",
    noSubscriberWarning: "No agent holding “{role}” is subscribed to this column — it will never get this work.",
kind: {
      intake: "Intake",
      queue: "Queue",
      work: "Work",
      review: "Review",
      approval: "Approval",
      rework: "Rework",
      parked: "Parked",
      terminal: "Terminal",
    },
  },
  catalog: {
    title: "External Agent Catalog",
    loadFailed: "Failed to load catalog status",
    syncFailed: "Catalog sync failed",
    syncToast: "Catalog synced",
    syncNow: "Sync now",
    syncing: "Syncing…",
    notConfigured: "No external agent catalog is configured",
    notConfiguredHelp:
      "Point AGENT_CATALOG_REPO at a git repository (or a local directory path) in the server's environment and restart to use this page.",
    lastSync: "Last sync",
    repoRef: "Source",
    created: "Created",
    updated: "Updated",
    merged: "Merged",
    skipped: "Skipped",
    pendingCount: "Waiting",
    pendingTitle: "Changes waiting for you",
    pendingEmpty: "Nothing pending — every catalog change was applied.",
    error: "Last error",
    dismiss: "Dismiss",
  },

};

export type SettingsPagesDict = typeof settingsPages;
