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
        "The Play Developer API has no endpoint that lists apps — that list comes only from the separate Reporting API, which this service account may not be allowed to reach. Everything else still works; the app is named by hand instead.",
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

    gcloud: {
      title: "Google Cloud",
      description:
        "A read-only service account for the Google Cloud project your services run in. It is what lets TaskTrooper read Cloud Run services and GKE clusters; nothing is ever deployed with it.",
      statusUnavailable: "Connection status unavailable.",
      configured: "Connected",
      notConfigured: "Not connected",
      updated: "Saved {date}",
      accountLabel: "Service account",
      projectLabel: "Project",
      // Why two fields are readable here while the key is not: they are stored
      // in the clear on purpose, so this card can say which connection is saved.
      identityHint:
        "These two are the only parts kept in the clear, so the connection can be named here. The key itself is stored encrypted and is never shown again.",
      jsonLabel: "Service account JSON",
      jsonPlaceholder: "{ \"type\": \"service_account\", … }",
      replaceHint:
        "The saved key is never read back: every save replaces it in full, and a key Google Cloud refuses is not kept.",
      save: "Save key",
      saved: "Service account saved",
      delete: "Remove",
      deleteTitle: "Remove this service account?",
      deleteDescription:
        "Cloud Run services and GKE clusters stop being readable until another key is saved. Nothing inside Google Cloud is changed.",
      deleted: "Service account removed",
      loadFailed: "Could not read the Google Cloud connection",
    },

    gcloudResources: {
      connectFirst: "Save a service account first to see what it can reach.",
      list: "List resources",
      title: "Resources in this project",
      retry: "Try listing again",
      loading: "Reading Google Cloud…",
      loadFailed: "Could not list the resources",
      notConnectedTitle: "Google Cloud is not connected yet",
      notConnectedBody:
        "Save a service account under Settings → Integrations before picking a Cloud Run service or a GKE cluster.",
      notConnectedAction: "Go to Integrations",
      unavailableTitle: "This service account cannot list resources",
      unavailableBody:
        "The key works, but it is not allowed to enumerate this project. Everything else still works; the resource name is typed by hand instead.",
      emptyTitle: "Nothing to pick yet",
      emptyBody:
        "The service account can read this project, but it holds no Cloud Run services and no GKE clusters.",
      cloudRunTitle: "Cloud Run services",
      cloudRunBadge: "Cloud Run",
      gkeTitle: "GKE clusters",
      gkeBadge: "GKE",
      familyEmpty: "None in this project.",
      familyUnavailableTitle: "Not listed with this key",
      cloudRunUnavailableBody:
        "This service account is not allowed to list Cloud Run services. Granting it roles/run.viewer on the project is enough — whatever is shown for GKE is unaffected.",
      gkeUnavailableBody:
        "This service account is not allowed to list GKE clusters. Granting it roles/container.viewer on the project is enough — the Cloud Run services above are unaffected.",
      unreachableLocations: "Some locations did not answer: {locations}",
      workloadsTitle: "Workloads are not listed over this connection",
      workloadsBody:
        "Cluster names, versions and node pools come from the Google Cloud API and are shown above. What runs inside a cluster does not: reading that means talking to the cluster's own control plane, and a private control plane only answers from inside your VPC — which the shared TaskTrooper server is not. This is a boundary of the connection, not a fault in it.",
    },

    gcloudPicker: {
      title: "Pick the Google Cloud resource",
      description: "Bind this repository to one Cloud Run service or GKE cluster in the connected project.",
      typeLabel: "Resource type",
      typeCloudRun: "Cloud Run service",
      typeGke: "GKE cluster",
      manualLabel: "Resource name",
      manualPlaceholder: "projects/my-project/locations/europe-west1/services/api",
      manualHint: "The full resource name exactly as Google Cloud shows it, including the project and the location.",
      bind: "Bind resource",
      binding: "Binding…",
      bound: "Resource bound",
      bindFailed: "Could not bind the resource",
    },

    vercelPicker: {
      title: "Pick the Vercel project",
      description: "Bind this repository to one project in the connected Vercel account.",
      projectsLabel: "Projects in this account",
      loading: "Reading Vercel…",
      loadFailed: "Could not list the projects",
      retry: "Try listing again",
      notConnectedTitle: "Vercel is not connected yet",
      notConnectedBody: "Connect the Vercel account under Settings → Integrations before picking a project.",
      notConnectedAction: "Go to Integrations",
      unavailableTitle: "This connection cannot list projects",
      unavailableBody:
        "The token works, but it is not allowed to enumerate projects in this scope. The project ID is pasted by hand instead.",
      emptyTitle: "No projects yet",
      emptyBody: "The connection works, but this Vercel scope holds no projects to pick from.",
      manualLabel: "Project ID",
      manualPlaceholder: "prj_…",
      manualHint: "Vercel dashboard → the project → Settings → General → Project ID.",
      link: "Link project",
      linking: "Linking…",
      linked: "Project linked",
      linkFailed: "Could not link the project",
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
    badgeLocalCli: "Local CLI",
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
    cliSwapHint: "Only one local CLI can be connected at a time — connecting this one disconnects the other.",

    // The Claude Code card absorbed the old Settings → Local Runner page.
    // Everything here is about the machine behind the CLI: which one is
    // answering, what one press of Connect is doing on it right now, and where
    // to go when the answer is "no machine at all".
    claudeCode: {
      stepInstalling: "Verifying claude and installing the agent catalog…",
      stepDisconnectingCli: "Disconnecting the CLI…",
      // The environment preflight checklist (EnvironmentPreflight), reported
      // over IPC by the desktop shell — the bundled server, Postgres, git, the
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

  analizAssignment: {
    title: "Analysis Task Assignment",
    subtitle:
      "Which agent an analysis (analiz) task for the backend, frontend or mobile area is auto-assigned to on creation.",
    loadFailed: "Failed to load",
    savedToast: "Assignment saved",
    saveFailed: "Failed to save",
    discardedToast: "Assignment not saved",

    areas: {
      backend: "Backend",
      frontend: "Frontend",
      mobile: "Mobile",
    },

    missingToolsTitle: "Missing tools",
    missingToolsBody:
      "The selected agent is missing tools the analysis workflow needs. Add them and save the assignment?",
    grantTools: "Yes, add them",
  },

};

export type SettingsPagesDict = typeof settingsPages;
