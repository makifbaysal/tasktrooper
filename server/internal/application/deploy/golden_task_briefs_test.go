package deploy_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/deploy"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// fixedTargetStore returns one fixed target from Get, for CreateSetupTask,
// which requires a target to already be configured.
type fixedTargetStore struct {
	target domain.DeployTarget
}

func (f *fixedTargetStore) ListByRepository(context.Context, uuid.UUID) ([]domain.DeployTarget, error) {
	return nil, nil
}
func (f *fixedTargetStore) ListAll(context.Context) ([]domain.DeployTarget, error) { return nil, nil }
func (f *fixedTargetStore) Get(context.Context, uuid.UUID, string, string) (domain.DeployTarget, error) {
	return f.target, nil
}
func (f *fixedTargetStore) Save(_ context.Context, t domain.DeployTarget) (domain.DeployTarget, error) {
	return t, nil
}
func (f *fixedTargetStore) Delete(context.Context, uuid.UUID, string, string) error { return nil }

// TestGoldenCreateSetupTaskDescription pins CreateSetupTask's exact task
// body, byte-for-byte, before this prose moves into
// catalog/system/prompts/briefs/**.
func TestGoldenCreateSetupTaskDescription(t *testing.T) {
	repositoryID := uuid.New()
	target := domain.DeployTarget{
		RepositoryID: repositoryID,
		Env:          domain.DeployEnvProd,
		Provider:     domain.DeployProviderGCPCloudRun,
		TemplateID:   "gcp-cloud-run",
		Vars: map[string]string{
			"gcp_project_id": "tt-prod",
			"gcp_region":     "europe-west1",
			"service_name":   "tasktrooper-api",
			"artifact_repo":  "tasktrooper",
			"health_url":     "https://api.example.com/health",
		},
	}
	repo := domain.Repository{ID: repositoryID, Kind: domain.RepoKindBackend}
	svc := deploy.NewService(&fixedTargetStore{target: target}, &fakeRepoResolver{repo: repo})
	tasks := &fakeTaskCreator{}
	svc.SetTaskCreator(tasks)

	_, err := svc.CreateSetupTask(context.Background(), repositoryID, domain.DeployEnvProd)
	require.NoError(t, err)

	tpl, ok := deploy.Template("gcp-cloud-run")
	require.True(t, ok)

	wantPrefix := "Author the prod deploy for this repository using the " + tpl.Name + " template.\n\n" +
		"Target: provider=gcp_cloud_run, env=prod, workflow file=.github/workflows/" + tpl.WorkflowFile + "\n" +
		"\nDefinition of done:\n" +
		"- the workflow file exists, is workflow_dispatch-triggerable and passes a manual run\n" +
		"- the deploy verifies itself (smoke check) and rolls back on failure\n" +
		"- the pipeline mapping for category " + domain.DeployEnvCategory(domain.DeployEnvProd) + " points at " + tpl.WorkflowFile + "\n" +
		"\n---\n\n"
	require.Equal(t, wantPrefix+deploy.Render(tpl, target), tasks.lastReq.Description)
	require.Equal(t, "Set up prod deploy ("+tpl.Name+")", tasks.lastReq.Title)
}

