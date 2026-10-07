package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/hostshell"
)

func TestBlockingCommandReasonRefusesServers(t *testing.T) {
	// Every one of these runs until interrupted, so in the foreground the only
	// possible outcome is the tool timeout.
	cases := []string{
		"npm run dev",
		"pnpm dev",
		"yarn start",
		"bun run dev",
		"npx next dev",
		"vite",
		"nodemon server.js",
		"webpack serve",
		"python3 -m http.server 8080",
		"uvicorn app:app --reload",
		"docker compose up",
		"docker-compose up --build",
		"tail -f /var/log/app.log",
		"kubectl port-forward svc/api 8080:80",
		"journalctl -fu bridge",
		// The shape that actually burned the budget: the server hidden behind a cd.
		"cd /data/workspaces/task-43f50579 && npm run dev 2>&1 | head -30",
		// Environment prefixes and sudo do not change what the command does.
		"PORT=3000 npm run dev",
		"sudo docker compose up",
	}
	for _, cmd := range cases {
		if reason := blockingCommandReason(cmd); reason == "" {
			t.Errorf("blockingCommandReason(%q) = \"\", want a refusal", cmd)
		}
	}
}

func TestBlockingCommandReasonAllowsTerminatingCommands(t *testing.T) {
	// These finish on their own. Refusing any of them would block real work,
	// which is why the patterns are kept narrow.
	cases := []string{
		"npm run build",
		"npm test",
		"npm run typecheck",
		"npm ci",
		"go build ./...",
		"go test ./...",
		"git status",
		"ls -la",
		"cat package.json",
		"grep -rn dev src",
		"docker compose build",
		"docker compose down",
		"tail -n 50 /var/log/app.log",
		"kubectl get pods",
		// "dev" appearing as an argument rather than the script being run.
		"cat /dev/null",
		"echo dev start serve",
	}
	for _, cmd := range cases {
		if reason := blockingCommandReason(cmd); reason != "" {
			t.Errorf("blockingCommandReason(%q) = %q, want \"\"", cmd, reason)
		}
	}
}

// Backgrounding is the supported way to get a server up, so a detached command
// passes through untouched — that is the escape hatch the refusal points at.
func TestBlockingCommandReasonAllowsBackgroundedServer(t *testing.T) {
	if reason := blockingCommandReason("npm run dev > /tmp/dev.log 2>&1 &"); reason != "" {
		t.Fatalf("backgrounded server refused: %q", reason)
	}
}

func TestBlockingCommandReasonNamesTheAlternative(t *testing.T) {
	reason := blockingCommandReason("npm run dev")
	for _, want := range []string{"build", "/tmp/tt-<task key>/dev.log"} {
		if !strings.Contains(reason, want) {
			t.Errorf("refusal %q does not mention %q; the agent needs to be told what to do instead", reason, want)
		}
	}
}

// The shipped 60s cap was below the cost of the loop every developer agent
// runs — install, build, test — so the agent could never verify its own work.
// A slow command must be able to ask for the time it needs, up to the ceiling.
func TestResolveTimeoutHonoursTheRequestedBudget(t *testing.T) {
	s := &shellTool{timeout: 3 * time.Minute, maxTimeout: 15 * time.Minute}

	if got := s.resolveTimeout(0); got != 3*time.Minute {
		t.Errorf("unspecified budget = %s, want the configured default", got)
	}
	if got := s.resolveTimeout(600); got != 10*time.Minute {
		t.Errorf("requested 600s = %s, want 10m", got)
	}
}

// The ceiling is the operator's, not the agent's: a request beyond it is
// clamped rather than honoured, so one command cannot hold a run indefinitely.
func TestResolveTimeoutClampsToTheCeiling(t *testing.T) {
	s := &shellTool{timeout: 3 * time.Minute, maxTimeout: 15 * time.Minute}

	if got := s.resolveTimeout(86400); got != 15*time.Minute {
		t.Errorf("requested a day = %s, want the ceiling", got)
	}
}

// A ceiling below the default would silently shorten every command, so New
// raises it to the default instead.
func TestNewNeverLetsTheCeilingUndercutTheDefault(t *testing.T) {
	s := New("/tmp", 5*time.Minute, time.Minute, domain.TerminalSandboxConfig{}).(*shellTool)

	if got := s.resolveTimeout(0); got != 5*time.Minute {
		t.Fatalf("default budget = %s, want 5m; a low ceiling must not shorten it", got)
	}
}

// A server started with its PID recorded used to be killed with the call's
// process tree: only a trailing `&` counted as backgrounding.
func TestBackgroundsAProcessPOSIX(t *testing.T) {
	cases := map[string]bool{
		"npm run dev > /tmp/dev.log 2>&1 &":                      true,
		"(npm run dev > log 2>&1 & echo $! > /tmp/tt-x/dev.pid)": true,
		"PORT=1 ./svc > log 2>&1 & echo $! > pid":                true,
		"nohup npx vite preview > log 2>&1 &":                    true,
		"go build ./... && go test ./...":                        false,
		"npm test 2>&1 | tail -20":                               false,
		"make build &> build.log":                                false,
		"echo err >&2":                                           false,
		"curl 'http://localhost:3000/api?a=1&b=2'":               false,
		`curl "http://localhost:3000/api?a=1&b=2" -o out.json`:   false,
		"cmd |& tee out.log":                                     false,
	}
	for command, want := range cases {
		if got := backgroundsAProcess(command, hostshell.POSIX); got != want {
			t.Errorf("backgroundsAProcess(%q) = %v, want %v", command, got, want)
		}
	}
}

func TestBackgroundsAProcessWindowsShells(t *testing.T) {
	ps := map[string]bool{
		"Start-Process -NoNewWindow -FilePath npm -ArgumentList 'run','dev'": true,
		"$j = Start-Job { npm run dev }":                                     true,
		"npm run dev &":                                                      true,
		"npm test; Get-Content log":                                          false,
	}
	for command, want := range ps {
		if got := backgroundsAProcess(command, hostshell.PowerShell); got != want {
			t.Errorf("PowerShell %q = %v, want %v", command, got, want)
		}
	}
	cmd := map[string]bool{
		`start "" /b npm run dev > log 2>&1`: true,
		`mkdir x & start /b node server.js`:  true,
		`npm test && type log`:               false,
	}
	for command, want := range cmd {
		if got := backgroundsAProcess(command, hostshell.Cmd); got != want {
			t.Errorf("cmd %q = %v, want %v", command, got, want)
		}
	}
}

func TestBlockingRefusalDetachesInTheHostShell(t *testing.T) {
	ps := prompt.ShellBlockingCommandText("npm run dev", "a dev server", string(hostshell.PowerShell))
	if !strings.Contains(ps, "Start-Process") || strings.Contains(ps, "mkdir -p") {
		t.Errorf("PowerShell refusal should detach with Start-Process: %q", ps)
	}
	cmd := prompt.ShellBlockingCommandText("npm run dev", "a dev server", string(hostshell.Cmd))
	if !strings.Contains(cmd, `start "" /b npm run dev`) {
		t.Errorf("cmd.exe refusal should detach with start /b: %q", cmd)
	}
}
