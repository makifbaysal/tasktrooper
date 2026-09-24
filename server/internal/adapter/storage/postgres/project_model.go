package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// ProjectModelStore persists the structured project model (migration 153):
// components, their CI checks, the edges between components and system
// resources, human-written notes, and the scans that produce all of it.
type ProjectModelStore struct {
	pool *DB
}

func NewProjectModelStore(pool *DB) *ProjectModelStore {
	return &ProjectModelStore{pool: pool}
}

var _ port.ProjectModelStore = (*ProjectModelStore)(nil)

// pmExecutor is satisfied by both *DB and pgx.Tx, so ApplyReconcile's
// transaction runs the exact same upsert SQL the Save* methods use outside a
// transaction.
type pmExecutor interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func nonNilMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return map[K]V{}
	}
	return m
}

// --- Component ---

const componentCols = `id, repository_id, path, name, role, stack, commands, mobile, docs, gates, status, manually_added, needs_review, last_scan_id, created_at, updated_at`

func scanComponent(row pgx.Row) (domain.Component, error) {
	var c domain.Component
	var nameJSON, roleJSON, stackJSON, commandsJSON, mobileJSON, docsJSON, gatesJSON []byte
	if err := row.Scan(
		&c.ID, &c.RepositoryID, &c.Path, &nameJSON, &roleJSON, &stackJSON, &commandsJSON, &mobileJSON, &docsJSON, &gatesJSON,
		&c.Status, &c.ManuallyAdded, &c.NeedsReview, &c.LastScanID, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return domain.Component{}, err
	}
	if err := json.Unmarshal(nameJSON, &c.Name); err != nil {
		return domain.Component{}, fmt.Errorf("unmarshal component name: %w", err)
	}
	if err := json.Unmarshal(roleJSON, &c.Role); err != nil {
		return domain.Component{}, fmt.Errorf("unmarshal component role: %w", err)
	}
	if err := json.Unmarshal(stackJSON, &c.Stack); err != nil {
		return domain.Component{}, fmt.Errorf("unmarshal component stack: %w", err)
	}
	if err := json.Unmarshal(commandsJSON, &c.Commands); err != nil {
		return domain.Component{}, fmt.Errorf("unmarshal component commands: %w", err)
	}
	if err := json.Unmarshal(docsJSON, &c.Docs); err != nil {
		return domain.Component{}, fmt.Errorf("unmarshal component docs: %w", err)
	}
	if err := json.Unmarshal(gatesJSON, &c.Gates); err != nil {
		return domain.Component{}, fmt.Errorf("unmarshal component gates: %w", err)
	}
	if mobileJSON != nil {
		var mobile domain.Fact[domain.MobileFacts]
		if err := json.Unmarshal(mobileJSON, &mobile); err != nil {
			return domain.Component{}, fmt.Errorf("unmarshal component mobile: %w", err)
		}
		c.Mobile = &mobile
	}
	return c, nil
}

const upsertComponentSQL = `
INSERT INTO project_components
	(id, repository_id, path, name, role, stack, commands, mobile, docs, gates, status, manually_added, needs_review, last_scan_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (id) DO UPDATE SET
	repository_id = EXCLUDED.repository_id,
	path = EXCLUDED.path,
	name = EXCLUDED.name,
	role = EXCLUDED.role,
	stack = EXCLUDED.stack,
	commands = EXCLUDED.commands,
	mobile = EXCLUDED.mobile,
	docs = EXCLUDED.docs,
	gates = EXCLUDED.gates,
	status = EXCLUDED.status,
	manually_added = EXCLUDED.manually_added,
	needs_review = EXCLUDED.needs_review,
	last_scan_id = EXCLUDED.last_scan_id,
	updated_at = now()
RETURNING ` + componentCols

