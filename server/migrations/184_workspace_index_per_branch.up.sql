-- Migration 060 meant one workspace index per (repository, branch), but the
-- one-per-repository unique index it dropped had been renamed by 022, so the
-- DROP missed it. A branch index next to its repository's default-branch index
-- has failed with a unique violation since: task branches were never indexed,
-- and the branch rows the reaper prunes were never written.
-- idx_workspace_indexes_repo_branch stays the uniqueness rule.
DROP INDEX IF EXISTS idx_workspace_indexes_repository;
