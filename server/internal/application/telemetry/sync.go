package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SyncInterval is how often telemetry's incidents are looked at. A local
// install cannot receive a webhook from a hosted telemetry, so it polls.
const SyncInterval = time.Minute

const (
	syncFirstRunDelay = 20 * time.Second
	openPageSize      = 200
	knownPageSize     = 500
)

// Incidents is prodops.Service: where a telemetry alert becomes a TaskTrooper
// incident with a remediation task for the release engineer.
type Incidents interface {
	Ingest(ctx context.Context, in domain.IncidentInput) (domain.Incident, error)
	List(ctx context.Context, filter domain.IncidentFilter) ([]domain.Incident, error)
}

// Repositories lists the repositories an alert's service can belong to.
type Repositories interface {
	List(ctx context.Context) ([]domain.Repository, error)
}

// IncidentSync turns incidents opened in telemetry into prodops incidents and
// closes them again when telemetry resolves them.
type IncidentSync struct {
	svc       *Service
	incidents Incidents
	repos     Repositories
	mu        sync.Mutex
}

func NewIncidentSync(svc *Service, incidents Incidents, repos Repositories) *IncidentSync {
	return &IncidentSync{svc: svc, incidents: incidents, repos: repos}
}

// Start launches the poller; it exits with ctx. sweep runs one pass, and is
// where a multi-tenant deployment fans out per tenant.
func (s *IncidentSync) Start(ctx context.Context, interval time.Duration, sweep func(ctx context.Context, pass func(context.Context))) {
	if s == nil || s.svc == nil || s.incidents == nil || s.repos == nil {
		return
	}
	if interval <= 0 {
		interval = SyncInterval
	}
	if sweep == nil {
		sweep = func(ctx context.Context, pass func(context.Context)) { pass(ctx) }
	}
	go func() {
		t := time.NewTimer(syncFirstRunDelay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sweep(ctx, s.RunOnce)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweep(ctx, s.RunOnce)
			}
		}
	}()
}

// flexID reads an id sent as a JSON string or a number.
type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexID(n.String())
	return nil
}

func (f flexID) String() string { return string(f) }

type remoteIncident struct {
	ID         flexID          `json:"id"`
	MonitorID  flexID          `json:"monitorId"`
	KpiID      flexID          `json:"kpiId"`
	Title      string          `json:"title"`
	Severity   string          `json:"severity"`
	State      string          `json:"state"`
	StartedAt  int64           `json:"startedAt"`
	ResolvedAt int64           `json:"resolvedAt"`
	Value      json.RawMessage `json:"value"`
	Service    string          `json:"service"`
	Endpoint   string          `json:"endpoint"`
	RootCause  json.RawMessage `json:"rootCause"`
	Quiet      bool            `json:"quiet"`
}

func (r remoteIncident) live() bool { return r.State == "firing" || r.State == "acknowledged" }

// RunOnce is one pass. A telemetry that is unset, down or rejecting the key is
// skipped quietly: the next pass tries again.
func (s *IncidentSync) RunOnce(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, err := s.svc.connection(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("telemetry sync: reading settings failed")
		return
	}
	if !conn.ok() {
		return
	}
	open, err := s.fetchOpen(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("telemetry sync: listing incidents failed")
		return
	}
	known, err := s.knownIncidents(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("telemetry sync: listing known incidents failed")
		return
	}
	scope := baseScope(conn.baseURL)
	openIDs := map[string]bool{}
	var repos []domain.Repository
	reposLoaded := false
	for _, r := range open {
		id := r.ID.String()
		openIDs[id] = true
		if !r.live() || r.Quiet {
			continue
		}
		fp := fingerprint(scope, id)
		if _, seen := known[fp]; seen {
			continue
		}
		if !reposLoaded {
			if repos, err = s.repos.List(ctx); err != nil {
				log.Warn().Err(err).Msg("telemetry sync: listing repositories failed")
				return
			}
			reposLoaded = true
		}
		repo, matched, ok := PickRepository(r.Service, repos)
		if !ok {
			log.Debug().Str("service", r.Service).Msg("telemetry sync: no repository to open the incident in")
			continue
		}
		in := incidentInput(conn.baseURL, scope, r, repo.ID, matched)
		if _, err := s.incidents.Ingest(ctx, in); err != nil {
			log.Warn().Err(err).Str("telemetry_incident", id).Msg("telemetry sync: ingest failed")
		}
	}
	s.closeRecovered(ctx, scope, known, openIDs)
}

