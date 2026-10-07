package winshim

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

type WinShimSuite struct {
	suite.Suite
}

func TestWinShimSuite(t *testing.T) {
	suite.Run(t, new(WinShimSuite))
}

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

const cmdShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0

IF EXIST "%dp0%\node.exe" (
  SET "_prog=%dp0%\node.exe"
) ELSE (
  SET "_prog=node"
  SET PATHEXT=%PATHEXT:;.JS;=;%
)

endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\@anthropic-ai\claude-code\cli.js" %*
`

const npxCmd = `:: Created by npm, please don't edit manually.
@ECHO OFF

SETLOCAL

SET "NODE_EXE=%~dp0\node.exe"
IF NOT EXIST "%NODE_EXE%" (
  SET "NODE_EXE=node"
)

SET "NPM_PREFIX_JS=%~dp0\node_modules\npm\bin\npm-prefix.js"
SET "NPX_CLI_JS=%~dp0\node_modules\npm\bin\npx-cli.js"
FOR /F "delims=" %%F IN ('CALL "%NODE_EXE%" "%NPM_PREFIX_JS%"') DO (
  SET "NPM_PREFIX_NPX_CLI_JS=%%F\node_modules\npm\bin\npx-cli.js"
)
IF EXIST "%NPM_PREFIX_NPX_CLI_JS%" (
  SET "NPX_CLI_JS=%NPM_PREFIX_NPX_CLI_JS%"
)

"%NODE_EXE%" "%NPX_CLI_JS%" %*
`

const pnpmShim = `@SETLOCAL
@IF NOT DEFINED NODE_PATH (
  @SET "NODE_PATH=%~dp0\..\pnpm\dist\node_modules"
)
@IF EXIST "%~dp0\node.exe" (
  "%~dp0\node.exe"  "%~dp0\..\pnpm\bin\pnpm.cjs" %*
) ELSE (
  @SET PATHEXT=%PATHEXT:;.JS;=;%
  node  "%~dp0\..\pnpm\bin\pnpm.cjs" %*
)
`

const nativeShim = `@ECHO off
"%~dp0\node_modules\@anthropic-ai\claude-code\bin\claude.exe"   %*
`

func files(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

func noNode() (string, error) { return "", errors.New("not found") }

func nodeOnPath() (string, error) { return `C:\Program Files\nodejs\node.exe`, nil }

func (s *WinShimSuite) TestNpmCmdShimUsesNodeOnPath() {
	shim := `C:\Users\John Doe\AppData\Roaming\npm\claude.cmd`
	cli := `C:\Users\John Doe\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\cli.js`

	got, ok := parse(shim, crlf(cmdShim), files(cli), nodeOnPath)

	s.Require().True(ok)
	s.Equal([]string{`C:\Program Files\nodejs\node.exe`, cli}, got)
}

func (s *WinShimSuite) TestNpmCmdShimPrefersNodeNextToIt() {
	shim := `C:\Program Files\nodejs\claude.cmd`
	cli := `C:\Program Files\nodejs\node_modules\@anthropic-ai\claude-code\cli.js`
	node := `C:\Program Files\nodejs\node.exe`

	got, ok := parse(shim, cmdShim, files(cli, node), noNode)

	s.Require().True(ok)
	s.Equal([]string{node, cli}, got)
}

func (s *WinShimSuite) TestNpxCmd() {
	shim := `C:\Program Files\nodejs\npx.cmd`
	node := `C:\Program Files\nodejs\node.exe`
	cli := `C:\Program Files\nodejs\node_modules\npm\bin\npx-cli.js`

	got, ok := parse(shim, crlf(npxCmd), files(node, cli), noNode)

	s.Require().True(ok)
	s.Equal([]string{node, cli}, got)
}

func (s *WinShimSuite) TestPnpmShim() {
	shim := `C:\Users\me\AppData\Local\pnpm\pnpm.cmd`
	cli := `C:\Users\me\AppData\Local\pnpm\bin\pnpm.cjs`

	got, ok := parse(shim, pnpmShim, files(cli), nodeOnPath)

	s.Require().True(ok)
	s.Equal([]string{`C:\Program Files\nodejs\node.exe`, cli}, got)
}

func (s *WinShimSuite) TestShimLaunchingANativeExe() {
	shim := `C:\npm\claude.cmd`
	exe := `C:\npm\node_modules\@anthropic-ai\claude-code\bin\claude.exe`

	got, ok := parse(shim, nativeShim, files(exe), noNode)

	s.Require().True(ok)
	s.Equal([]string{exe}, got)
}

func (s *WinShimSuite) TestUnresolvableShimFallsBack() {
	_, ok := parse(`C:\npm\claude.cmd`, cmdShim, files(), nodeOnPath)
	s.False(ok, "the script is missing")

	_, ok = parse(`C:\npm\claude.cmd`, cmdShim, files(`C:\npm\node_modules\@anthropic-ai\claude-code\cli.js`), noNode)
	s.False(ok, "no node to run it with")

	_, ok = parse(`C:\tools\build.bat`, "@echo off\r\nmsbuild /m\r\n", files(), nodeOnPath)
	s.False(ok, "a batch file that is not a shim")
}

func (s *WinShimSuite) TestResolveIgnoresNonShims() {
	_, ok := Resolve("/usr/local/bin/claude")
	s.False(ok)
}
