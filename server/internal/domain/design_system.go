package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type DesignSystemScope string

const (
	DesignSystemScopeProject    DesignSystemScope = "project"
	DesignSystemScopeRepository DesignSystemScope = "repository"
)

func ValidDesignSystemScope(s DesignSystemScope) bool {
	return s == DesignSystemScopeProject || s == DesignSystemScopeRepository
}

type DesignSystemStatus string

const (
	DesignSystemInReview   DesignSystemStatus = "in_review"
	DesignSystemApproved   DesignSystemStatus = "approved"
	DesignSystemSuperseded DesignSystemStatus = "superseded"
)

var (
	ErrDesignSystemNotFound     = errors.New("design system not found")
	ErrDesignSystemInvalid      = errors.New("invalid design system")
	ErrDesignSystemNoRepository = errors.New("the project has no repository to run the design task in")
)

// DesignSystem is one version of a project's base design system or of a
// repository's layer on top of it. Tokens is a DTCG token tree: groups are
// objects, a token is an object carrying "$value".
type DesignSystem struct {
	ID            uuid.UUID          `json:"id"`
	Scope         DesignSystemScope  `json:"scope"`
	ProjectID     *uuid.UUID         `json:"project_id,omitempty"`
	RepositoryID  *uuid.UUID         `json:"repository_id,omitempty"`
	Version       int                `json:"version"`
	Status        DesignSystemStatus `json:"status"`
	DesignMD      string             `json:"design_md"`
	Tokens        json.RawMessage    `json:"tokens"`
	InventoryMD   string             `json:"inventory_md"`
	Rationale     string             `json:"rationale"`
	SourceTaskID  *uuid.UUID         `json:"source_task_id,omitempty"`
	SourceTaskKey string             `json:"source_task_key,omitempty"`
	// SourceTaskRepositoryID is read from the task, so the UI can open its
	// review page.
	SourceTaskRepositoryID *uuid.UUID `json:"source_task_repository_id,omitempty"`
	CreatedBy              string     `json:"created_by"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	ApprovedAt             *time.Time `json:"approved_at,omitempty"`
	// Lint is computed on read, never stored.
	Lint []DesignLintFinding `json:"lint,omitempty"`
}

// TargetID is the project id for a base and the repository id for a layer.
func (d DesignSystem) TargetID() uuid.UUID {
	if d.Scope == DesignSystemScopeProject && d.ProjectID != nil {
		return *d.ProjectID
	}
	if d.RepositoryID != nil {
		return *d.RepositoryID
	}
	return uuid.Nil
}

type DesignSystemRequest struct {
	ID           uuid.UUID         `json:"id"`
	Scope        DesignSystemScope `json:"scope"`
	ProjectID    *uuid.UUID        `json:"project_id,omitempty"`
	RepositoryID *uuid.UUID        `json:"repository_id,omitempty"`
	TaskID       uuid.UUID         `json:"task_id"`
	// TaskRepositoryID is the repository the design task lives in.
	TaskRepositoryID uuid.UUID `json:"task_repository_id"`
	CreatedAt        time.Time `json:"created_at"`
}

// DesignSystemProposal is what a design task submits for one target: a new
// base for a project or a new layer for a repository.
type DesignSystemProposal struct {
	Scope        DesignSystemScope
	ProjectID    *uuid.UUID
	RepositoryID *uuid.UUID
	DesignMD     string
	Tokens       json.RawMessage
	InventoryMD  string
	Rationale    string
	TaskID       uuid.UUID
	TaskKey      string
	CreatedBy    string
}

// EffectiveDesignSystem is what an agent working in a repository follows: the
// project base it builds on, the repository's own layer, and the two merged.
type EffectiveDesignSystem struct {
	RepositoryID uuid.UUID          `json:"repository_id"`
	Project      *InitiativeProject `json:"project,omitempty"`
	Base         *DesignSystem      `json:"base,omitempty"`
	Layer        *DesignSystem      `json:"layer,omitempty"`
	Tokens       json.RawMessage    `json:"tokens"`
	// Ambiguous: the repository belongs to several projects with an approved
	// base and none was chosen, so no base applies until one is.
	Ambiguous bool `json:"ambiguous,omitempty"`
}

func (e EffectiveDesignSystem) Empty() bool {
	return e.Base == nil && e.Layer == nil
}

const designTokenValueKey = "$value"

// ParseDesignTokens checks raw is a JSON object and returns it compacted;
// empty input is the empty tree.
func ParseDesignTokens(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return json.RawMessage(`{}`), nil
	}
	var tree map[string]any
	if err := json.Unmarshal(trimmed, &tree); err != nil {
		return nil, fmt.Errorf("%w: tokens must be a JSON object (a DTCG token tree): %v", ErrDesignSystemInvalid, err)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDesignSystemInvalid, err)
	}
	return buf.Bytes(), nil
}

// CheckLayerTokens refuses a repository layer that tries to delete a token
// with null: a layer adds and overrides, so every repository keeps the base's
// semantic names.
func CheckLayerTokens(raw json.RawMessage) error {
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return fmt.Errorf("%w: %v", ErrDesignSystemInvalid, err)
	}
	if path := firstNullPath(tree, ""); path != "" {
		return fmt.Errorf("%w: a repository layer cannot remove %q — it can only add tokens or change their values", ErrDesignSystemInvalid, path)
	}
	return nil
}

func firstNullPath(node map[string]any, prefix string) string {
	keys := make([]string, 0, len(node))
	for k := range node {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch v := node[k].(type) {
		case nil:
			return path
		case map[string]any:
			if p := firstNullPath(v, path); p != "" {
				return p
			}
		}
	}
	return ""
}

// MergeDesignTokens lays layer over base: a token (an object with "$value")
// in the layer replaces the base's token whole, a group merges key by key.
func MergeDesignTokens(base, layer json.RawMessage) (json.RawMessage, error) {
	b, err := tokenTree(base)
	if err != nil {
		return nil, err
	}
	l, err := tokenTree(layer)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(mergeTokenGroups(b, l))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func tokenTree(raw json.RawMessage) (map[string]any, error) {
	tree := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return tree, nil
	}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDesignSystemInvalid, err)
	}
	if tree == nil {
		tree = map[string]any{}
	}
	return tree, nil
}

func mergeTokenGroups(base, layer map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(layer))
	for k, v := range base {
		out[k] = v
	}
	for k, lv := range layer {
		lg, layerIsGroup := lv.(map[string]any)
		bg, baseIsGroup := out[k].(map[string]any)
		if layerIsGroup && baseIsGroup && !isDesignToken(lg) && !isDesignToken(bg) {
			out[k] = mergeTokenGroups(bg, lg)
			continue
		}
		out[k] = lv
	}
	return out
}

func isDesignToken(node map[string]any) bool {
	_, ok := node[designTokenValueKey]
	return ok
}

// OverriddenTokenPaths lists the token paths a layer sets that the base also
// defines, so the UI can show what a repository changes.
func OverriddenTokenPaths(base, layer json.RawMessage) ([]string, error) {
	b, err := tokenTree(base)
	if err != nil {
		return nil, err
	}
	l, err := tokenTree(layer)
	if err != nil {
		return nil, err
	}
	var paths []string
	collectOverrides(b, l, "", &paths)
	sort.Strings(paths)
	return paths, nil
}

func collectOverrides(base, layer map[string]any, prefix string, out *[]string) {
	for k, lv := range layer {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		bv, ok := base[k]
		if !ok {
			continue
		}
		lg, layerIsGroup := lv.(map[string]any)
		bg, baseIsGroup := bv.(map[string]any)
		if layerIsGroup && baseIsGroup && !isDesignToken(lg) && !isDesignToken(bg) {
			collectOverrides(bg, lg, path, out)
			continue
		}
		*out = append(*out, path)
	}
}

// DesignSystemLabel names a version for people: "v3".
func DesignSystemLabel(d *DesignSystem) string {
	if d == nil {
		return ""
	}
	return "v" + fmt.Sprint(d.Version)
}

func TrimDesignText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}
