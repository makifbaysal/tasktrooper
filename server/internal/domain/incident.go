package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrIncidentNotFound signals that no incident matches the id/filter.
var ErrIncidentNotFound = errors.New("incident not found")

// IncidentSeverity orders how loudly an incident interrupts; only high and
// critical open a remediation task on their own.
type IncidentSeverity string

const (
	IncidentSeverityCritical IncidentSeverity = "critical"
	IncidentSeverityHigh     IncidentSeverity = "high"
	IncidentSeverityMedium   IncidentSeverity = "medium"
	IncidentSeverityLow      IncidentSeverity = "low"
)

// Rank returns a comparable weight (higher = worse), used for auto-triage
// thresholds and list sorting.
func (s IncidentSeverity) Rank() int {
	switch s {
	case IncidentSeverityCritical:
		return 4
	case IncidentSeverityHigh:
		return 3
	case IncidentSeverityMedium:
		return 2
	case IncidentSeverityLow:
		return 1
	}
	return 0
}

// NormalizeSeverity maps the many spellings alerting systems use (P1, error,
// fatal, …) onto the four levels; unknown input becomes medium so an alert is
// never silently downgraded to noise.
func NormalizeSeverity(raw string) IncidentSeverity {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "critical", "crit", "fatal", "p1", "sev1", "page", "emergency", "down":
		return IncidentSeverityCritical
	case "high", "error", "major", "p2", "sev2":
		return IncidentSeverityHigh
	case "medium", "warning", "warn", "moderate", "p3", "sev3":
		return IncidentSeverityMedium
	case "low", "info", "informational", "minor", "p4", "sev4", "debug":
		return IncidentSeverityLow
	}
	return IncidentSeverityMedium
}

// IncidentStatus is the incident's position in the triage → remedy → fix loop.
type IncidentStatus string

const (
	IncidentStatusOpen     IncidentStatus = "open"
	IncidentStatusTriaging IncidentStatus = "triaging"
	IncidentStatusProposed IncidentStatus = "proposed"
	IncidentStatusFixing   IncidentStatus = "fixing"
	IncidentStatusResolved IncidentStatus = "resolved"
	IncidentStatusIgnored  IncidentStatus = "ignored"
)

// ValidIncidentStatus reports whether a value is one of the six statuses.
func ValidIncidentStatus(s IncidentStatus) bool {
	switch s {
	case IncidentStatusOpen, IncidentStatusTriaging, IncidentStatusProposed,
		IncidentStatusFixing, IncidentStatusResolved, IncidentStatusIgnored:
		return true
	}
	return false
}

// IsTerminal reports whether the incident no longer participates in dedupe: a
// recurrence after this point is a genuinely new incident.
func (s IncidentStatus) IsTerminal() bool {
	return s == IncidentStatusResolved || s == IncidentStatusIgnored
}

// IncidentSource records how the incident reached us.
const (
	IncidentSourceWebhook = "webhook" // external alerting (Alertmanager, Sentry, Cloud Monitoring, generic JSON)
	IncidentSourceProbe   = "probe"   // our own health probe against a deploy target
	IncidentSourceDeploy  = "deploy"  // a deploy pipeline that failed
	IncidentSourceManual  = "manual"  // opened by a human or an agent
)

// IncidentPolicy is the per-repository answer to "what happens when production
// breaks": stay quiet, propose a fix, or let the board fix it.
type IncidentPolicy string

const (
	IncidentPolicyOff     IncidentPolicy = "off"
	IncidentPolicySuggest IncidentPolicy = "suggest"
	IncidentPolicyAutoFix IncidentPolicy = "auto_fix"
)

func ValidIncidentPolicy(p IncidentPolicy) bool {
	switch p {
	case IncidentPolicyOff, IncidentPolicySuggest, IncidentPolicyAutoFix:
		return true
	}
	return false
}

