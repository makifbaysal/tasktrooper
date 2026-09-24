package domain

import "github.com/google/uuid"

// Legacy repository_dependencies.target_kind values (migration 134); the
// table stays for LegacyModelSource to read even though the feature that
// wrote it is gone.
const (
	DependencyTargetSubRepo  = "sub_repo"
	DependencyTargetRepo     = "repo"
	DependencyTargetDatabase = "database"
)

// LegacyDependency is one repository_dependencies row (migration 134), read
// only to carry it into component_links.
type LegacyDependency struct {
	ID                   uuid.UUID
	RepositoryID         uuid.UUID
	TargetKind           string
	TargetRepositoryID   *uuid.UUID
	TargetSubProjectPath string
	DatabaseLabel        string
	DatabaseEngine       string
	DatabaseEnv          string
	DatabaseHost         string
	DatabasePort         int
	DatabaseName         string
	Note                 string
}

// Legacy repository_profile_sections.section values (migration 086) that
// LegacyAgentSection.Section can carry — only the agent-writable ones, since
// LegacyModelSource filters to origin = 'agent' at the query. The backfill
// maps each onto the project_notes topic it becomes; "notes" is the old
// free-text catch-all and folds into gotchas.
const (
	ProfileSectionPurpose       = "purpose"
	ProfileSectionEntrypoints   = "entrypoints"
	ProfileSectionConventions   = "conventions"
	ProfileSectionInvariants    = "invariants"
	ProfileSectionChangeRecipes = "change_recipes"
	ProfileSectionDangerZones   = "danger_zones"
	ProfileSectionGotchas       = "gotchas"
	ProfileSectionNotes         = "notes"
)

// LegacyAgentSection is one agent-written repository_profile_sections row,
// read only to carry it into project_notes.
type LegacyAgentSection struct {
	Section        string
	BodyMD         string
	Evidence       []SourceEvidence
	SourceCommit   string
	SubProjectPath string
}
