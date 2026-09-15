package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestValidateRepoDependencyRequestAcceptsAValidRepoTarget(t *testing.T) {
	source := uuid.New()
	target := uuid.New()
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &target,
	}, source)
	require.NoError(t, err)
}

func TestValidateRepoDependencyRequestAcceptsAValidSubRepoTarget(t *testing.T) {
	source := uuid.New()
	target := uuid.New()
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:           domain.DependencyTargetSubRepo,
		TargetRepositoryID:   &target,
		TargetSubProjectPath: "apps/api",
	}, source)
	require.NoError(t, err)
}

func TestValidateRepoDependencyRequestAcceptsAValidDatabaseTarget(t *testing.T) {
	source := uuid.New()
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:     domain.DependencyTargetDatabase,
		DatabaseLabel:  "Prod Postgres",
		DatabaseEngine: domain.DatabaseEnginePostgres,
		DatabaseEnv:    domain.DeployEnvProd,
		DatabasePort:   5432,
	}, source)
	require.NoError(t, err)
}

func TestValidateRepoDependencyRequestRejectsAnUnknownTargetKind(t *testing.T) {
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{TargetKind: "bogus"}, uuid.New())
	require.ErrorContains(t, err, "invalid target_kind")
}

func TestValidateRepoDependencyRequestRejectsARepoSelfReference(t *testing.T) {
	source := uuid.New()
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetRepo,
		TargetRepositoryID: &source,
	}, source)
	require.ErrorContains(t, err, "cannot depend on itself")
}

func TestValidateRepoDependencyRequestRejectsARepoTargetWithoutID(t *testing.T) {
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind: domain.DependencyTargetRepo,
	}, uuid.New())
	require.ErrorContains(t, err, "target_repository_id is required")
}

func TestValidateRepoDependencyRequestRejectsASubRepoTargetWithoutPath(t *testing.T) {
	target := uuid.New()
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:         domain.DependencyTargetSubRepo,
		TargetRepositoryID: &target,
	}, uuid.New())
	require.ErrorContains(t, err, "target_sub_project_path is required")
}

func TestValidateRepoDependencyRequestRejectsADatabaseTargetWithoutLabel(t *testing.T) {
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind: domain.DependencyTargetDatabase,
	}, uuid.New())
	require.ErrorContains(t, err, "database_label is required")
}

func TestValidateRepoDependencyRequestRejectsAnInvalidDatabaseEnv(t *testing.T) {
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:    domain.DependencyTargetDatabase,
		DatabaseLabel: "Cache",
		DatabaseEnv:   domain.DeployEnvLocal,
	}, uuid.New())
	require.ErrorContains(t, err, "database_env must be")
}

func TestValidateRepoDependencyRequestRejectsAPortOutOfRange(t *testing.T) {
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:    domain.DependencyTargetDatabase,
		DatabaseLabel: "Cache",
		DatabasePort:  70000,
	}, uuid.New())
	require.ErrorContains(t, err, "database_port must be between")

	err = domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:    domain.DependencyTargetDatabase,
		DatabaseLabel: "Cache",
		DatabasePort:  -1,
	}, uuid.New())
	require.ErrorContains(t, err, "database_port must be between")
}

func TestValidateRepoDependencyRequestRejectsAnInvalidDatabaseEngine(t *testing.T) {
	err := domain.ValidateRepoDependencyRequest(domain.SaveRepoDependencyRequest{
		TargetKind:     domain.DependencyTargetDatabase,
		DatabaseLabel:  "Cache",
		DatabaseEngine: "oracle",
	}, uuid.New())
	require.ErrorContains(t, err, "invalid database engine")
}
