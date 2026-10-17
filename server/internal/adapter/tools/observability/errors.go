package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	defaultGroupsSince = 7 * 24 * time.Hour
	defaultGroupsLimit = 50
	maxGroupsLimit     = 200
	groupSampleLines   = 15
	groupSampleChars   = 3000
)

type listErrorGroupsTool struct{ kit *ToolKit }

func (t *listErrorGroupsTool) Name() string { return listErrorGroupsToolName }

func (t *listErrorGroupsTool) Definition() domain.ToolDefinition {
	return fn(listErrorGroupsToolName, map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"component":     stringSchema(),
			"environment":   environmentSchema(),
			"since":         stringSchema(),
			"limit":         map[string]any{"type": "integer", "minimum": 1, "maximum": maxGroupsLimit},
			"repository_id": stringSchema(),
		},
	})
}

type errorGroupView struct {
	Fingerprint     string                     `json:"fingerprint"`
	Component       string                     `json:"component"`
	Message         string                     `json:"message"`
	Count           int                        `json:"count"`
	FirstSeen       time.Time                  `json:"first_seen"`
	LastSeen        time.Time                  `json:"last_seen"`
	New             bool                       `json:"new"`
	Provider        string                     `json:"provider,omitempty"`
	Source          string                     `json:"source,omitempty"`
	Sample          string                     `json:"sample,omitempty"`
	SampleTruncated bool                       `json:"sample_truncated,omitempty"`
	ExternalURL     string                     `json:"external_url,omitempty"`
	Definition      *domain.ErrorDefinitionRef `json:"definition"`
}

type unavailableView struct {
	Component string `json:"component"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

func (t *listErrorGroupsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args struct {
		Component    string `json:"component"`
		Environment  string `json:"environment"`
		Since        string `json:"since"`
		Limit        int    `json:"limit"`
		RepositoryID string `json:"repository_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	sc, err := t.kit.resolve(ctx, name, args.RepositoryID, args.Component, args.Environment, false)
	if err != nil {
		return toolError(name, err.Error())
	}
	now := time.Now().UTC()
	since, err := parseSince(args.Since, now, defaultGroupsSince)
	if err != nil {
		return toolError(name, err.Error())
	}
	limit := args.Limit
	if limit <= 0 {
		limit = defaultGroupsLimit
	}
	limit = min(limit, maxGroupsLimit)

	all, err := t.kit.Components.ListComponents(ctx, sc.repositoryID)
	if err != nil {
		return toolError(name, err.Error())
	}
	paths := map[uuid.UUID]string{}
	var ids []uuid.UUID
	for _, c := range all {
		if c.Status == domain.ComponentStatusDismissed {
			continue
		}
		paths[c.ID] = c.Path
		if sc.component == nil || sc.component.ID == c.ID {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return toolJSON(name, map[string]any{"environment": sc.environment, "groups": []errorGroupView{}, "count": 0})
	}
	res, err := t.kit.Errors.Errors(ctx, opsquery.ErrorsQuery{Environment: sc.environment, Since: since, ComponentIDs: ids})
	if err != nil {
		return toolError(name, err.Error())
	}
	if t.kit.Definitions != nil {
		res = t.kit.Definitions.Annotate(ctx, res, true)
	}
	views := make([]errorGroupView, 0, min(len(res.Groups), limit))
	for _, g := range res.Groups {
		if len(views) == limit {
			res.Truncated = true
			break
		}
		sample, cut := headLines(g.Sample, groupSampleLines, groupSampleChars)
		views = append(views, errorGroupView{
			Fingerprint: g.Fingerprint, Component: paths[g.ComponentID], Message: g.Message, Count: g.Count,
			FirstSeen: g.FirstSeen, LastSeen: g.LastSeen, New: g.New, Provider: string(g.Provider), Source: g.Source,
			Sample: sample, SampleTruncated: cut, ExternalURL: g.ExternalURL, Definition: g.Definition,
		})
	}
	unavailable := make([]unavailableView, 0, len(res.Unavailable))
	for _, u := range res.Unavailable {
		unavailable = append(unavailable, unavailableView{Component: paths[u.ComponentID], Code: string(u.Code), Message: u.Message})
	}
	return toolJSON(name, map[string]any{
		"environment": sc.environment,
		"since":       since,
		"groups":      views,
		"count":       len(views),
		"truncated":   res.Truncated,
		"unavailable": unavailable,
	})
}

func headLines(s string, n, max int) (string, bool) {
	s = strings.TrimSpace(s)
	cut := false
	if lines := strings.SplitN(s, "\n", n+1); len(lines) > n {
		s = strings.Join(lines[:n], "\n")
		cut = true
	}
	if len(s) > max {
		s = domain.TruncateHead(s, max)
		cut = true
	}
	return s, cut
}

type proposeErrorDefinitionsTool struct{ kit *ToolKit }

func (t *proposeErrorDefinitionsTool) Name() string { return proposeErrorDefinitionsToolName }

func (t *proposeErrorDefinitionsTool) Definition() domain.ToolDefinition {
	definition := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name", "match", "category", "action"},
		"properties": map[string]any{
			"name":      stringSchema(),
			"component": stringSchema(),
			"match": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"fingerprint":    stringSchema(),
					"message_regex":  stringSchema(),
					"exception_type": stringSchema(),
					"endpoint":       stringSchema(),
					"status":         map[string]any{"type": "integer", "minimum": 100, "maximum": 599},
					"attributes":     stringMapSchema(),
				},
			},
			"category": map[string]any{"type": "string", "enum": []string{"bug", "expected", "dependency", "infrastructure", "user_error"}},
			"severity": severitySchema(),
			"action":   map[string]any{"type": "string", "enum": []string{"none", "ignore", "alert", "incident", "task"}},
			"alert_threshold": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"count", "window_seconds"},
				"properties": map[string]any{
					"count":          map[string]any{"type": "integer", "minimum": 1},
					"window_seconds": map[string]any{"type": "integer", "minimum": 60},
				},
			},
			"rationale": stringSchema(),
			"evidence":  map[string]any{"type": "array", "items": stringSchema()},
		},
	}
	return fn(proposeErrorDefinitionsToolName, map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"definitions"},
		"properties": map[string]any{
			"definitions":   map[string]any{"type": "array", "minItems": 1, "items": definition},
			"source":        map[string]any{"type": "string", "enum": []string{"ai", "code"}},
			"repository_id": stringSchema(),
		},
	})
}