func (s *IncidentSync) fetchOpen(ctx context.Context) ([]remoteIncident, error) {
	resp, err := s.svc.Get(ctx, "incidents", url.Values{"state": {"open"}, "limit": {strconv.Itoa(openPageSize)}})
	if err != nil {
		return nil, err
	}
	if resp.Status != 200 {
		return nil, fmt.Errorf("incidents answered %d", resp.Status)
	}
	var out []remoteIncident
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, fmt.Errorf("decode incidents: %w", err)
	}
	return out, nil
}

func (s *IncidentSync) fetchOne(ctx context.Context, id string) (remoteIncident, bool, error) {
	resp, err := s.svc.Get(ctx, "incidents/"+url.PathEscape(id), nil)
	if err != nil {
		return remoteIncident{}, false, err
	}
	if resp.Status == 404 {
		return remoteIncident{}, false, nil
	}
	if resp.Status != 200 {
		return remoteIncident{}, false, fmt.Errorf("incident %s answered %d", id, resp.Status)
	}
	var out remoteIncident
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return remoteIncident{}, false, fmt.Errorf("decode incident: %w", err)
	}
	return out, true, nil
}

func (s *IncidentSync) knownIncidents(ctx context.Context) (map[string]domain.Incident, error) {
	list, err := s.incidents.List(ctx, domain.IncidentFilter{Source: domain.IncidentSourceTelemetry, Limit: knownPageSize})
	if err != nil {
		return nil, err
	}
	out := make(map[string]domain.Incident, len(list))
	for _, inc := range list {
		out[inc.Fingerprint] = inc
	}
	return out, nil
}

// closeRecovered resolves the TaskTrooper incidents whose telemetry incident is
// no longer open. An incident that dropped out of the open list is asked about
// by id, so a list cut at its page size never closes anything by mistake.
func (s *IncidentSync) closeRecovered(ctx context.Context, scope string, known map[string]domain.Incident, openIDs map[string]bool) {
	prefix := fingerprint(scope, "")
	for fp, inc := range known {
		if inc.Status.IsTerminal() || !strings.HasPrefix(fp, prefix) {
			continue
		}
		id := strings.TrimPrefix(fp, prefix)
		if openIDs[id] {
			continue
		}
		r, found, err := s.fetchOne(ctx, id)
		if err != nil {
			log.Debug().Err(err).Str("telemetry_incident", id).Msg("telemetry sync: checking an incident failed")
			continue
		}
		if found && r.State != "resolved" {
			continue
		}
		in := domain.IncidentInput{
			RepositoryID: inc.RepositoryID, Env: inc.Env, Source: domain.IncidentSourceTelemetry,
			Fingerprint: fp, Title: inc.Title, Resolved: true,
		}
		if _, err := s.incidents.Ingest(ctx, in); err != nil {
			log.Warn().Err(err).Str("telemetry_incident", id).Msg("telemetry sync: resolving failed")
		}
	}
}

// baseScope tells two telemetry servers' incident ids apart.
func baseScope(baseURL string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(baseURL)))
	return hex.EncodeToString(sum[:3])
}

func fingerprint(scope, id string) string { return "telemetry:" + scope + ":" + id }

