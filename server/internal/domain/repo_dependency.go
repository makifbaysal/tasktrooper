package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Dependency target kinds: what the source repository is pointed at.
const (
	DependencyTargetSubRepo  = "sub_repo"
	DependencyTargetRepo     = "repo"
	DependencyTargetDatabase = "database"
)

// ValidDependencyTargetKind reports whether k is a known dependency target.
func ValidDependencyTargetKind(k string) bool {
	switch k {
	case DependencyTargetSubRepo, DependencyTargetRepo, DependencyTargetDatabase:
		return true
	}
	return false
}

// Database engines a manually-recorded database dependency can name.
const (
	DatabaseEnginePostgres = "postgres"
	DatabaseEngineMySQL    = "mysql"
	DatabaseEngineMongoDB  = "mongodb"
	DatabaseEngineRedis    = "redis"
	DatabaseEngineOther    = "other"
)

// ValidDatabaseEngine reports whether e is a known database engine.
func ValidDatabaseEngine(e string) bool {
	switch e {
	case DatabaseEnginePostgres, DatabaseEngineMySQL, DatabaseEngineMongoDB, DatabaseEngineRedis, DatabaseEngineOther:
		return true
	}
	return false
}

// RepoDependency is one edge FROM RepositoryID TO a sub-project, another
// repository, or a manually-recorded database. Mirrors HostingLink's shape —
// one row per binding, TargetKind decides which fields apply.
type RepoDependency struct {
	ID                   uuid.UUID  `json:"id"`
	RepositoryID         uuid.UUID  `json:"repository_id"`
	TargetKind           string     `json:"target_kind"`
	TargetRepositoryID   *uuid.UUID `json:"target_repository_id,omitempty"`
	TargetSubProjectPath string     `json:"target_sub_project_path,omitempty"`
	DatabaseLabel        string     `json:"database_label,omitempty"`
	DatabaseEngine       string     `json:"database_engine,omitempty"`
	DatabaseEnv          string     `json:"database_env,omitempty"`
	DatabaseHost         string     `json:"database_host,omitempty"`
	DatabasePort         int        `json:"database_port,omitempty"`
	DatabaseName         string     `json:"database_name,omitempty"`
	DatabaseUsername     string     `json:"database_username,omitempty"`
	// DatabaseSecret is masked (secrets.MaskedValue()) whenever a secret is
	// stored, "" when none was ever set. The real value never round-trips —
	// same rule as internal/application/mcp/resolve.go's secret fields.
	DatabaseSecret string    `json:"database_secret,omitempty"`
	Note           string    `json:"note,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SaveRepoDependencyRequest creates or updates one dependency row.
type SaveRepoDependencyRequest struct {
	TargetKind           string     `json:"target_kind"`
	TargetRepositoryID   *uuid.UUID `json:"target_repository_id,omitempty"`
	TargetSubProjectPath string     `json:"target_sub_project_path,omitempty"`
	DatabaseLabel        string     `json:"database_label,omitempty"`
	DatabaseEngine       string     `json:"database_engine,omitempty"`
	DatabaseEnv          string     `json:"database_env,omitempty"`
	DatabaseHost         string     `json:"database_host,omitempty"`
	DatabasePort         int        `json:"database_port,omitempty"`
	DatabaseName         string     `json:"database_name,omitempty"`
	DatabaseUsername     string     `json:"database_username,omitempty"`
	// DatabaseSecret: "" or secrets.MaskedValue() leaves the stored secret
	// untouched (only meaningful on an update); anything else re-encrypts.
	DatabaseSecret string `json:"database_secret,omitempty"`
	Note           string `json:"note,omitempty"`
}

// ValidateRepoDependencyRequest checks the field-level rules that do not
// require reading another repository row: target_kind is one of the known
// values, a repo/sub_repo target carries a target_repository_id, a repo
// target is not a self-reference, and a database target's own fields are
// well-formed. The "same project" and "sub-project path exists" checks read
// other repositories and live in the application service instead.
func ValidateRepoDependencyRequest(req SaveRepoDependencyRequest, sourceRepositoryID uuid.UUID) error {
	switch req.TargetKind {
	case DependencyTargetSubRepo:
		if req.TargetRepositoryID == nil {
			return fmt.Errorf("target_repository_id is required for a %s dependency", DependencyTargetSubRepo)
		}
		if req.TargetSubProjectPath == "" {
			return fmt.Errorf("target_sub_project_path is required for a %s dependency", DependencyTargetSubRepo)
		}
	case DependencyTargetRepo:
		if req.TargetRepositoryID == nil {
			return fmt.Errorf("target_repository_id is required for a %s dependency", DependencyTargetRepo)
		}
		if req.TargetSubProjectPath != "" {
			return fmt.Errorf("target_sub_project_path must be empty for a %s dependency", DependencyTargetRepo)
		}
		if *req.TargetRepositoryID == sourceRepositoryID {
			return fmt.Errorf("a repository cannot depend on itself")
		}
	case DependencyTargetDatabase:
		if req.TargetRepositoryID != nil {
			return fmt.Errorf("target_repository_id must be empty for a %s dependency", DependencyTargetDatabase)
		}
		if req.TargetSubProjectPath != "" {
			return fmt.Errorf("target_sub_project_path must be empty for a %s dependency", DependencyTargetDatabase)
		}
		if req.DatabaseLabel == "" {
			return fmt.Errorf("database_label is required for a %s dependency", DependencyTargetDatabase)
		}
		if req.DatabaseEnv != "" && req.DatabaseEnv != DeployEnvStage && req.DatabaseEnv != DeployEnvProd {
			return fmt.Errorf("database_env must be %q or %q", DeployEnvStage, DeployEnvProd)
		}
		if req.DatabaseEngine != "" && !ValidDatabaseEngine(req.DatabaseEngine) {
			return fmt.Errorf("invalid database engine: %s", req.DatabaseEngine)
		}
		if req.DatabasePort < 0 || req.DatabasePort > 65535 {
			return fmt.Errorf("database_port must be between 0 and 65535, got %d", req.DatabasePort)
		}
	default:
		return fmt.Errorf("invalid target_kind: %s", req.TargetKind)
	}
	return nil
}
