// projectsHub namespace: English source of truth.
// Covers the projects list (pages/ProjectsPage) and the single project page
// (pages/ProjectPage). Reuses projectModel.* for role/type/shape vocabulary.
export const projectsHub = {
  title: "Projects",
  subtitle: "{projects} projects · {repositories} repositories · {components} components",
  actions: {
    addRepository: "Add repository",
    newProject: "New project",
  },
  view: {
    cards: "Cards",
    map: "Map",
  },
  map: {
    independent: "Independent",
    canvasLabel: "All projects map",
  },
  attention: {
    message: "{count} things need your review",
    review: "Review",
  },
  filters: {
    allRoles: "All",
    searchPlaceholder: "Search projects, repositories, stack…",
  },
  card: {
    counts: "{repos} repos · {components} components",
    reviewBadge: "{count} to review",
    editProject: "Edit project",
    deleteProject: "Delete project",
    deleteTitle: "Delete {name}?",
    deleteDescription: "Repositories in this project are not deleted — they only lose the link to it.",
    noDescription: "No description",
    emptyRepos: "No repositories yet",
    linksTo: "Links to",
    sharesPrefix: "Shares",
    sharesWith: "with",
    scanRunning: "Scanning…",
    scanFailed: "Scan failed",
    scanNone: "Never scanned",
    gitWarning: "Git issue — {warning}",
  },
  unassigned: {
    title: "Repositories without a project",
    description: "Not linked to any project yet.",
    addToProject: "Add to project",
  },
  empty: {
    title: "Create your first project",
    description: "Group the repositories that make up one product or initiative.",
    cta: "Create your first project",
  },
  deleted: "Project deleted",
  deleteFailed: "Failed to delete the project",
  project: {
    breadcrumb: "Projects",
    edit: "Edit",
    notFound: {
      title: "Project not found",
      description: "It may have been deleted, or the link is wrong.",
      back: "Back to projects",
    },
    tabs: {
      architecture: "Architecture",
      repositories: "Repositories",
      review: "Review",
      settings: "Settings",
    },
    table: {
      repository: "Repository",
      shape: "Shape",
      components: "Components",
      requiredChecks: "Required checks",
      review: "Review",
      lastScan: "Last scan",
      open: "Open",
    },
    reviewEmpty: "Nothing to review",
    settings: {
      nameLabel: "Name",
      descriptionLabel: "Description",
      saved: "Project updated",
      members: "Repositories",
      noMembers: "No repositories in this project yet.",
      removeFromProject: "Remove from project",
      removed: "Removed from the project",
      removeFailed: "Failed to remove the repository",
      addExisting: "Add existing repository",
      addExistingPlaceholder: "Choose a repository…",
      added: "Added to the project",
      addFailed: "Failed to add the repository",
      dangerZone: "Danger zone",
      deleteProject: "Delete project",
      deleteWarning: "Repositories are not deleted — only removed from this project.",
      deleteConfirmTitle: "Delete {name}?",
      deleteConfirmDescription: "This cannot be undone. Its repositories stay; only this project is removed.",
      delete: "Delete project",
    },
  },
};

export type ProjectsHubDict = typeof projectsHub;
