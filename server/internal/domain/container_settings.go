package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ContainerSettings is how a component is containerised. It is entered by a
// person; a scan never fills it in.
type ContainerSettings struct {
	// Enabled is the "has containers" switch: it is what makes the container
	// command purposes appear on the component.
	Enabled bool `json:"enabled,omitempty"`
	// Registry is the image repository the component's image is pushed to,
	// e.g. "ghcr.io/acme/api".
	Registry string `json:"registry,omitempty"`
	// Platforms are the buildx targets, e.g. "linux/amd64", "linux/arm64".
	Platforms []string `json:"platforms,omitempty"`
	// TagPattern names the image tag; "{sha}" and "{version}" are filled in by
	// whoever runs the build. Empty means no tag has been decided.
	TagPattern string `json:"tag_pattern,omitempty"`
	// ContainerOnly tells agents to run the component's tooling inside its
	// containers rather than on the host.
	ContainerOnly bool `json:"container_only,omitempty"`
}

var ErrInvalidContainerSettings = errors.New("invalid container settings")

var (
	platformPattern = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+(/[a-z0-9]+)?$`)
	registryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	tagPattern      = regexp.MustCompile(`^[A-Za-z0-9_.{}-]+$`)
)

// Normalized trims the fields, drops duplicate platforms and refuses values
// that cannot be a registry, a platform or a tag.
func (s ContainerSettings) Normalized() (ContainerSettings, error) {
	s.Registry = strings.TrimSpace(s.Registry)
	s.TagPattern = strings.TrimSpace(s.TagPattern)
	if s.Registry != "" && !registryPattern.MatchString(s.Registry) {
		return ContainerSettings{}, fmt.Errorf("%w: registry %q", ErrInvalidContainerSettings, s.Registry)
	}
	if s.TagPattern != "" && !tagPattern.MatchString(s.TagPattern) {
		return ContainerSettings{}, fmt.Errorf("%w: tag pattern %q", ErrInvalidContainerSettings, s.TagPattern)
	}
	var platforms []string
	seen := map[string]bool{}
	for _, p := range s.Platforms {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		if !platformPattern.MatchString(p) {
			return ContainerSettings{}, fmt.Errorf("%w: platform %q", ErrInvalidContainerSettings, p)
		}
		seen[p] = true
		platforms = append(platforms, p)
	}
	s.Platforms = platforms
	return s, nil
}

func (s ContainerSettings) IsZero() bool {
	return !s.Enabled && s.Registry == "" && len(s.Platforms) == 0 && s.TagPattern == "" && !s.ContainerOnly
}
