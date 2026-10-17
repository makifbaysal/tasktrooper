---
key: tool.record_local_deploy
version: "2"
params:
    command: What you ran, e.g. 'bash scripts/release-local.sh web'
    conclusion: success | failure | cancelled. Required when status is completed; a failed script is reported as failure, not left unreported.
    env: stage | preprod | prod — the environment the script actually deployed to
    head_ref: Branch or ref that was built (usually main)
    head_sha: Commit that was built — `git rev-parse origin/main`, since the scripts build origin/main, not the working tree.
    repository_id: Repository UUID from your task snapshot. Omit to use the repository this run is working in.
    run_id: The run_id the in_progress call returned. Required by the completed call, omitted by the first one.
    status: in_progress before the script runs, completed after it
---
Record a deploy you ran from this machine (the repo's scripts/release-local.sh or scripts/deploy-local.sh) so it appears in Operations → Changes next to the GitHub Actions ones. Call it TWICE per deploy: once before the script with status=in_progress, then once after it with status=completed and the conclusion, passing back the run_id the first call returned. This records only — it never deploys anything.