// TestGoldenCreateLocalSetupTaskDescription pins CreateLocalSetupTask's
// exact task body across the kinds whose bullet list branches
// (mobile/worker/plain), byte-for-byte, before it moves into
// catalog/system/prompts/briefs/**.
func TestGoldenCreateLocalSetupTaskDescription(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want string
	}{
		{
			name: "backend has no extra bullet",
			kind: domain.RepoKindBackend,
			want: "Write scripts/dev.sh: a COMPLETE, executable bootstrap script that gets this backend running on a developer's own machine. A script, not a markdown guide — do not write one.\n\n" +
				"Definition of done — running it once on a fresh machine leaves the project running, with no other step:\n" +
				"- installs every dependency and toolchain the project needs (checks first, installs only what is missing)\n" +
				"- prepares env/config: creates the .env (or equivalent) from the example with local defaults, runs the migrations and seeds a first run needs\n" +
				"- starts every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process\n" +
				"- idempotent: a second run is safe and duplicates nothing\n" +
				"- executable (`chmod +x`), `#!/usr/bin/env bash`, `set -euo pipefail`\n" +
				"- runs on macOS, Linux and Windows (Git Bash): branch on `uname -s` (`Darwin`, `Linux`, `MINGW*|MSYS*`) wherever a step differs — package manager, starting a service, paths. When a toolchain is missing and the OS has no installer the script can drive (no brew, apt or winget), print the exact install command and exit non-zero instead of guessing\n" +
				"- uses only tools every one of those shells has: probe a port with bash itself (`(: < /dev/tcp/127.0.0.1/$PORT) 2>/dev/null`) or node, never by parsing lsof, ss or netstat; no `ps -o`, `setsid` or `flock`, which Git Bash lacks\n" +
				"- a short usage header comment at the top: what it does, how to run it (`bash scripts/dev.sh [port]`, which works on every OS — the executable bit means nothing on Windows), and the port/URL it comes up on\n" +
				"- accepts an optional port argument (`scripts/dev.sh [port]`) overriding the default, so it can be rerun when the default port is taken\n" +
				"- matches what the repo actually needs today (its real package manager, build tool, ports) — not a generic template\n",
		},
		{
			name: "mobile adds the simulator/emulator bullet",
			kind: domain.RepoKindMobile,
			want: "Write scripts/dev.sh: a COMPLETE, executable bootstrap script that gets this mobile running on a developer's own machine. A script, not a markdown guide — do not write one.\n\n" +
				"Definition of done — running it once on a fresh machine leaves the project running, with no other step:\n" +
				"- installs every dependency and toolchain the project needs (checks first, installs only what is missing)\n" +
				"- prepares env/config: creates the .env (or equivalent) from the example with local defaults, runs the migrations and seeds a first run needs\n" +
				"- starts every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process\n" +
				"- idempotent: a second run is safe and duplicates nothing\n" +
				"- executable (`chmod +x`), `#!/usr/bin/env bash`, `set -euo pipefail`\n" +
				"- runs on macOS, Linux and Windows (Git Bash): branch on `uname -s` (`Darwin`, `Linux`, `MINGW*|MSYS*`) wherever a step differs — package manager, starting a service, paths. When a toolchain is missing and the OS has no installer the script can drive (no brew, apt or winget), print the exact install command and exit non-zero instead of guessing\n" +
				"- uses only tools every one of those shells has: probe a port with bash itself (`(: < /dev/tcp/127.0.0.1/$PORT) 2>/dev/null`) or node, never by parsing lsof, ss or netstat; no `ps -o`, `setsid` or `flock`, which Git Bash lacks\n" +
				"- a short usage header comment at the top: what it does, how to run it (`bash scripts/dev.sh [port]`, which works on every OS — the executable bit means nothing on Windows), and the port/URL it comes up on\n" +
				"- accepts an optional port argument (`scripts/dev.sh [port]`) overriding the default, so it can be rerun when the default port is taken\n" +
				"- matches what the repo actually needs today (its real package manager, build tool, ports) — not a generic template\n" +
				"- covers both iOS (simulator) and Android (emulator) if the repo ships both platforms; the iOS steps run only on macOS (`uname -s` is `Darwin`) — elsewhere they are skipped with a message and Android still comes up\n",
		},
		{
			name: "worker adds the queue bullet",
			kind: domain.RepoKindWorker,
			want: "Write scripts/dev.sh: a COMPLETE, executable bootstrap script that gets this worker running on a developer's own machine. A script, not a markdown guide — do not write one.\n\n" +
				"Definition of done — running it once on a fresh machine leaves the project running, with no other step:\n" +
				"- installs every dependency and toolchain the project needs (checks first, installs only what is missing)\n" +
				"- prepares env/config: creates the .env (or equivalent) from the example with local defaults, runs the migrations and seeds a first run needs\n" +
				"- starts every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process\n" +
				"- idempotent: a second run is safe and duplicates nothing\n" +
				"- executable (`chmod +x`), `#!/usr/bin/env bash`, `set -euo pipefail`\n" +
				"- runs on macOS, Linux and Windows (Git Bash): branch on `uname -s` (`Darwin`, `Linux`, `MINGW*|MSYS*`) wherever a step differs — package manager, starting a service, paths. When a toolchain is missing and the OS has no installer the script can drive (no brew, apt or winget), print the exact install command and exit non-zero instead of guessing\n" +
				"- uses only tools every one of those shells has: probe a port with bash itself (`(: < /dev/tcp/127.0.0.1/$PORT) 2>/dev/null`) or node, never by parsing lsof, ss or netstat; no `ps -o`, `setsid` or `flock`, which Git Bash lacks\n" +
				"- a short usage header comment at the top: what it does, how to run it (`bash scripts/dev.sh [port]`, which works on every OS — the executable bit means nothing on Windows), and the port/URL it comes up on\n" +
				"- accepts an optional port argument (`scripts/dev.sh [port]`) overriding the default, so it can be rerun when the default port is taken\n" +
				"- matches what the repo actually needs today (its real package manager, build tool, ports) — not a generic template\n" +
				"- starts any queue/broker the worker needs locally (or configures a fake/in-memory mode when there is none to start)\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := domain.Repository{ID: uuid.New(), Kind: tc.kind}
			svc := deploy.NewService(&fakeTargetStore{}, &fakeRepoResolver{repo: repo})
			tasks := &fakeTaskCreator{}
			svc.SetTaskCreator(tasks)

			_, err := svc.CreateLocalSetupTask(context.Background(), repo.ID, "")
			require.NoError(t, err)
			require.Equal(t, tc.want, tasks.lastReq.Description)
			require.Equal(t, "Write the local bootstrap script ("+tc.kind+")", tasks.lastReq.Title)
		})
	}
}
