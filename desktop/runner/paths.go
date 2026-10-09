package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Everything this program touches on disk is under one root, and this file is
// the only place that decides what "under" means.
//
// The control plane names directories — a workspace to run in, a folder to
// clone into — and the control plane is trusted, but "trusted" is not the same
// as "infallible", and a bug there must not be able to point a `git clone` at
// the user's home directory or a `claude` session at `/`. One check, in one
// function, with the test beside it.

// pathSegment is what a caller may name as one component: no dots-only
// segments, no separators, no leading dash.
//
// The leading dash matters as much as the traversal does. These names end up as
// arguments to `git`, and an argument that begins with `-` is a FLAG — a
// directory called `--upload-pack=…` is a command, not a folder. `git` offers
// `--` to end its options and this code uses it, but a name that cannot be a
// flag in the first place is one less thing that has to keep being true.
var pathSegment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)

// windowsDeviceName is a segment Windows resolves to a device rather than a
// file — `nul`, `com1.txt` — whatever directory it is in.
var windowsDeviceName = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)

// segmentAllowed is pathSegment, plus what Windows does to a name behind its
// back. There, `repos/nul` is the null device and `repos/acme.` is
// `repos/acme`: a segment that names something other than what it spells is a
// containment check that passed for the wrong file.
func segmentAllowed(part, goos string) bool {
	if !pathSegment.MatchString(part) {
		return false
	}
	if goos == "windows" {
		return !windowsDeviceName.MatchString(part) && !strings.HasSuffix(part, ".")
	}
	return true
}

// resolveInWorkspace turns a caller-supplied relative path into an absolute one
// under root, or refuses it.
//
// Relative only. An absolute path from the caller would have to be checked for
// containment anyway, and accepting one invites the caller to build paths out of
// a root it learned from some earlier call — which is how the root stops being
// the only thing that decides where work happens.
//
// The containment check is on the CLEANED path rather than on the input, so
// `a/../../b` is rejected for what it resolves to and not for how it is spelled.
// Symlinks are deliberately not resolved: the parent directories here are ones
// this program created, and an EvalSymlinks that fails on a path that does not
// exist yet would make "prepare a workspace that is not there" impossible.
func resolveInWorkspace(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("a path is required")
	}
	// Either separator, on every OS: responses carry rel paths slash-separated,
	// and one recorded from an older Windows runner is backslash-separated. A
	// backslash is never part of a segment pathSegment allows, so reading it as
	// a separator widens nothing.
	rel = filepath.FromSlash(strings.ReplaceAll(rel, `\`, "/"))
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%q must be relative to the workspace folder, not absolute", rel)
	}

	parts := strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/")
	for _, part := range parts {
		if !segmentAllowed(part, runtime.GOOS) {
			return "", fmt.Errorf("%q contains a path segment this runner will not use (%q)", rel, part)
		}
	}

	full := filepath.Join(root, filepath.Clean(rel))
	// Belt to the segment check's braces: if the join ever produced something
	// outside root, no amount of reasoning about the segments would matter.
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q resolves outside the workspace folder", rel)
	}
	return full, nil
}