// Incident is one deduplicated production problem.
type Incident struct {
	ID           uuid.UUID `json:"id"`
	RepositoryID uuid.UUID `json:"repository_id"`
	Env          string    `json:"env"`
	Source       string    `json:"source"`
	// Fingerprint dedupes recurrences: the same fingerprint in the same
	// (repository, env) folds into an existing non-terminal incident instead of
	// opening a second task for the same outage.
	Fingerprint string           `json:"fingerprint"`
	Title       string           `json:"title"`
	Detail      string           `json:"detail,omitempty"`
	Severity    IncidentSeverity `json:"severity"`
	Status      IncidentStatus   `json:"status"`
	// Payload is the raw alert as received, so an agent can read fields the
	// normalizer did not model.
	Payload map[string]any `json:"payload,omitempty"`
	// Remedy is the current proposal (rules-derived first, agent-written once
	// it has diagnosed).
	Remedy     string `json:"remedy,omitempty"`
	RemedyKind string `json:"remedy_kind,omitempty"`
	// RemedyAuthor says who produced the current proposal — one of the
	// RemedyAuthor* values; empty means unknown.
	RemedyAuthor string `json:"remedy_author,omitempty"`
	Confidence   int    `json:"confidence"`
	Occurrences  int    `json:"occurrences"`
	// TaskID links the board task that carries the diagnosis/fix.
	TaskID      *uuid.UUID `json:"task_id,omitempty"`
	FirstSeenAt time.Time  `json:"first_seen_at"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	// Events is the timeline; only filled by Get.
	Events []IncidentEvent `json:"events,omitempty"`
}

// IncidentPayloadComponentID is the payload key an incident names the
// component it is about under; an alert rule's incident always does.
const IncidentPayloadComponentID = "component_id"

// ComponentID is the component the incident's payload names, when it names
// one.
func (i Incident) ComponentID() (uuid.UUID, bool) {
	raw, ok := i.Payload[IncidentPayloadComponentID].(string)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, false
	}
	return id, true
}

// Incident event kinds.
const (
	IncidentEventDetected    = "detected"
	IncidentEventRecurred    = "recurred"
	IncidentEventTriaged     = "triaged"
	IncidentEventProposed    = "proposed"
	IncidentEventTaskCreated = "task_created"
	IncidentEventNotified    = "notified"
	IncidentEventResolved    = "resolved"
	IncidentEventIgnored     = "ignored"
	IncidentEventNote        = "note"
)

// IncidentEvent is one entry of the incident timeline.
type IncidentEvent struct {
	ID         uuid.UUID `json:"id"`
	IncidentID uuid.UUID `json:"incident_id"`
	Kind       string    `json:"kind"`
	Message    string    `json:"message"`
	CreatedAt  time.Time `json:"created_at"`
}

// IncidentInput is a normalized alert, ready to be ingested — what every
// webhook shape and the health probe both collapse to.
type IncidentInput struct {
	RepositoryID uuid.UUID        `json:"repository_id"`
	Env          string           `json:"env"`
	Source       string           `json:"source"`
	Fingerprint  string           `json:"fingerprint"`
	Title        string           `json:"title"`
	Detail       string           `json:"detail"`
	Severity     IncidentSeverity `json:"severity"`
	Payload      map[string]any   `json:"payload,omitempty"`
	// Resolved marks a recovery signal (Alertmanager "resolved", a probe that
	// went green again): it closes the matching incident instead of opening one.
	Resolved bool `json:"resolved"`
}

// IncidentFilter narrows incident listings.
type IncidentFilter struct {
	RepositoryID *uuid.UUID
	Env          string
	Statuses     []IncidentStatus
	// Source narrows to one incident source (alert_rule, webhook, …).
	Source string
	Limit  int
}

// LiveIncidentStatuses are the statuses an incident still needs someone in.
var LiveIncidentStatuses = []IncidentStatus{
	IncidentStatusOpen, IncidentStatusTriaging, IncidentStatusProposed, IncidentStatusFixing,
}

// Remedy kinds — the shape of the fix, not the fix itself. Kind drives who is
// asked to act and whether a rollback is offered.
const (
	RemedyKindRollback   = "rollback"
	RemedyKindCodeFix    = "code_fix"
	RemedyKindConfig     = "config"
	RemedyKindDependency = "dependency"
	RemedyKindCapacity   = "capacity"
	RemedyKindUnknown    = "unknown"
)

// Remedy authorship — who wrote the proposal currently on the incident: the
// distinction lets a recurrence tell the rules engine's own earlier output
// apart from a diagnosis somebody actually reached, so machine triage never
// clobbers a human's write.
const (
	// RemedyAuthorAutoTriage is the rules-engine first pass at ingest: a
	// hypothesis, not a conclusion.
	RemedyAuthorAutoTriage = "auto_triage"
	RemedyAuthorAgent      = "agent"
	RemedyAuthorHuman      = "human"
)

// Remedy is what the suggestion engine produces: a named hypothesis with
// concrete steps.
type Remedy struct {
	Kind    string   `json:"kind"`
	Summary string   `json:"summary"`
	Steps   []string `json:"steps,omitempty"`
	// Confidence is 0-100; auto-fix only engages above a high threshold.
	Confidence int `json:"confidence"`
	// Rollback marks the remedy as "redeploy the last good ref", which the
	// engine can execute rather than merely describe.
	Rollback bool `json:"rollback"`
	// Evidence lists the facts the hypothesis rests on (recent deploy, prior
	// occurrence, probe history).
	Evidence []string `json:"evidence,omitempty"`
}

// Text renders the remedy as the markdown block posted to the board/incident.
func (r Remedy) Text() string {
	var b strings.Builder
	b.WriteString(r.Summary)
	if len(r.Steps) > 0 {
		b.WriteString("\n\nSteps:")
		for _, s := range r.Steps {
			b.WriteString("\n- " + s)
		}
	}
	if len(r.Evidence) > 0 {
		b.WriteString("\n\nEvidence:")
		for _, e := range r.Evidence {
			b.WriteString("\n- " + e)
		}
	}
	return b.String()
}

// IncidentFingerprint derives a stable dedupe key from the parts an alert
// identifies itself by; empty parts are skipped so a missing field does not
// create a second identity for the same alert.
func IncidentFingerprint(parts ...string) string {
	var kept []string
	for _, p := range parts {
		p = strings.ToLower(strings.Join(strings.Fields(p), " "))
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(kept, "|")))
	return hex.EncodeToString(sum[:8])
}
