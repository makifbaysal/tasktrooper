---
key: repository.workflow_setup_task_brief
version: 1
inputs: [Kind]
---
Create and push GitHub Actions CI/CD workflows for this repository (kind: {{.Kind}}).

Required jobs (under .github/workflows):
- validate: lint / static analysis / type checking
- build: compilation ({{if eq .Kind "frontend"}}e.g. npm/pnpm build{{else if eq .Kind "mobile"}}e.g. xcodebuild/fastlane — do not use docker{{else if eq .Kind "data"}}e.g. uv sync, ruff and the package build; dbt build for a dbt project{{else if eq .Kind "game"}}e.g. the engine's headless build (Unity -batchmode, godot --headless --export-release, Unreal RunUAT BuildCookRun) — hosted runners carry no engine or licence, so name the runner/image the job needs{{else if eq .Kind "monorepo"}}a separate build per subproject (backend/frontend/mobile/worker/data/game){{else}}e.g. go build / docker build{{end}})
- test: automated tests
- stage_deploy: a workflow_dispatch-triggerable workflow that deploys to staging
- preprod_deploy: (optional) a workflow_dispatch-triggerable workflow that deploys to a pre-production environment
- prod_deploy: a workflow_dispatch-triggerable workflow that deploys to production

Once each workflow exists, save the job/workflow mapping under Repository Settings > Pipeline so the QA gate and release steps run through GitHub Actions.
