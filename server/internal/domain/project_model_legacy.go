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