// created_at is deliberately absent from the SET list above: omitting it from
// an ON CONFLICT UPDATE is what keeps the original insert's created_at on an
// upsert, while a genuine INSERT still gets it from the column DEFAULT.
func upsertComponent(ctx context.Context, q pmExecutor, c domain.Component) (domain.Component, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	status := c.Status
	if status == "" {
		status = domain.ComponentStatusActive
	}
	nameJSON, err := json.Marshal(c.Name)
	if err != nil {
		return domain.Component{}, fmt.Errorf("marshal component name: %w", err)
	}
	roleJSON, err := json.Marshal(c.Role)
	if err != nil {
		return domain.Component{}, fmt.Errorf("marshal component role: %w", err)
	}
	stackJSON, err := json.Marshal(c.Stack)
	if err != nil {
		return domain.Component{}, fmt.Errorf("marshal component stack: %w", err)
	}
	commandsJSON, err := json.Marshal(nonNilSlice(c.Commands))
	if err != nil {
		return domain.Component{}, fmt.Errorf("marshal component commands: %w", err)
	}
	docsJSON, err := json.Marshal(c.Docs)
	if err != nil {
		return domain.Component{}, fmt.Errorf("marshal component docs: %w", err)
	}
	gatesJSON, err := json.Marshal(c.Gates)
	if err != nil {
		return domain.Component{}, fmt.Errorf("marshal component gates: %w", err)
	}
	var mobileJSON []byte
	if c.Mobile != nil {
		mobileJSON, err = json.Marshal(c.Mobile)
		if err != nil {
			return domain.Component{}, fmt.Errorf("marshal component mobile: %w", err)
		}
	}
	row := q.QueryRow(ctx, upsertComponentSQL,
		c.ID, c.RepositoryID, c.Path, nameJSON, roleJSON, stackJSON, commandsJSON, mobileJSON, docsJSON, gatesJSON,
		status, c.ManuallyAdded, c.NeedsReview, c.LastScanID)
	out, err := scanComponent(row)
	if err != nil {
		return domain.Component{}, fmt.Errorf("upsert component: %w", err)
	}
	return out, nil
}

func (s *ProjectModelStore) SaveComponent(ctx context.Context, c domain.Component) (domain.Component, error) {
	return upsertComponent(ctx, s.pool, c)
}

func (s *ProjectModelStore) ListComponents(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+componentCols+`
		FROM project_components WHERE repository_id = $1 ORDER BY path`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list components: %w", err)
	}
	defer rows.Close()
	var out []domain.Component
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan component: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ProjectModelStore) ListComponentsForRepositories(ctx context.Context, repositoryIDs []uuid.UUID) ([]domain.Component, error) {
	if len(repositoryIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+componentCols+`
		FROM project_components WHERE repository_id = ANY($1) ORDER BY path`, repositoryIDs)
	if err != nil {
		return nil, fmt.Errorf("list components for repositories: %w", err)
	}
	defer rows.Close()
	var out []domain.Component
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan component: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ProjectModelStore) ListAllComponents(ctx context.Context) ([]domain.Component, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+componentCols+` FROM project_components ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("list all components: %w", err)
	}
	defer rows.Close()
	var out []domain.Component
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan component: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ProjectModelStore) GetComponent(ctx context.Context, id uuid.UUID) (domain.Component, error) {
	c, err := scanComponent(s.pool.QueryRow(ctx, `SELECT `+componentCols+`
		FROM project_components WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Component{}, fmt.Errorf("get component: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.Component{}, fmt.Errorf("get component: %w", err)
	}
	return c, nil
}

// --- ComponentCheck ---

const checkCols = `id, repository_id, component_id, source, workflow, workflow_name, job_key, job_name, purpose, environment, triggers, path_filters, steps, local_commands, gate, dispatchable, status, missing, needs_review, created_at, updated_at`

const checkColsPrefixed = `cc.id, cc.repository_id, cc.component_id, cc.source, cc.workflow, cc.workflow_name, cc.job_key, cc.job_name, cc.purpose, cc.environment, cc.triggers, cc.path_filters, cc.steps, cc.local_commands, cc.gate, cc.dispatchable, cc.status, cc.missing, cc.needs_review, cc.created_at, cc.updated_at`

