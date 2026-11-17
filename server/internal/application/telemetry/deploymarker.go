package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// markTimeout bounds one marker, which runs off the release's own path.
const markTimeout = 20 * time.Second

// MarkerComponents and MarkerRepos are the slices of the project model and
// the repository service a deploy marker names its service from.
type MarkerComponents interface {
	GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error)
}

type MarkerRepos interface {
	Get(ctx context.Context, id uuid.UUID) (domain.Repository, error)
}

// DeployMarker tells the connected telemetry that a release went live, so
// its charts and incident correlation show the deploy. It is the release
// service's DeployMarker.
type DeployMarker struct {
	svc        *Service
	components MarkerComponents
	repos      MarkerRepos
}

func NewDeployMarker(svc *Service, components MarkerComponents, repos MarkerRepos) *DeployMarker {
	return &DeployMarker{svc: svc, components: components, repos: repos}
}

// DeployInput is the telemetry API's DeployInput.
type DeployInput struct {
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Version     string `json:"version"`
	CommitSHA   string `json:"commitSha,omitempty"`
	Message     string `json:"message,omitempty"`
	TS          int64  `json:"ts,omitempty"`
}

// MarkDeployed posts the marker for a release whose deploy just succeeded.
// It is best effort: no telemetry, no version or any failure is logged and
// dropped, never returned, because a marker must not hold up a release.
func (m *DeployMarker) MarkDeployed(ctx context.Context, r domain.Release) {
	if m == nil || m.svc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, markTimeout)
	defer cancel()
	if ok, err := m.svc.Configured(ctx); err != nil || !ok {
		return
	}
	in, ok := m.input(ctx, r)
	if !ok {
		return
	}
	logger := log.With().Str("release_id", r.ID.String()).Str("service", in.Service).Str("version", in.Version).Logger()
	resp, err := m.svc.Write(ctx, http.MethodPost, "deploys", nil, in)
	switch {
	case err != nil:
		logger.Warn().Err(err).Msg("telemetry: deploy marker not sent")
	case resp.Status >= 400:
		logger.Warn().Int("status", resp.Status).Msg("telemetry: deploy marker refused")
	default:
		logger.Debug().Msg("telemetry: deploy marker sent")
	}
}

func (m *DeployMarker) input(ctx context.Context, r domain.Release) (DeployInput, bool) {
	version := strings.TrimSpace(r.CommitSHA)
	if version == "" {
		version = strings.TrimSpace(r.Version)
	}
	if version == "" {
		return DeployInput{}, false
	}
	service := m.serviceName(ctx, r)
	if service == "" {
		return DeployInput{}, false
	}
	in := DeployInput{
		Service:     service,
		Environment: string(domain.EnvironmentProduction),
		Version:     version,
		CommitSHA:   strings.TrimSpace(r.CommitSHA),
		Message:     markerMessage(r),
	}
	if r.DeployedAt != nil {
		in.TS = r.DeployedAt.UnixMilli()
	}
	return in, true
}

func markerMessage(r domain.Release) string {
	keys := make([]string, 0, len(r.Tasks))
	for _, t := range r.Tasks {
		if k := strings.TrimSpace(t.Key); k != "" {
			keys = append(keys, k)
		}
	}
	msg := "TaskTrooper release"
	if v := strings.TrimSpace(r.Version); v != "" {
		msg += " " + v
	}
	if len(keys) > 0 {
		msg += ": " + strings.Join(keys, ", ")
	}
	return msg
}

// serviceName maps a release's component to the service telemetry knows it
// by. Telemetry names services after what the SDK was configured with, and
// TaskTrooper keeps no mapping of its own, so the names a component goes by
// (its name, its folder, then the repository's name and checkout folder) are
// matched against the services telemetry reports, the same loose match
// PickRepository routes alerts back with. With no match the first candidate
// is used: a marker under the component's own name is still findable.
func (m *DeployMarker) serviceName(ctx context.Context, r domain.Release) string {
	candidates := m.candidates(ctx, r)
	if len(candidates) == 0 {
		return ""
	}
	if known := m.knownServices(ctx); len(known) > 0 {
		for _, c := range candidates {
			want := slug(c)
			for _, s := range known {
				if slug(s) == want {
					return s
				}
			}
		}
	}
	return candidates[0]
}

func (m *DeployMarker) candidates(ctx context.Context, r domain.Release) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || s == "." || slug(s) == "" {
			return
		}
		for _, have := range out {
			if have == s {
				return
			}
		}
		out = append(out, s)
	}
	if r.ComponentID != nil && m.components != nil {
		if c, err := m.components.GetComponent(ctx, *r.ComponentID); err == nil {
			add(c.Name.Get())
			if c.Path != "" && c.Path != "." {
				add(path.Base(c.Path))
			}
		}
	}
	if m.repos != nil {
		if repo, err := m.repos.Get(ctx, r.RepositoryID); err == nil {
			add(repo.Name)
			if repo.RootPath != "" {
				add(path.Base(filepath.ToSlash(repo.RootPath)))
			}
		}
	}
	return out
}

func (m *DeployMarker) knownServices(ctx context.Context) []string {
	resp, err := m.svc.Get(ctx, "services", url.Values{"range": {"7d"}})
	if err != nil || resp.Status >= 400 {
		return nil
	}
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(resp.Body, &list); err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s.Name != "" {
			out = append(out, s.Name)
		}
	}
	return out
}

