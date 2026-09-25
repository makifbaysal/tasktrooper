package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReleaseLocalRun is a batch release's build-and-publish command run on this
// machine.
type ReleaseLocalRun struct {
	Argv       []string   `json:"argv"`
	LogPath    string     `json:"log_path,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	Tail       string     `json:"tail,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// ReleaseStoreBuild tracks one platform's store build of a batch release: the
// deploy is done when the store's internal channel shows a build other than
// the one it had before.
type ReleaseStoreBuild struct {
	Platform      string `json:"platform"`
	Engine        string `json:"engine,omitempty"`
	BaselineBuild string `json:"baseline_build,omitempty"`
	Build         string `json:"build,omitempty"`
	Error         string `json:"error,omitempty"`
}

type ReleaseCutPreview struct {
	SuggestedVersion string           `json:"suggested_version"`
	PreviousVersion  string           `json:"previous_version,omitempty"`
	Tag              string           `json:"tag,omitempty"`
	CommitSHA        string           `json:"commit_sha"`
	Notes            string           `json:"notes"`
	Tasks            []ReleaseTaskRef `json:"tasks"`
}

type ReleaseCutRequest struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

var (
	// ErrReleaseTagExists: a batch release never re-uses a tag — a version
	// that already shipped must not be silently published again.
	ErrReleaseTagExists = errors.New("the release tag already exists")
	ErrReleaseEmpty     = errors.New("the draft release carries no tasks")
	ErrInvalidVersion   = errors.New("invalid release version")
)

const DefaultReleaseTagPattern = "v{version}"

func ReleaseTag(pattern, version string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = DefaultReleaseTagPattern
	}
	return strings.ReplaceAll(pattern, "{version}", strings.TrimSpace(version))
}

func ValidReleaseVersion(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("%w: the version is empty", ErrInvalidVersion)
	}
	if len(v) > 64 {
		return fmt.Errorf("%w: at most 64 characters", ErrInvalidVersion)
	}
	if strings.HasPrefix(v, "-") || strings.HasPrefix(v, ".") {
		return fmt.Errorf("%w: %q cannot start with '-' or '.'", ErrInvalidVersion, v)
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '+', r == '-', r == '_':
		default:
			return fmt.Errorf("%w: %q may only contain letters, digits and . + - _", ErrInvalidVersion, v)
		}
	}
	return nil
}
