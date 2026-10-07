package prompt_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/hostshell"
	"github.com/stretchr/testify/suite"
)

type HostEnvironmentSuite struct {
	suite.Suite
}

func TestHostEnvironmentSuite(t *testing.T) {
	suite.Run(t, new(HostEnvironmentSuite))
}

func host(os string, shell hostshell.Shell) hostshell.Host {
	return hostshell.Host{OS: os, Arch: "amd64", Shell: shell}
}

var (
	gitBash           = hostshell.Shell{Name: "Git Bash", Kind: hostshell.POSIX}
	posixSh           = hostshell.Shell{Name: "sh", Kind: hostshell.POSIX}
	pwsh              = hostshell.Shell{Name: "PowerShell", Kind: hostshell.PowerShell}
	windowsPowerShell = hostshell.Shell{Name: "Windows PowerShell", Kind: hostshell.PowerShell}
	cmdExe            = hostshell.Shell{Name: "cmd.exe", Kind: hostshell.Cmd}
)

func (s *HostEnvironmentSuite) TestEveryHostRenders() {
	cases := []struct {
		name string
		host hostshell.Host
	}{
		{"windows_git_bash", host("Windows", gitBash)},
		{"windows_pwsh", host("Windows", pwsh)},
		{"windows_powershell", host("Windows", windowsPowerShell)},
		{"windows_cmd", host("Windows", cmdExe)},
		{"macos_sh", host("macOS", posixSh)},
		{"linux_sh", host("Linux", posixSh)},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			assertGolden(s.T(), "host_environment_"+tc.name, prompt.HostEnvironmentNote(tc.host, true))
		})
	}
}

func (s *HostEnvironmentSuite) TestWindowsGitBashSaysForwardSlashesAndNoSudo() {
	got := prompt.HostEnvironmentNote(host("Windows", gitBash), true)

	s.Contains(got, "Windows (amd64)")
	s.Contains(got, "Git Bash")
	s.Contains(got, "forward slashes")
	s.Contains(got, "sudo")
}

func (s *HostEnvironmentSuite) TestPowerShellSaysItIsNotBash() {
	got := prompt.HostEnvironmentNote(host("Windows", windowsPowerShell), true)

	s.Contains(got, "Windows PowerShell, not bash")
	s.Contains(got, "$env:NAME")
	s.NotContains(got, "or `&&`", "Windows PowerShell 5.1 has no && operator")
	s.Contains(prompt.HostEnvironmentNote(host("Windows", pwsh), true), "or `&&`")
}

func (s *HostEnvironmentSuite) TestMacOSWarnsAboutBSDSed() {
	s.Contains(prompt.HostEnvironmentNote(host("macOS", posixSh), true), "sed -i ''")
}

// An agent CLI runs commands through its own shell tool, so the note names the
// OS only and never claims what run_terminal uses.
func (s *HostEnvironmentSuite) TestWithoutShellNamesOnlyTheOS() {
	got := prompt.HostEnvironmentNote(host("Windows", gitBash), false)

	s.Contains(got, "Windows (amd64)")
	s.NotContains(got, "run_terminal")
	s.NotContains(got, "Git Bash")
}

func (s *HostEnvironmentSuite) TestSystemPromptCarriesTheHost() {
	current := hostshell.Current()

	inPrompt := prompt.BuildSystemPromptFor(domain.Agent{}, nil, nil, nil, "", prompt.SkillsInPrompt)
	s.Contains(inPrompt, prompt.HostEnvironmentNote(current, true))

	onDisk := prompt.BuildSystemPromptFor(domain.Agent{}, nil, nil, nil, "", prompt.SkillsOnDisk)
	s.Contains(onDisk, prompt.HostEnvironmentNote(current, false))
}

func (s *HostEnvironmentSuite) TestShellPathUsesForwardSlashesOnlyForGitBashOnWindows() {
	win := `C:\Users\me\AppData\Roaming\TaskTrooper\workspaces\task-1`
	s.Equal("C:/Users/me/AppData/Roaming/TaskTrooper/workspaces/task-1", prompt.ShellPathFor("windows", hostshell.POSIX, win))
	s.Equal(win, prompt.ShellPathFor("windows", hostshell.PowerShell, win))
	s.Equal("/home/me/ws", prompt.ShellPathFor("linux", hostshell.POSIX, "/home/me/ws"))
}
