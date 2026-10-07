package core

import (
	"os"
	"runtime"
	"strings"
)

// windowsProcessEnv is what a Windows process needs beyond PATH just to run
// and find its own profile: without SystemRoot sockets and crypto fail,
// without USERPROFILE/APPDATA an npm CLI cannot find its login, and without
// TEMP it writes into C:\Windows. None of them is a credential.
var windowsProcessEnv = map[string]bool{
	"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "SYSTEMDRIVE": true,
	"COMSPEC": true, "TEMP": true, "TMP": true, "USERPROFILE": true, "HOMEDRIVE": true,
	"HOMEPATH": true, "APPDATA": true, "LOCALAPPDATA": true, "PROGRAMDATA": true,
	"ALLUSERSPROFILE": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "PROGRAMW6432": true,
	"COMMONPROGRAMFILES": true, "COMMONPROGRAMFILES(X86)": true, "PUBLIC": true, "OS": true,
	"NUMBER_OF_PROCESSORS": true, "PROCESSOR_ARCHITECTURE": true, "USERNAME": true,
	"USERDOMAIN": true, "COMPUTERNAME": true,
}

// MinimalEnv is a toolchain-only child environment: PATH and HOME, and on
// Windows the process basics in windowsProcessEnv, with HOME taken from
// USERPROFILE when it is unset there.
func MinimalEnv() []string {
	return minimalEnv(runtime.GOOS, os.Environ())
}

func minimalEnv(goos string, environ []string) []string {
	lookup := func(key string) string {
		for i := len(environ) - 1; i >= 0; i-- {
			name, val, ok := strings.Cut(environ[i], "=")
			if ok && (name == key || goos == "windows" && strings.EqualFold(name, key)) {
				return val
			}
		}
		return ""
	}
	if goos != "windows" {
		return []string{"PATH=" + lookup("PATH"), "HOME=" + lookup("HOME")}
	}
	var out []string
	hasHome := false
	for _, entry := range environ {
		name, val, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}
		upper := strings.ToUpper(name)
		if upper == "HOME" && val != "" {
			hasHome = true
			out = append(out, entry)
			continue
		}
		if windowsProcessEnv[upper] {
			out = append(out, entry)
		}
	}
	if !hasHome {
		if profile := lookup("USERPROFILE"); profile != "" {
			out = append(out, "HOME="+profile)
		}
	}
	return out
}