func scanCheck(row pgx.Row) (domain.ComponentCheck, error) {
	var c domain.ComponentCheck
	var purposeJSON, triggersJSON, pathFiltersJSON, stepsJSON, localCommandsJSON, gateJSON []byte
	if err := row.Scan(
		&c.ID, &c.RepositoryID, &c.ComponentID, &c.Source, &c.Workflow, &c.WorkflowName, &c.JobKey, &c.JobName,
		&purposeJSON, &c.Environment, &triggersJSON, &pathFiltersJSON, &stepsJSON, &localCommandsJSON, &gateJSON,
		&c.Dispatchable, &c.Status, &c.Missing, &c.NeedsReview, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return domain.ComponentCheck{}, err
	}
	if err := json.Unmarshal(purposeJSON, &c.Purpose); err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("unmarshal check purpose: %w", err)
	}
	if err := json.Unmarshal(triggersJSON, &c.Triggers); err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("unmarshal check triggers: %w", err)
	}
	if err := json.Unmarshal(pathFiltersJSON, &c.PathFilters); err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("unmarshal check path filters: %w", err)
	}
	if err := json.Unmarshal(stepsJSON, &c.Steps); err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("unmarshal check steps: %w", err)
	}
	if err := json.Unmarshal(localCommandsJSON, &c.LocalCommands); err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("unmarshal check local commands: %w", err)
	}
	if err := json.Unmarshal(gateJSON, &c.Gate); err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("unmarshal check gate: %w", err)
	}
	return c, nil
}

const upsertCheckSQL = `
INSERT INTO component_checks
	(id, repository_id, component_id, source, workflow, workflow_name, job_key, job_name, purpose, environment, triggers, path_filters, steps, local_commands, gate, dispatchable, status, missing, needs_review)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (id) DO UPDATE SET
	repository_id = EXCLUDED.repository_id,
	component_id = EXCLUDED.component_id,
	source = EXCLUDED.source,
	workflow = EXCLUDED.workflow,
	workflow_name = EXCLUDED.workflow_name,
	job_key = EXCLUDED.job_key,
	job_name = EXCLUDED.job_name,
	purpose = EXCLUDED.purpose,
	environment = EXCLUDED.environment,
	triggers = EXCLUDED.triggers,
	path_filters = EXCLUDED.path_filters,
	steps = EXCLUDED.steps,
	local_commands = EXCLUDED.local_commands,
	gate = EXCLUDED.gate,
	dispatchable = EXCLUDED.dispatchable,
	status = EXCLUDED.status,
	missing = EXCLUDED.missing,
	needs_review = EXCLUDED.needs_review,
	updated_at = now()
RETURNING ` + checkCols

func upsertCheck(ctx context.Context, q pmExecutor, c domain.ComponentCheck) (domain.ComponentCheck, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	source := c.Source
	if source == "" {
		source = domain.CheckSourceCI
	}
	status := c.Status
	if status == "" {
		status = domain.ModelStatusActive
	}
	purposeJSON, err := json.Marshal(c.Purpose)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("marshal check purpose: %w", err)
	}
	triggersJSON, err := json.Marshal(nonNilSlice(c.Triggers))
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("marshal check triggers: %w", err)
	}
	pathFiltersJSON, err := json.Marshal(nonNilSlice(c.PathFilters))
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("marshal check path filters: %w", err)
	}
	stepsJSON, err := json.Marshal(nonNilSlice(c.Steps))
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("marshal check steps: %w", err)
	}
	localCommandsJSON, err := json.Marshal(c.LocalCommands)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("marshal check local commands: %w", err)
	}
	gateJSON, err := json.Marshal(c.Gate)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("marshal check gate: %w", err)
	}
	row := q.QueryRow(ctx, upsertCheckSQL,
		c.ID, c.RepositoryID, c.ComponentID, source, c.Workflow, c.WorkflowName, c.JobKey, c.JobName,
		purposeJSON, c.Environment, triggersJSON, pathFiltersJSON, stepsJSON, localCommandsJSON, gateJSON,
		c.Dispatchable, status, c.Missing, c.NeedsReview)
	out, err := scanCheck(row)
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("upsert check: %w", err)
	}
	return out, nil
}

func (s *ProjectModelStore) SaveCheck(ctx context.Context, c domain.ComponentCheck) (domain.ComponentCheck, error) {
	return upsertCheck(ctx, s.pool, c)
}