// incidentSeverity: every telemetry alert opens a task, so nothing maps below
// high; critical stays critical.
func incidentSeverity(raw string) domain.IncidentSeverity {
	if strings.EqualFold(strings.TrimSpace(raw), "critical") {
		return domain.IncidentSeverityCritical
	}
	return domain.IncidentSeverityHigh
}

func incidentInput(baseURL, scope string, r remoteIncident, repoID uuid.UUID, matched bool) domain.IncidentInput {
	id := r.ID.String()
	var detail strings.Builder
	if r.Service != "" {
		fmt.Fprintf(&detail, "Service: %s", r.Service)
		if r.Endpoint != "" {
			fmt.Fprintf(&detail, " · %s", r.Endpoint)
		}
		detail.WriteString("\n")
	}
	if v := rawText(r.Value); v != "" {
		fmt.Fprintf(&detail, "Value: %s\n", v)
	}
	if r.StartedAt > 0 {
		fmt.Fprintf(&detail, "Started: %s\n", time.UnixMilli(r.StartedAt).UTC().Format(time.RFC3339))
	}
	if cause := rootCauseText(r.RootCause); cause != "" {
		fmt.Fprintf(&detail, "Root cause (telemetry RCA): %s\n", cause)
	}
	if !matched {
		fmt.Fprintf(&detail, "No repository is named after the service %q; the incident was opened in the first repository.\n", r.Service)
	}
	fmt.Fprintf(&detail, "Telemetry incident: %s (state %s, severity %s)", id, r.State, r.Severity)

	payload := map[string]any{
		"telemetry_incident_id": id,
		"telemetry_base_url":    baseURL,
		"telemetry_state":       r.State,
		"telemetry_severity":    r.Severity,
		"service":               r.Service,
		"endpoint":              r.Endpoint,
		"started_at":            r.StartedAt,
	}
	if r.MonitorID.String() != "" {
		payload["monitor_id"] = r.MonitorID.String()
	}
	if r.KpiID.String() != "" {
		payload["kpi_id"] = r.KpiID.String()
	}
	if len(r.RootCause) > 0 && string(r.RootCause) != "null" {
		var rc any
		if json.Unmarshal(r.RootCause, &rc) == nil {
			payload["root_cause"] = rc
		}
	}
	return domain.IncidentInput{
		RepositoryID: repoID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceTelemetry,
		Fingerprint:  fingerprint(scope, id),
		Title:        strings.TrimSpace(r.Title),
		Detail:       strings.TrimSpace(detail.String()),
		Severity:     incidentSeverity(r.Severity),
		Payload:      payload,
	}
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

// rootCauseText reads telemetry's RCA result: {summary, confidence} or a bare string.
func rootCauseText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var rc struct {
		Summary    string   `json:"summary"`
		Confidence *float64 `json:"confidence"`
	}
	if json.Unmarshal(raw, &rc) == nil && rc.Summary != "" {
		if rc.Confidence != nil {
			return fmt.Sprintf("%s (%d%% confidence)", rc.Summary, int(*rc.Confidence*100+0.5))
		}
		return rc.Summary
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// PickRepository chooses the repository an alert about service belongs to: the
// one named after it (its name, its checkout folder or one of its
// sub-projects' folders), else the first repository, because a missed alert is
// worse than one that needs re-routing; the release engineer files the fix
// where it belongs. ok is false only when there is no repository at all.
func PickRepository(service string, repos []domain.Repository) (repo domain.Repository, matched, ok bool) {
	if len(repos) == 0 {
		return domain.Repository{}, false, false
	}
	if want := slug(service); want != "" {
		for _, r := range repos {
			if slug(r.Name) == want || slug(path.Base(filepath.ToSlash(r.RootPath))) == want {
				return r, true, true
			}
		}
		for _, r := range repos {
			for _, sp := range r.SubProjects {
				if sp.Path != "." && slug(path.Base(sp.Path)) == want {
					return r, true, true
				}
			}
		}
	}
	return repos[0], false, true
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
