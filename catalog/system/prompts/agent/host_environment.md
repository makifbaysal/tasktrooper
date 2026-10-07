---
key: agent.host_environment
version: 1
inputs: [OS, Arch, ShellName, ShellKind, HasShell]
---
## Host machine
This run executes on {{.OS}} ({{.Arch}}). Write every command, path and script invocation for this OS{{if .HasShell}} and for the shell run_terminal uses: {{.ShellName}}{{end}}. Never assume macOS or Linux tools on a machine that is not one.
{{- if .HasShell}}
{{- if eq .ShellKind "posix"}}
{{- if eq .OS "Windows"}}
- run_terminal runs Git Bash: POSIX syntax (`&&`, `$VAR`, single quotes) and the Unix tools Git ships (ls, cat, grep, sed, find, mkdir -p) work. Windows programs (git, node, npm, go, python, dotnet) run as usual; run a `.bat`/`.cmd` script as `cmd //c script.cmd`.
- Write paths with forward slashes (`C:/Users/me/repo` or `/c/Users/me/repo`): a backslash is an escape character in bash. Quote any path with spaces.
- Not available: sudo, apt, brew, `open`, systemctl, lsof. `chmod +x` has no effect; run a script as `bash script.sh`. To find what holds a port use `netstat -ano | grep :<port>` and stop it with `taskkill //PID <pid> //F`.
{{- else if eq .OS "macOS"}}
- run_terminal runs `sh` (POSIX). The userland is BSD, not GNU: `sed -i ''` needs the empty suffix, `grep -P` and `timeout` do not exist, `date -d` and `readlink -f` behave differently.
{{- else}}
- run_terminal runs `sh` (POSIX) with GNU tools: `sed -i` takes no suffix argument.
{{- end}}
{{- else if eq .ShellKind "powershell"}}
- run_terminal runs {{.ShellName}}, not bash: chain commands with `;`{{if eq .ShellName "PowerShell"}} or `&&`{{end}}, read environment variables as `$env:NAME`, list with `Get-ChildItem`, search with `Select-String`, delete with `Remove-Item -Recurse -Force`, create a directory with `New-Item -ItemType Directory -Force`. POSIX tools and syntax (sed, grep, chmod, `export X=1`, `2>&1 &`) do not exist.
- Quote any path with spaces. To find what holds a port use `Get-NetTCPConnection -LocalPort <port>` and stop it with `Stop-Process -Id <pid> -Force`.
{{- else}}
- run_terminal runs cmd.exe: chain commands with `&&`, read environment variables as `%NAME%`, use `dir`, `type`, `findstr`, `rmdir /s /q`, `mkdir`. POSIX tools and syntax do not exist.
{{- end}}
{{- end}}