func (s *ProjectModelStore) ListChecks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentCheck, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+checkColsPrefixed+`
		FROM component_checks cc
		JOIN project_components pc ON pc.id = cc.component_id
		WHERE cc.repository_id = $1
		ORDER BY pc.path, cc.workflow, cc.job_key`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}
	defer rows.Close()
	var out []domain.ComponentCheck
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ProjectModelStore) GetCheck(ctx context.Context, id uuid.UUID) (domain.ComponentCheck, error) {
	c, err := scanCheck(s.pool.QueryRow(ctx, `SELECT `+checkCols+`
		FROM component_checks WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ComponentCheck{}, fmt.Errorf("get check: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.ComponentCheck{}, fmt.Errorf("get check: %w", err)
	}
	return c, nil
}

// --- SystemResource ---

const resourceCols = `id, kind, vendor, name, identity_key, details, created_at, updated_at`

func scanResource(row pgx.Row) (domain.SystemResource, error) {
	var r domain.SystemResource
	var detailsJSON []byte
	if err := row.Scan(&r.ID, &r.Kind, &r.Vendor, &r.Name, &r.IdentityKey, &detailsJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return domain.SystemResource{}, err
	}
	if err := json.Unmarshal(detailsJSON, &r.Details); err != nil {
		return domain.SystemResource{}, fmt.Errorf("unmarshal resource details: %w", err)
	}
	return r, nil
}

func (s *ProjectModelStore) GetResource(ctx context.Context, id uuid.UUID) (domain.SystemResource, error) {
	r, err := scanResource(s.pool.QueryRow(ctx, `SELECT `+resourceCols+`
		FROM system_resources WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SystemResource{}, fmt.Errorf("get resource: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("get resource: %w", err)
	}
	return r, nil
}

func (s *ProjectModelStore) ListResources(ctx context.Context, ids []uuid.UUID) ([]domain.SystemResource, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+resourceCols+`
		FROM system_resources WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	defer rows.Close()
	var out []domain.SystemResource
	for rows.Next() {
		r, err := scanResource(rows)
		if err != nil {
			return nil, fmt.Errorf("scan resource: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const upsertResourceSQL = `
INSERT INTO system_resources (id, kind, vendor, name, identity_key, details)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (identity_key) DO UPDATE SET
	kind = EXCLUDED.kind,
	vendor = EXCLUDED.vendor,
	name = EXCLUDED.name,
	-- jsonb concatenation favors its right operand's keys on a clash, which is
	-- exactly "new keys win" without a read-then-merge round trip.
	details = system_resources.details || EXCLUDED.details,
	updated_at = now()
RETURNING ` + resourceCols

func (s *ProjectModelStore) EnsureResource(ctx context.Context, r domain.SystemResource) (domain.SystemResource, error) {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	detailsJSON, err := json.Marshal(nonNilMap(r.Details))
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("marshal resource details: %w", err)
	}
	row := s.pool.QueryRow(ctx, upsertResourceSQL, r.ID, r.Kind, r.Vendor, r.Name, r.IdentityKey, detailsJSON)
	out, err := scanResource(row)
	if err != nil {
		return domain.SystemResource{}, fmt.Errorf("ensure resource: %w", err)
	}
	return out, nil
}

// --- ComponentLink ---

const linkCols = `id, repository_id, from_component_id, to_component_id, to_resource_id, protocol, detail, env_vars, evidence, confidence, reason, hint, status, source, auto_confirmed, signal_key, target_host, target_port, missing, created_at, updated_at`

const linkColsPrefixed = `cl.id, cl.repository_id, cl.from_component_id, cl.to_component_id, cl.to_resource_id, cl.protocol, cl.detail, cl.env_vars, cl.evidence, cl.confidence, cl.reason, cl.hint, cl.status, cl.source, cl.auto_confirmed, cl.signal_key, cl.target_host, cl.target_port, cl.missing, cl.created_at, cl.updated_at`

func scanLink(row pgx.Row) (domain.ComponentLink, error) {
	var l domain.ComponentLink
	var envVarsJSON, evidenceJSON []byte
	if err := row.Scan(
		&l.ID, &l.RepositoryID, &l.FromComponentID, &l.ToComponentID, &l.ToResourceID, &l.Protocol, &l.Detail,
		&envVarsJSON, &evidenceJSON, &l.Confidence, &l.Reason, &l.Hint, &l.Status, &l.Source, &l.AutoConfirmed,
		&l.SignalKey, &l.TargetHost, &l.TargetPort, &l.Missing, &l.CreatedAt, &l.UpdatedAt,
	); err != nil {
		return domain.ComponentLink{}, err
	}
	if err := json.Unmarshal(envVarsJSON, &l.EnvVars); err != nil {
		return domain.ComponentLink{}, fmt.Errorf("unmarshal link env vars: %w", err)
	}
	if err := json.Unmarshal(evidenceJSON, &l.Evidence); err != nil {
		return domain.ComponentLink{}, fmt.Errorf("unmarshal link evidence: %w", err)
	}
	return l, nil
}

const upsertLinkSQL = `
INSERT INTO component_links
	(id, repository_id, from_component_id, to_component_id, to_resource_id, protocol, detail, env_vars, evidence, confidence, reason, hint, status, source, auto_confirmed, signal_key, target_host, target_port, missing)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT (id) DO UPDATE SET
	repository_id = EXCLUDED.repository_id,
	from_component_id = EXCLUDED.from_component_id,
	to_component_id = EXCLUDED.to_component_id,
	to_resource_id = EXCLUDED.to_resource_id,
	protocol = EXCLUDED.protocol,
	detail = EXCLUDED.detail,
	env_vars = EXCLUDED.env_vars,
	evidence = EXCLUDED.evidence,
	confidence = EXCLUDED.confidence,
	reason = EXCLUDED.reason,
	hint = EXCLUDED.hint,
	status = EXCLUDED.status,
	source = EXCLUDED.source,
	auto_confirmed = EXCLUDED.auto_confirmed,
	signal_key = EXCLUDED.signal_key,
	target_host = EXCLUDED.target_host,
	target_port = EXCLUDED.target_port,
	missing = EXCLUDED.missing,
	updated_at = now()
RETURNING ` + linkCols

func upsertLink(ctx context.Context, q pmExecutor, l domain.ComponentLink) (domain.ComponentLink, error) {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	protocol := l.Protocol
	if protocol == "" {
		protocol = domain.LinkOther
	}
	status := l.Status
	if status == "" {
		status = domain.LinkSuggested
	}
	source := l.Source
	if source == "" {
		source = domain.LinkSourceScan
	}
	confidence := l.Confidence
	if confidence == "" {
		confidence = domain.ConfidenceLow
	}
	envVarsJSON, err := json.Marshal(nonNilSlice(l.EnvVars))
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("marshal link env vars: %w", err)
	}
	evidenceJSON, err := json.Marshal(nonNilSlice(l.Evidence))
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("marshal link evidence: %w", err)
	}
	row := q.QueryRow(ctx, upsertLinkSQL,
		l.ID, l.RepositoryID, l.FromComponentID, l.ToComponentID, l.ToResourceID, protocol, l.Detail,
		envVarsJSON, evidenceJSON, confidence, l.Reason, l.Hint, status, source, l.AutoConfirmed, l.SignalKey,
		l.TargetHost, l.TargetPort, l.Missing)
	out, err := scanLink(row)
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("upsert link: %w", err)
	}
	return out, nil
}

func (s *ProjectModelStore) SaveLink(ctx context.Context, l domain.ComponentLink) (domain.ComponentLink, error) {
	return upsertLink(ctx, s.pool, l)
}

func (s *ProjectModelStore) ListLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+linkCols+`
		FROM component_links WHERE repository_id = $1 ORDER BY created_at`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()
	var out []domain.ComponentLink
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListIncomingLinks joins through the target component to find edges whose
// origin repository is not the one asking — the FK on to_component_id makes
// this an inner join safe from fan-out (at most one component per link).
func (s *ProjectModelStore) ListIncomingLinks(ctx context.Context, repositoryID uuid.UUID) ([]domain.ComponentLink, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+linkColsPrefixed+`
		FROM component_links cl
		JOIN project_components pc ON pc.id = cl.to_component_id
		WHERE pc.repository_id = $1 AND cl.repository_id <> $1
		ORDER BY cl.created_at`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list incoming links: %w", err)
	}
	defer rows.Close()
	var out []domain.ComponentLink
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListLinksForRepositories: LEFT JOIN, not INNER, because an unresolved link
// (to_component_id NULL) can still match on repository_id — and since
// to_component_id references at most one component, the join cannot
// duplicate a link row, so no DISTINCT is needed to dedupe.
func (s *ProjectModelStore) ListLinksForRepositories(ctx context.Context, repositoryIDs []uuid.UUID) ([]domain.ComponentLink, error) {
	if len(repositoryIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+linkColsPrefixed+`
		FROM component_links cl
		LEFT JOIN project_components pc ON pc.id = cl.to_component_id
		WHERE cl.repository_id = ANY($1) OR pc.repository_id = ANY($1)
		ORDER BY cl.created_at`, repositoryIDs)
	if err != nil {
		return nil, fmt.Errorf("list links for repositories: %w", err)
	}
	defer rows.Close()
	var out []domain.ComponentLink
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *ProjectModelStore) GetLink(ctx context.Context, id uuid.UUID) (domain.ComponentLink, error) {
	l, err := scanLink(s.pool.QueryRow(ctx, `SELECT `+linkCols+`
		FROM component_links WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ComponentLink{}, fmt.Errorf("get link: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.ComponentLink{}, fmt.Errorf("get link: %w", err)
	}
	return l, nil
}

func (s *ProjectModelStore) DeleteLink(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM component_links WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete link: %w", port.ErrNotFound)
	}
	return nil
}

// --- ProjectNote ---

const noteCols = `id, repository_id, component_id, topic, body_md, evidence, source_commit, stale, author, locked, created_at, updated_at`

func scanNote(row pgx.Row) (domain.ProjectNote, error) {
	var n domain.ProjectNote
	var evidenceJSON []byte
	if err := row.Scan(
		&n.ID, &n.RepositoryID, &n.ComponentID, &n.Topic, &n.BodyMD, &evidenceJSON, &n.SourceCommit,
		&n.Stale, &n.Author, &n.Locked, &n.CreatedAt, &n.UpdatedAt,
	); err != nil {
		return domain.ProjectNote{}, err
	}
	if err := json.Unmarshal(evidenceJSON, &n.Evidence); err != nil {
		return domain.ProjectNote{}, fmt.Errorf("unmarshal note evidence: %w", err)
	}
	return n, nil
}

// upsertNoteSQL targets the expression unique index directly (its inference
// expressions must match idx_project_notes_scope_topic exactly): the nil
// component sentinel is what lets a repository-level note and a component
// note share the same topic without colliding.
const upsertNoteSQL = `
INSERT INTO project_notes (id, repository_id, component_id, topic, body_md, evidence, source_commit, stale, author, locked)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (repository_id, COALESCE(component_id, '00000000-0000-0000-0000-000000000000'::uuid), topic) DO UPDATE SET
	body_md = EXCLUDED.body_md,
	evidence = EXCLUDED.evidence,
	source_commit = EXCLUDED.source_commit,
	stale = EXCLUDED.stale,
	author = EXCLUDED.author,
	locked = EXCLUDED.locked,
	updated_at = now()
RETURNING ` + noteCols

func (s *ProjectModelStore) SaveNote(ctx context.Context, n domain.ProjectNote) (domain.ProjectNote, error) {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	author := n.Author
	if author == "" {
		author = domain.NoteAuthorAgent
	}
	evidenceJSON, err := json.Marshal(nonNilSlice(n.Evidence))
	if err != nil {
		return domain.ProjectNote{}, fmt.Errorf("marshal note evidence: %w", err)
	}
	row := s.pool.QueryRow(ctx, upsertNoteSQL,
		n.ID, n.RepositoryID, n.ComponentID, n.Topic, n.BodyMD, evidenceJSON, n.SourceCommit, n.Stale, author, n.Locked)
	out, err := scanNote(row)
	if err != nil {
		return domain.ProjectNote{}, fmt.Errorf("save note: %w", err)
	}
	return out, nil
}

func (s *ProjectModelStore) ListNotes(ctx context.Context, repositoryID uuid.UUID) ([]domain.ProjectNote, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+noteCols+`
		FROM project_notes WHERE repository_id = $1 ORDER BY topic`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()
	var out []domain.ProjectNote
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *ProjectModelStore) GetNote(ctx context.Context, id uuid.UUID) (domain.ProjectNote, error) {
	n, err := scanNote(s.pool.QueryRow(ctx, `SELECT `+noteCols+`
		FROM project_notes WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectNote{}, fmt.Errorf("get note: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.ProjectNote{}, fmt.Errorf("get note: %w", err)
	}
	return n, nil
}

func (s *ProjectModelStore) DeleteNote(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM project_notes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete note: %w", port.ErrNotFound)
	}
	return nil
}

// MarkNotesStale loads the repository's notes rather than pushing the prefix
// match into SQL: the match is "evidence path equals or is a directory
// ancestor of a changed path", which is cheap over the handful of notes one
// repository holds and far simpler than expressing prefix-of-many in a query.
func (s *ProjectModelStore) MarkNotesStale(ctx context.Context, repositoryID uuid.UUID, changedPaths []string) ([]domain.ProjectNote, error) {
	notes, err := s.ListNotes(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("mark notes stale: %w", err)
	}
	var out []domain.ProjectNote
	for _, n := range notes {
		if !noteEvidenceTouchesAny(n, changedPaths) {
			continue
		}
		updated, err := scanNote(s.pool.QueryRow(ctx, `
			UPDATE project_notes SET stale = true, updated_at = now() WHERE id = $1
			RETURNING `+noteCols, n.ID))
		if err != nil {
			return nil, fmt.Errorf("mark note stale: %w", err)
		}
		out = append(out, updated)
	}
	return out, nil
}

func noteEvidenceTouchesAny(n domain.ProjectNote, changedPaths []string) bool {
	for _, ev := range n.Evidence {
		for _, changed := range changedPaths {
			if ev.Path == changed || strings.HasPrefix(changed, ev.Path+"/") {
				return true
			}
		}
	}
	return false
}

// --- ProjectScan ---

const scanCols = `id, repository_id, trigger, status, stage, commit_sha, events, result, review_count, error, started_at, finished_at`

// scanColsNoResult casts the placeholder explicitly so pgx sees a concrete
// jsonb OID for the column instead of the ambiguous "unknown" type a bare
// NULL literal would report.
const scanColsNoResult = `id, repository_id, trigger, status, stage, commit_sha, events, NULL::jsonb, review_count, error, started_at, finished_at`

func scanProjectScan(row pgx.Row) (domain.ProjectScan, error) {
	var sc domain.ProjectScan
	var eventsJSON, resultJSON []byte
	if err := row.Scan(
		&sc.ID, &sc.RepositoryID, &sc.Trigger, &sc.Status, &sc.Stage, &sc.CommitSHA, &eventsJSON, &resultJSON,
		&sc.ReviewCount, &sc.Error, &sc.StartedAt, &sc.FinishedAt,
	); err != nil {
		return domain.ProjectScan{}, err
	}
	if err := json.Unmarshal(eventsJSON, &sc.Events); err != nil {
		return domain.ProjectScan{}, fmt.Errorf("unmarshal scan events: %w", err)
	}
	if resultJSON != nil {
		var result domain.ScanResult
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return domain.ProjectScan{}, fmt.Errorf("unmarshal scan result: %w", err)
		}
		sc.Result = &result
	}
	return sc, nil
}

const insertScanSQL = `
INSERT INTO project_scans (id, repository_id, trigger, status, stage, commit_sha, events, result, review_count, error, started_at, finished_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING ` + scanCols

func (s *ProjectModelStore) CreateScan(ctx context.Context, sc domain.ProjectScan) (domain.ProjectScan, error) {
	if sc.ID == uuid.Nil {
		sc.ID = uuid.New()
	}
	startedAt := sc.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	eventsJSON, err := json.Marshal(nonNilSlice(sc.Events))
	if err != nil {
		return domain.ProjectScan{}, fmt.Errorf("marshal scan events: %w", err)
	}
	var resultJSON []byte
	if sc.Result != nil {
		resultJSON, err = json.Marshal(sc.Result)
		if err != nil {
			return domain.ProjectScan{}, fmt.Errorf("marshal scan result: %w", err)
		}
	}
	row := s.pool.QueryRow(ctx, insertScanSQL,
		sc.ID, sc.RepositoryID, sc.Trigger, sc.Status, sc.Stage, sc.CommitSHA, eventsJSON, resultJSON,
		sc.ReviewCount, sc.Error, startedAt, sc.FinishedAt)
	out, err := scanProjectScan(row)
	if err != nil {
		return domain.ProjectScan{}, fmt.Errorf("create scan: %w", err)
	}
	return out, nil
}

const updateScanSQL = `
UPDATE project_scans SET
	status = $2, stage = $3, commit_sha = $4, events = $5, result = $6, review_count = $7, error = $8, finished_at = $9
WHERE id = $1`

func (s *ProjectModelStore) UpdateScan(ctx context.Context, sc domain.ProjectScan) error {
	eventsJSON, err := json.Marshal(nonNilSlice(sc.Events))
	if err != nil {
		return fmt.Errorf("marshal scan events: %w", err)
	}
	var resultJSON []byte
	if sc.Result != nil {
		resultJSON, err = json.Marshal(sc.Result)
		if err != nil {
			return fmt.Errorf("marshal scan result: %w", err)
		}
	}
	tag, err := s.pool.Exec(ctx, updateScanSQL,
		sc.ID, sc.Status, sc.Stage, sc.CommitSHA, eventsJSON, resultJSON, sc.ReviewCount, sc.Error, sc.FinishedAt)
	if err != nil {
		return fmt.Errorf("update scan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update scan: %w", port.ErrNotFound)
	}
	return nil
}

func (s *ProjectModelStore) GetScan(ctx context.Context, id uuid.UUID) (domain.ProjectScan, error) {
	sc, err := scanProjectScan(s.pool.QueryRow(ctx, `SELECT `+scanCols+`
		FROM project_scans WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectScan{}, fmt.Errorf("get scan: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.ProjectScan{}, fmt.Errorf("get scan: %w", err)
	}
	return sc, nil
}

func (s *ProjectModelStore) LatestScan(ctx context.Context, repositoryID uuid.UUID) (domain.ProjectScan, error) {
	sc, err := scanProjectScan(s.pool.QueryRow(ctx, `SELECT `+scanColsNoResult+`
		FROM project_scans WHERE repository_id = $1 ORDER BY started_at DESC LIMIT 1`, repositoryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectScan{}, fmt.Errorf("latest scan: %w", port.ErrNotFound)
	}
	if err != nil {
		return domain.ProjectScan{}, fmt.Errorf("latest scan: %w", err)
	}
	return sc, nil
}

func (s *ProjectModelStore) FailInterruptedScans(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE project_scans SET status = $1, error = 'interrupted by a server restart', finished_at = now()
		WHERE status IN ($2, $3)`,
		domain.ScanFailed, domain.ScanQueued, domain.ScanRunning)
	if err != nil {
		return 0, fmt.Errorf("fail interrupted scans: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// --- ModelReconciler ---

// ApplyReconcile runs one scan's worth of writes atomically. Deletes carry
// r.RepositoryID in their WHERE clause even though the id list already came
// from that repository's own reconcile pass — a stray id from a differently
// scoped caller must be a no-op, not a cross-repository delete.
func (s *ProjectModelStore) ApplyReconcile(ctx context.Context, r port.ModelReconcile) error {
	return s.pool.InTx(ctx, func(tx pgx.Tx) error {
		for _, c := range r.SaveComponents {
			if _, err := upsertComponent(ctx, tx, c); err != nil {
				return fmt.Errorf("reconcile upsert component: %w", err)
			}
		}
		for _, c := range r.SaveChecks {
			if _, err := upsertCheck(ctx, tx, c); err != nil {
				return fmt.Errorf("reconcile upsert check: %w", err)
			}
		}
		for _, l := range r.SaveLinks {
			if _, err := upsertLink(ctx, tx, l); err != nil {
				return fmt.Errorf("reconcile upsert link: %w", err)
			}
		}
		if len(r.DeleteLinks) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM component_links WHERE repository_id = $1 AND id = ANY($2)`,
				r.RepositoryID, r.DeleteLinks); err != nil {
				return fmt.Errorf("reconcile delete links: %w", err)
			}
		}
		if len(r.DeleteChecks) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM component_checks WHERE repository_id = $1 AND id = ANY($2)`,
				r.RepositoryID, r.DeleteChecks); err != nil {
				return fmt.Errorf("reconcile delete checks: %w", err)
			}
		}
		if len(r.DeleteComponents) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM project_components WHERE repository_id = $1 AND id = ANY($2)`,
				r.RepositoryID, r.DeleteComponents); err != nil {
				return fmt.Errorf("reconcile delete components: %w", err)
			}
		}
		return nil
	})
}
