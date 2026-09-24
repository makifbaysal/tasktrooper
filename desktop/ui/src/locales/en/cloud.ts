// cloud namespace: English source of truth.
// Phase 2 — connected cloud provider accounts (Settings → Integrations),
// the environment review item (ReviewList) and the projects hub's per-
// component environment chips. Interpolation uses {name} placeholders.
export const cloud = {
  providers: {
    vercel: "Vercel",
    gcp: "Google Cloud",
    aws: "AWS",
  },
  environments: {
    production: "Production",
    staging: "Staging",
    preview: "Preview",
    development: "Development",
  },
  environmentsShort: {
    production: "Prod",
    staging: "Stg",
    preview: "Preview",
    development: "Dev",
  },
  health: {
    healthy: "Healthy",
    deploying: "Deploying",
    degraded: "Degraded",
    failed: "Failed",
    unknown: "Unknown",
  },
  errorCount: "{count} errors in the last 24 h",
  accounts: {
    title: "Cloud accounts",
    description:
      "The provider accounts TaskTrooper reads deployments, logs and errors through — connected once, used by every repository's environments.",
    loadFailed: "Could not read the connected accounts",
    empty: "No cloud accounts connected yet.",
    connect: "Connect",
    connectProvider: "Connect {provider}",
    status: {
      ok: "Connected",
      error: "Error",
      unverified: "Unverified",
    },
    verifiedAt: "Verified {date}",
    neverVerified: "Never verified",
    verify: "Verify",
    verified: "Account verified",
    verifyFailed: "Verification failed",
    rename: "Rename",
    renameTitle: "Rename this account",
    renameSaved: "Account renamed",
    replaceCredential: "Replace credential",
    remove: "Remove",
    removeTitle: "Remove this account?",
    removeDescription:
      "Environments bound through {label} keep their rows but lose live deployments, logs and errors until another account is connected.",
    removed: "Account removed",
  },
  dialog: {
    connectTitle: "Connect {provider}",
    replaceTitle: "Replace the {provider} credential",
    labelField: "Label",
    labelPlaceholder: "e.g. Production Vercel",
    save: "Save",
    saving: "Saving…",
    connected: "Account connected",
    replaced: "Credential replaced",
    authErrorTitle: "The provider rejected this credential",
    authErrorGeneric: "Could not verify this credential",
    vercel: {
      tokenLabel: "Access token",
      tokenPlaceholder: "paste a Vercel access token",
      teamLabel: "Team ID (optional)",
      teamPlaceholder: "team_…",
      permissions: "Needs a token with read access.",
    },
    gcp: {
      jsonLabel: "Service account JSON",
      jsonPlaceholder: "{ \"type\": \"service_account\", … }",
      pickFile: "Choose a file…",
      fileReadFailed: "Could not read that file",
      permissions: "Needs the Cloud Run Viewer, Logs Viewer and Error Reporting Viewer roles.",
    },
    aws: {
      accessKeyIdLabel: "Access key ID",
      secretAccessKeyLabel: "Secret access key",
      sessionTokenLabel: "Session token (optional)",
      regionLabel: "Region",
      regionPlaceholder: "e.g. us-east-1",
      regionCustom: "Other…",
      permissions: "Needs read-only access to ECS, Lambda, App Runner and CloudWatch Logs.",
    },
  },
  review: {
    whereQuestion: "Where does {component} run in {environment}?",
    candidatesLabel: "Matching resources",
    useThis: "Use this",
    connectPrompt: "Connect a {provider} account to read its deployments, logs and errors",
    connectAction: "Connect",
    notNow: "Not now",
  },
};

export type CloudDict = typeof cloud;
