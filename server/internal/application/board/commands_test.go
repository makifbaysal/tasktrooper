package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveVerifyStages_GoProject(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	stages := ResolveVerifyStages(dir, domain.Repository{}, nil)
	if len(stages) != 2 || stages[0].Command[0] != "go" || stages[1].Command[1] != "vet" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func TestResolveVerifyStages_OverrideWins(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	stages := ResolveVerifyStages(dir, domain.Repository{VerifyCommand: "make check"}, nil)
	if len(stages) != 1 || stages[0].Command[0] != "make" || stages[0].Command[1] != "check" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func TestResolveVerifyStages_Rust(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "Cargo.toml")
	stages := ResolveVerifyStages(dir, domain.Repository{}, nil)
	if len(stages) != 1 || stages[0].Command[0] != "cargo" || stages[0].Command[1] != "check" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func TestResolveVerifyStages_Python(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "pyproject.toml")
	stages := ResolveVerifyStages(dir, domain.Repository{}, nil)
	if len(stages) != 1 || stages[0].Command[0] != "python" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func writeNPMProject(t *testing.T, dir string) {
	t.Helper()
	pkg := `{"scripts":{"build":"next build","test":"vitest run"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveVerifyStages_InstallsNPMDepsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	writeNPMProject(t, dir)
	stages := ResolveVerifyStages(dir, domain.Repository{}, nil)
	if len(stages) != 2 {
		t.Fatalf("expected install + build, got %+v", stages)
	}
	if !stages[0].Setup || stages[0].Command[1] != "install" {
		t.Fatalf("first stage must be the dependency install: %+v", stages[0])
	}
	if stages[0].Timeout <= 0 {
		t.Error("install stage needs its own timeout")
	}
	if stages[1].Command[2] != "build" {
		t.Fatalf("second stage must be the build: %+v", stages[1])
	}
}

func TestResolveVerifyStages_UsesNPMCIWithLockfile(t *testing.T) {
	dir := t.TempDir()
	writeNPMProject(t, dir)
	touch(t, dir, "package-lock.json")
	stages := ResolveVerifyStages(dir, domain.Repository{}, nil)
	if stages[0].Command[1] != "ci" {
		t.Fatalf("a lockfile must select npm ci: %+v", stages[0])
	}
}

func TestResolveVerifyStages_NPMBuildWithNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeNPMProject(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	stages := ResolveVerifyStages(dir, domain.Repository{}, nil)
	if len(stages) != 1 || stages[0].Command[0] != "npm" {
		t.Fatalf("unexpected stages: %+v", stages)
	}
}

func writeLockedPackage(t *testing.T, dir string, withNodeModules bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, dir, "package.json")
	touch(t, dir, "package-lock.json")
	if withNodeModules {
		if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func stageNames(stages []Stage) string {
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}

func TestResolveVerifyStages_DeclaredCommandInstallsNestedPackages(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	writeLockedPackage(t, filepath.Join(dir, "desktop"), false)
	writeLockedPackage(t, filepath.Join(dir, "desktop", "ui"), false)
	writeLockedPackage(t, filepath.Join(dir, "tools", "node_modules", "dep"), false)
	writeLockedPackage(t, filepath.Join(dir, "a", "b", "c", "d"), false)
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(dir, "docs"), "package.json")

	stages := ResolveVerifyStages(dir, domain.Repository{VerifyCommand: "make lint"}, nil)
	if got, want := stageNames(stages), "npm-install-desktop,npm-install-desktop/ui,verify"; got != want {
		t.Fatalf("stages = %s, want %s", got, want)
	}
	for _, s := range stages[:2] {
		if !s.Setup || s.Command[0] != "npm" || s.Command[1] != "ci" {
			t.Fatalf("setup stage %+v is not an npm ci setup stage", s)
		}
	}
	if last := stages[len(stages)-1]; last.Setup || strings.Join(last.Command, " ") != "make lint" {
		t.Fatalf("last stage = %+v, want the declared command", last)
	}
}

func TestResolveVerifyStages_DeclaredCommandSkipsInstalledPackages(t *testing.T) {
	dir := t.TempDir()
	writeLockedPackage(t, dir, true)
	writeLockedPackage(t, filepath.Join(dir, "web"), true)
	stages := ResolveVerifyStages(dir, domain.Repository{VerifyCommand: "npm test"}, nil)
	if got := stageNames(stages); got != "verify" {
		t.Fatalf("stages = %s, want only verify", got)
	}
}

func TestResolveVerifyStages_DeclaredCommandInstallsRootPackage(t *testing.T) {
	dir := t.TempDir()
	writeLockedPackage(t, dir, false)
	stages := ResolveVerifyStages(dir, domain.Repository{VerifyCommand: "npm test"}, nil)
	if got := stageNames(stages); got != "npm-install,verify" {
		t.Fatalf("stages = %s, want npm-install,verify", got)
	}
	if strings.Contains(strings.Join(stages[0].Command, " "), "--prefix") {
		t.Fatalf("root install should run in place: %+v", stages[0])
	}
}

func TestResolveVerifyStages_RequiredCommandsWinOverEverything(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	required := []domain.LocalCommand{
		{Dir: "apps/api", Argv: []string{"go", "test", "./..."}},
		{Dir: ".", Argv: []string{"go", "vet", "./..."}},
	}
	stages := ResolveVerifyStages(dir, domain.Repository{VerifyCommand: "make check"}, required)
	if got, want := stageNames(stages), "check:apps/api:go,check:.:go"; got != want {
		t.Fatalf("stages = %s, want %s", got, want)
	}
	if stages[0].Dir != "apps/api" || stages[1].Dir != "." {
		t.Fatalf("stage dirs = %+v", stages)
	}
	if stages[0].Command[1] != "test" || stages[1].Command[1] != "vet" {
		t.Fatalf("stage commands = %+v", stages)
	}
}

func TestResolveVerifyStages_RequiredCommandsInstallNestedPackagesFirst(t *testing.T) {
	dir := t.TempDir()
	writeLockedPackage(t, filepath.Join(dir, "web"), false)
	required := []domain.LocalCommand{{Dir: "web", Argv: []string{"npm", "test"}}}
	stages := ResolveVerifyStages(dir, domain.Repository{}, required)
	if got, want := stageNames(stages), "npm-install-web,check:web:npm"; got != want {
		t.Fatalf("stages = %s, want %s", got, want)
	}
}

func TestVerifyEnvRefusesNpxAutoInstall(t *testing.T) {
	env := strings.Join(verifyEnv([]string{"PATH=/usr/bin"}, []string{"GOTOOLCHAIN=local"}), "\n")
	if !strings.Contains(env, "npm_config_yes=false") || !strings.Contains(env, "GOTOOLCHAIN=local") {
		t.Fatalf("env = %s", env)
	}
}
