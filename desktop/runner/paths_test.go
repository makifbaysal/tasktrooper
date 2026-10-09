package main

import (
	"path/filepath"
	"testing"
)

// On Windows a name can resolve to something other than what it spells: a
// device (`nul`, `com1.txt`), or — with a trailing dot stripped — a sibling.
// segmentAllowed takes the OS as an argument so both answers are tested on
// every machine.
func TestSegmentAllowedRefusesWhatWindowsRenames(t *testing.T) {
	refusedOnWindows := []string{
		"con", "CON", "prn", "aux", "nul", "Nul",
		"com0", "com1", "COM9", "lpt1", "LPT9",
		"nul.txt", "con.tar.gz", "Com1.json",
		"acme.", "acme..",
	}
	for _, segment := range refusedOnWindows {
		if segmentAllowed(segment, "windows") {
			t.Errorf("segmentAllowed(%q, windows) = true; Windows does not open the file that name spells", segment)
		}
		if !segmentAllowed(segment, "darwin") || !segmentAllowed(segment, "linux") {
			t.Errorf("segmentAllowed(%q) refused it off Windows, where it is an ordinary name", segment)
		}
	}

	allowedEverywhere := []string{
		"console", "nullable", "auxiliary", "com10", "lpt", "printer",
		"acme-api", "acme.api_v2", "con_", "x.con",
	}
	for _, segment := range allowedEverywhere {
		for _, goos := range []string{"windows", "darwin", "linux"} {
			if !segmentAllowed(segment, goos) {
				t.Errorf("segmentAllowed(%q, %s) = false, want true", segment, goos)
			}
		}
	}

	for _, segment := range []string{"-x", "..", ".", "a b", "a\\b", ""} {
		for _, goos := range []string{"windows", "darwin"} {
			if segmentAllowed(segment, goos) {
				t.Errorf("segmentAllowed(%q, %s) = true; pathSegment refuses it on every OS", segment, goos)
			}
		}
	}
}

// Responses carry rel paths slash-separated, and one recorded from a Windows
// runner before that was backslash-separated. Both name the same directory on
// every OS, and neither separator opens a way out of the root.
func TestResolveInWorkspaceAcceptsEitherSeparator(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "repos", "acme-api")
	for _, in := range []string{"repos/acme-api", `repos\acme-api`} {
		got, err := resolveInWorkspace(root, in)
		if err != nil {
			t.Fatalf("resolveInWorkspace(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("resolveInWorkspace(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{`repos\..\..\secrets`, `\etc`, `..\secrets`} {
		if got, err := resolveInWorkspace(root, in); err == nil {
			t.Fatalf("resolveInWorkspace(%q) = %q, want a refusal", in, got)
		}
	}
}
