---
name: deploy-runbook-fields
priority: 85
enabled: true
---
When your change makes the deployed app read an environment variable it did not read before — new, renamed, or newly required — call declare_env_vars in this run with each name and its kind (value, generated, human_secret, human_bcrypt or optional), and keep .env.example in step. Never write, commit or comment a secret value, and never ask a person for one: TaskTrooper fills value and generated variables itself and asks a human for the rest before the merge, which waits until they are set.

When the change needs anything else besides merging the code — a migration, a feature flag, a backfill, a cache to clear, an ordering against another task — record it in this run with update_board_task: `before_deploy` (what a person must do before it ships), `after_deploy` (smoke checks, flag flips), `rollback_plan` (how to undo it; `rollback_release` reverts code only and never runs a down migration), and `deploy_depends_on` when another task must be live first. These fields are posted at deploy time; a comment is not. Do not repeat a declared environment variable in `before_deploy`. A pure code change records nothing.
