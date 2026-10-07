package board

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Stage struct {
	Name string
	// Dir is repo-relative; runVerification runs the stage inside
	// filepath.Join(workspace, Dir) and refuses a Dir that escapes the workspace.
	Dir     string
	Command []string
	Setup   bool
	Timeout time.Duration
}

func splitCommand(s string) []string { return strings.Fields(strings.TrimSpace(s)) }

func markerExists(dir string, names ...string) bool {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

func detectBuild(dir string) []Stage {
	var out []Stage
	switch {
	case markerExists(dir, "go.mod"):
		out = append(out,
			Stage{Name: "build", Command: []string{"go", "build", "./..."}},
			Stage{Name: "vet", Command: []string{"go", "vet", "./..."}})
	case markerExists(dir, "Cargo.toml"):
		out = append(out, Stage{Name: "build", Command: []string{"cargo", "check"}})
	case markerExists(dir, "pyproject.toml", "requirements.txt"):
		out = append(out, Stage{Name: "build", Command: []string{pythonCommand(runtime.GOOS, exec.LookPath), "-m", "compileall", "."}})
	case markerExists(dir, "pom.xml"):
		out = append(out, Stage{Name: "build", Command: []string{"mvn", "-q", "compile"}})
	case markerExists(dir, "build.gradle", "build.gradle.kts"):
		out = append(out, Stage{Name: "build", Command: []string{"gradle", "build", "-x", "test"}})
	}
	if markerExists(dir, "pubspec.yaml") {
		out = append(out, Stage{Name: "analyze", Command: []string{"flutter", "analyze"}})
	}
	if hasNPMScript(dir, "build") {
		out = append(out, npmInstallStage(dir, "")...)
		out = append(out, Stage{Name: "build-web", Command: []string{"npm", "run", "build"}})
	}
	if webDir := filepath.Join(dir, "web"); hasNPMScript(webDir, "build") {
		out = append(out, npmInstallStage(webDir, "web")...)
		out = append(out, Stage{Name: "build-web", Command: []string{"npm", "run", "build", "--prefix", webDir}})
	}
	return out
}

// pythonCommand names the interpreter a Python build stage runs. macOS 12.3+
// and Debian/Ubuntu ship python3 with no python, so python3 comes first.
// Windows is the other way round: python.org installs python.exe and the py
// launcher but no python3.exe, and a bare python3 lookup there lands on the
// Microsoft Store alias stub, which exits 9009 instead of running anything.
func pythonCommand(goos string, lookPath func(string) (string, error)) string {
	candidates, fallback := []string{"python3", "python"}, "python3"
	if goos == "windows" {
		candidates, fallback = []string{"py", "python", "python3"}, "python"
	}
	for _, name := range candidates {
		if _, err := lookPath(name); err == nil {
			return name
		}
	}
	return fallback
}

const npmInstallDefaultTimeout = 15 * time.Minute

func npmInstallStage(dir, label string) []Stage {
	if hasNodeModules(dir) {
		return nil
	}
	cmd := []string{"npm", "install", "--no-audit", "--no-fund"}
	if markerExists(dir, "package-lock.json") {
		cmd = []string{"npm", "ci", "--no-audit", "--no-fund"}
	}
	if label != "" {
		cmd = append(cmd, "--prefix", dir)
	}
	name := "npm-install"
	if label != "" {
		name += "-" + label
	}
	return []Stage{{Name: name, Command: cmd, Setup: true, Timeout: npmInstallDefaultTimeout}}
}

func hasNodeModules(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "node_modules"))
	return err == nil && info.IsDir()
}

// ResolveVerifyStages picks the verification plan for one run: the project
// model's required checks when there are any, else the repository's own
// declared VerifyCommand, else the language auto-detection.
func ResolveVerifyStages(dir string, repo domain.Repository, required []domain.LocalCommand) []Stage {
	if len(required) > 0 {
		stages := nodeSetupStages(dir)
		for _, cmd := range required {
			if len(cmd.Argv) == 0 {
				continue
			}
			stages = append(stages, Stage{
				Name:    fmt.Sprintf("check:%s:%s", cmd.Dir, cmd.Argv[0]),
				Command: cmd.Argv,
				Dir:     cmd.Dir,
			})
		}
		return stages
	}
	if cmd := splitCommand(repo.VerifyCommand); len(cmd) > 0 {
		return append(nodeSetupStages(dir), Stage{Name: "verify", Command: cmd})
	}
	return detectBuild(dir)
}

const (
	nodeSetupMaxDepth  = 3
	nodeSetupMaxStages = 6
)

var skipSetupDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"out": true, "coverage": true, "target": true,
}

func nodeSetupStages(dir string) []Stage {
	var rels []string
	var walk func(abs, rel string, depth int)
	walk = func(abs, rel string, depth int) {
		if len(rels) >= nodeSetupMaxStages {
			return
		}
		if markerExists(abs, "package.json") && markerExists(abs, "package-lock.json") && !hasNodeModules(abs) {
			rels = append(rels, rel)
		}
		if depth >= nodeSetupMaxDepth {
			return
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") || skipSetupDirs[name] {
				continue
			}
			walk(filepath.Join(abs, name), filepath.Join(rel, name), depth+1)
		}
	}
	walk(dir, "", 0)
	var stages []Stage
	for _, rel := range rels {
		if rel == "" {
			stages = append(stages, npmInstallStage(dir, "")...)
			continue
		}
		stages = append(stages, npmInstallStage(filepath.Join(dir, rel), filepath.ToSlash(rel))...)
	}
	return stages
}
