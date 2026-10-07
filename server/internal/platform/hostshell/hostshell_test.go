package hostshell

import (
	"context"
	"encoding/base64"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/suite"
)

type HostShellSuite struct {
	suite.Suite
}

func TestHostShellSuite(t *testing.T) {
	suite.Run(t, new(HostShellSuite))
}

func fakeProbe(onPath map[string]string, files []string, env map[string]string) probe {
	fileSet := make(map[string]bool, len(files))
	for _, f := range files {
		fileSet[f] = true
	}
	return probe{
		lookPath: func(name string) (string, error) {
			if p, ok := onPath[name]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		},
		isFile: func(p string) bool { return fileSet[p] },
		getenv: func(k string) string { return env[k] },
	}
}

func (s *HostShellSuite) TestUnixUsesSh() {
	got := detect("linux", fakeProbe(map[string]string{"sh": "/usr/bin/sh"}, nil, nil))
	s.Equal(Shell{Path: "/usr/bin/sh", Name: "sh", Kind: POSIX}, got)
}

func (s *HostShellSuite) TestUnixFallsBackToBinShWhenPathIsEmpty() {
	got := detect("darwin", fakeProbe(nil, nil, nil))
	s.Equal("/bin/sh", got.Path)
	s.Equal(POSIX, got.Kind)
}

func (s *HostShellSuite) TestWindowsFindsGitBashNextToGitOnPath() {
	gitDir := filepath.Join("C:", "Program Files", "Git")
	bash := filepath.Join(gitDir, "bin", "bash.exe")
	got := detect("windows", fakeProbe(
		map[string]string{"git": filepath.Join(gitDir, "cmd", "git.exe"), "powershell": "powershell.exe"},
		[]string{bash}, nil))
	s.Equal(Shell{Path: bash, Name: "Git Bash", Kind: POSIX}, got)
}

func (s *HostShellSuite) TestWindowsFindsGitBashFromMingwGit() {
	gitDir := filepath.Join("D:", "Tools", "Git")
	bash := filepath.Join(gitDir, "bin", "bash.exe")
	got := detect("windows", fakeProbe(
		map[string]string{"git": filepath.Join(gitDir, "mingw64", "bin", "git.exe")},
		[]string{bash}, nil))
	s.Equal(bash, got.Path)
}

func (s *HostShellSuite) TestWindowsFindsGitBashUnderProgramFilesWhenGitIsNotOnPath() {
	bash := filepath.Join("C:", "Program Files", "Git", "bin", "bash.exe")
	got := detect("windows", fakeProbe(nil, []string{bash},
		map[string]string{"ProgramFiles": filepath.Join("C:", "Program Files")}))
	s.Equal(bash, got.Path)
	s.Equal(POSIX, got.Kind)
}

func (s *HostShellSuite) TestWindowsNeverPicksTheWSLBashLauncher() {
	got := detect("windows", fakeProbe(
		map[string]string{"bash": `C:\Windows\System32\bash.exe`, "powershell": "powershell.exe"}, nil, nil))
	s.Equal(PowerShell, got.Kind)
	s.Equal("Windows PowerShell", got.Name)
}

func (s *HostShellSuite) TestWindowsPrefersPwshOverWindowsPowerShell() {
	got := detect("windows", fakeProbe(map[string]string{"pwsh": "pwsh.exe", "powershell": "powershell.exe"}, nil, nil))
	s.Equal("PowerShell", got.Name)
	s.Equal("pwsh.exe", got.Path)
}

func (s *HostShellSuite) TestWindowsFallsBackToComSpec() {
	got := detect("windows", fakeProbe(nil, nil, map[string]string{"ComSpec": `C:\Windows\system32\cmd.exe`}))
	s.Equal(Shell{Path: `C:\Windows\system32\cmd.exe`, Name: "cmd.exe", Kind: Cmd}, got)
}

func (s *HostShellSuite) TestPowerShellScriptIsEncodedAsUTF16LE() {
	script := `Write-Output "héllo"; Get-ChildItem`
	args := Shell{Path: "pwsh.exe", Kind: PowerShell}.args(script)
	s.Equal("-EncodedCommand", args[len(args)-2])

	raw, err := base64.StdEncoding.DecodeString(args[len(args)-1])
	s.Require().NoError(err)
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	s.Equal(script, string(utf16.Decode(units)))
}

func (s *HostShellSuite) TestOSName() {
	s.Equal("macOS", OSName("darwin"))
	s.Equal("Windows", OSName("windows"))
	s.Equal("Linux", OSName("linux"))
	s.Equal("plan9", OSName("plan9"))
}

func (s *HostShellSuite) TestDefaultRunsAScript() {
	if runtime.GOOS == "windows" {
		s.T().Skip("covered by the detection tests; the shell found depends on the machine")
	}
	out, err := Default().Command(context.Background(), `printf '%s' "$((1+2))"`).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		s.FailNow(string(exitErr.Stderr))
	}
	s.Require().NoError(err)
	s.Equal("3", strings.TrimSpace(string(out)))
}

func (s *HostShellSuite) TestResolveArgvRunsScriptsThroughGitBash() {
	bash := Shell{Path: `C:\Program Files\Git\bin\bash.exe`, Name: "Git Bash", Kind: POSIX}

	s.Equal([]string{bash.Path, "./scripts/ci.sh", "--fast"}, resolveArgv(bash, []string{"./scripts/ci.sh", "--fast"}))
	s.Equal([]string{bash.Path, "scripts/release-local.sh", "web"}, resolveArgv(bash, []string{"bash", "scripts/release-local.sh", "web"}))
	s.Equal([]string{bash.Path, "x.sh"}, resolveArgv(bash, []string{`C:\Windows\System32\bash.exe`, "x.sh"}))
	s.Equal([]string{"npm", "test"}, resolveArgv(bash, []string{"npm", "test"}))
}

func (s *HostShellSuite) TestResolveArgvLeavesArgvAloneWithoutAPOSIXShell() {
	ps := Shell{Path: "powershell.exe", Kind: PowerShell}
	s.Equal([]string{"./ci.sh"}, resolveArgv(ps, []string{"./ci.sh"}))
}