type proposedDefinition struct {
	Name           string                      `json:"name"`
	Component      string                      `json:"component"`
	Match          domain.ErrorMatch           `json:"match"`
	Category       string                      `json:"category"`
	Severity       string                      `json:"severity"`
	Action         string                      `json:"action"`
	AlertThreshold *domain.ErrorAlertThreshold `json:"alert_threshold"`
	Rationale      string                      `json:"rationale"`
	Evidence       []string                    `json:"evidence"`
}

type storedView struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Category string    `json:"category"`
	Action   string    `json:"action"`
}

type duplicateView struct {
	Name       string    `json:"name"`
	ExistingID uuid.UUID `json:"existing_id"`
}

type rejectedView struct {
	Name   string            `json:"name"`
	Fields map[string]string `json:"fields"`
}

func (t *proposeErrorDefinitionsTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	name := t.Name()
	var args struct {
		Definitions  []proposedDefinition `json:"definitions"`
		Source       string               `json:"source"`
		RepositoryID string               `json:"repository_id"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return toolError(name, fmt.Sprintf("invalid arguments: %v", err))
	}
	if len(args.Definitions) == 0 {
		return toolError(name, "definitions: required")
	}
	sc, err := t.kit.resolve(ctx, name, args.RepositoryID, "", "", false)
	if err != nil {
		return toolError(name, err.Error())
	}
	source := domain.ErrorDefinitionAI
	switch args.Source {
	case "", string(domain.ErrorDefinitionAI):
		if sc.job != nil && sc.job.Kind == domain.AIJobErrorCatalog {
			source = domain.ErrorDefinitionCode
		}
	case string(domain.ErrorDefinitionCode):
		source = domain.ErrorDefinitionCode
	default:
		return toolError(name, fmt.Sprintf("invalid source %q", args.Source))
	}

	proposals := make([]errordef.Proposal, 0, len(args.Definitions))
	rejected := []rejectedView{}
	keep := make([]int, 0, len(args.Definitions))
	for i, d := range args.Definitions {
		compID, err := t.componentID(ctx, sc, d.Component)
		if err != nil {
			rejected = append(rejected, rejectedView{Name: d.Name, Fields: map[string]string{"component": err.Error()}})
			continue
		}
		req := errordef.DefinitionRequest{
			ComponentID: &compID,
			Name:        &args.Definitions[i].Name,
			Match:       &args.Definitions[i].Match,
			Category:    &args.Definitions[i].Category,
			Severity:    &args.Definitions[i].Severity,
			Action:      &args.Definitions[i].Action,
			Rationale:   &args.Definitions[i].Rationale,
			Evidence:    &args.Definitions[i].Evidence,
		}
		if d.AlertThreshold != nil {
			req.AlertThreshold = domain.SetNullable(*d.AlertThreshold)
		}
		proposals = append(proposals, errordef.Proposal{DefinitionRequest: req, Source: source})
		keep = append(keep, i)
	}
	outcomes, err := t.kit.Definitions.Propose(ctx, sc.repositoryID, proposals)
	if err != nil {
		return toolError(name, err.Error())
	}
	stored := []storedView{}
	duplicates := []duplicateView{}
	for i, o := range outcomes {
		rationale := args.Definitions[keep[i]].Rationale
		switch {
		case o.Stored != nil:
			stored = append(stored, storedView{ID: o.Stored.ID, Name: o.Stored.Name, Category: string(o.Stored.Category), Action: string(o.Stored.Action)})
			t.kit.record(ctx, domain.AIJobItem{Kind: domain.AIJobItemErrorDefinition, ID: o.Stored.ID, Name: o.Stored.Name, Rationale: rationale})
		case o.Existing != nil:
			duplicates = append(duplicates, duplicateView{Name: o.Name, ExistingID: *o.Existing})
		default:
			rejected = append(rejected, rejectedView{Name: o.Name, Fields: o.Fields})
		}
	}
	return toolJSON(name, map[string]any{"stored": stored, "duplicates": duplicates, "rejected": rejected})
}

// componentID is the definition's component as the request's component_id:
// "" for repository-wide when neither the definition nor the job names one.
func (t *proposeErrorDefinitionsTool) componentID(ctx context.Context, sc scope, path string) (string, error) {
	comp, err := t.kit.component(ctx, t.Name(), sc, path, false)
	if err != nil {
		return "", err
	}
	if comp == nil {
		return "", nil
	}
	return comp.ID.String(), nil
}
