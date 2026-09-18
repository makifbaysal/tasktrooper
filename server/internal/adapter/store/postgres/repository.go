package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

type RepositoryStore struct {
	pool *DB

	// hosts describes THIS host's filesystem, so a root_path written by another
	// host can be re-anchored on the way out of the store. See localizeRootPath.
	hosts hostRoots

	cipherOnce sync.Once
	cipher     *secrets.Cipher
	cipherErr  error
}

func NewRepositoryStore(pool *DB) *RepositoryStore {
	return &RepositoryStore{pool: pool}
}

// SetHostRoots tells the store which filesystem it is reading rows on behalf
// of. It is a setter rather than a constructor argument because every caller
// that does not know (tests, tools that never touch a working copy) is
// correctly served by the zero value: with no workspace root nothing is ever
// re-anchored and the stored path is returned verbatim, exactly as before.
func (s *RepositoryStore) SetHostRoots(workspaceRoot string, allowedRoots []string) *RepositoryStore {
	s.hosts.set(workspaceRoot, allowedRoots)
	return s
}

// localizeRootPath translates a stored root_path into one this host can
// actually use, and is applied to every repositories row leaving this store.
//
// root_path is an absolute path belonging to whichever host wrote the row, and
// one database is now served by two of them: the cloud pod (PVC at
// /data) and the user's Mac behind a reverse tunnel (DATA_DIR under the repo).
// A board run on the Mac read the pod's "/data/..." and died in git clone with
// `mkdir /data: read-only file system`. Translating here rather than at each
// consumer is deliberate — the board runner, task chat, webhooks, repo profile,
// the indexer and the board tools all reach a working copy through a row read
// from this store, and none of them can be expected to remember the rule.
//
// The path is only rewritten when it is genuinely foreign (see
// workspace.HostRootPath); anything that exists here, or that lies under one of
// this host's roots, is left alone.
func (s *RepositoryStore) localizeRootPath(r *domain.Repository) {
	if s == nil || r == nil || r.RootPath == "" {
		return
	}
	resolved, reanchored, first := s.hosts.localize(r.RootPath)
	if !reanchored {
		return
	}
	if first {
		log.Info().
			Str("repository", r.Name).
			Str("stored_root_path", r.RootPath).
			Str("host_root_path", resolved).
			Msg("repository root_path was written by another host; re-anchored to this host's workspace root")
	}
	r.RootPath = resolved
}

// repositoryCols is the canonical repositories column list shared by every
// SELECT/RETURNING below so the read order can never drift from scanRepository.
const repositoryCols = `id, name, description, root_path, remote_url, verify_command, build_command, test_command, kind, mobile_platform, detected_bundle_id, detected_package_name, detected_xcode_scheme, detected_gradle_module, release_engine, sub_repo_kinds, sub_projects, docs, docs_task_id, auto_release_on_done, require_human_review, require_review_chain, require_release_deploy, require_pipeline_for_review, incident_policy, test_strategy, coverage_threshold, require_overall_coverage, mutation_enabled, mutation_threshold, webhook_hook_id, COALESCE(profile_md, ''), profile_updated_at, created_at, updated_at`

// scanRepository reads a single repositories row in repositoryCols order and
// re-anchors its root_path onto this host. Every SELECT/RETURNING in this file
// goes through it, so no read path can hand a consumer another host's path.
func (s *RepositoryStore) scanRepository(row interface{ Scan(dest ...any) error }) (domain.Repository, error) {
	r, err := scanRepositoryRow(row)
	if err != nil {
		return r, err
	}
	s.localizeRootPath(&r)
	return r, nil
}

// scanRepositoryRow is the raw column read, kept separate only so the scan
// order stays in one place.
func scanRepositoryRow(row interface{ Scan(dest ...any) error }) (domain.Repository, error) {
	var r domain.Repository
	var incidentPolicy string
	var webhookHookID int64
	var subProjectsJSON []byte
	var docsJSON []byte
	err := row.Scan(
		&r.ID, &r.Name, &r.Description, &r.RootPath, &r.RemoteURL, &r.VerifyCommand, &r.BuildCommand, &r.TestCommand,
		&r.Kind, &r.MobilePlatform, &r.DetectedAppIdentity.BundleID, &r.DetectedAppIdentity.PackageName,
		&r.DetectedBuildTargets.XcodeScheme, &r.DetectedBuildTargets.GradleModule, &r.ReleaseEngine,
		&r.SubRepoKinds, &subProjectsJSON, &docsJSON, &r.DocsTaskID,
		&r.AutoReleaseOnDone, &r.RequireHumanReview,
		&r.RequireReviewChain, &r.RequireReleaseDeploy, &r.RequirePipelineForReview, &incidentPolicy,
		&r.TestStrategy, &r.CoverageThreshold, &r.RequireOverallCoverage, &r.MutationEnabled, &r.MutationThreshold,
		&webhookHookID, &r.ProfileMD, &r.ProfileUpdatedAt, &r.CreatedAt, &r.UpdatedAt,
	)
	r.IncidentPolicy = domain.IncidentPolicy(incidentPolicy)
	r.WebhookInstalled = webhookHookID != 0
	_ = json.Unmarshal(subProjectsJSON, &r.SubProjects)
	_ = json.Unmarshal(docsJSON, &r.Docs)
	return r, err
}

// SetCipher injects the cipher derived at boot, before
// runtime.scrubProcessSecrets wipes MCP_SECRETS_KEY from the process
// environment. Without it, SetWebhook/WebhookSecret only derive a cipher on
// first use — which for this store is always a later HTTP request, after the
// key is already gone — so every webhook install failed with a
// missing-key error that surfaced as "Set up webhook" never succeeding and
// webhook_installed never turning true.
func (s *RepositoryStore) SetCipher(c *secrets.Cipher, err error) {
	s.cipherOnce.Do(func() { s.cipher, s.cipherErr = c, err })
}

func (s *RepositoryStore) getCipher() (*secrets.Cipher, error) {
	s.cipherOnce.Do(func() {
		s.cipher, s.cipherErr = secrets.NewCipherFromEnv()
	})
	return s.cipher, s.cipherErr
}

// SetWebhook stores the push-webhook secret (encrypted at rest) together with
// the GitHub hook id it belongs to. The two travel together on purpose: a
// secret without its hook (or the reverse) can only produce signature
// mismatches that look like attacks in the logs.
func (s *RepositoryStore) SetWebhook(ctx context.Context, id uuid.UUID, secret string, hookID int64) error {
	cipher, err := s.getCipher()
	if err != nil {
		return err
	}
	ct, err := cipher.Encrypt(secret)
	if err != nil {
		return fmt.Errorf("encrypt webhook secret: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE repositories SET webhook_secret_enc = $2, webhook_hook_id = $3, updated_at = now()
		WHERE id = $1
	`, id, base64.StdEncoding.EncodeToString(ct), hookID)
	if err != nil {
		return fmt.Errorf("set repository webhook: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set repository webhook: %w", port.ErrNotFound)
	}
	return nil
}

// WebhookSecret decrypts and returns the repo's webhook secret; "" when no
// webhook has been set up.
func (s *RepositoryStore) WebhookSecret(ctx context.Context, id uuid.UUID) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT webhook_secret_enc FROM repositories WHERE id = $1`, id).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("get webhook secret: %w", port.ErrNotFound)
		}
		return "", fmt.Errorf("get webhook secret: %w", err)
	}
	if value == "" {
		return "", nil
	}
	cipher, err := s.getCipher()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode webhook secret: %w", err)
	}
	return cipher.Decrypt(raw)
}

func (s *RepositoryStore) Create(ctx context.Context, name, description, rootPath, remoteURL, kind string) (domain.Repository, error) {
	// NULLIF keeps the column default ('backend') for callers that pass no
	// kind, so the insert stays valid against the NOT NULL column.
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		INSERT INTO repositories (name, description, root_path, remote_url, kind)
		VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5, ''), 'backend'))
		RETURNING `+repositoryCols,
		name, description, rootPath, remoteURL, kind))
	if err != nil {
		return domain.Repository{}, fmt.Errorf("create repository: %w", err)
	}
	return r, nil
}

// UpdateRemoteURL records the origin a repository was cloned from. Used both at
// registration and to backfill rows created before remote_url existed, by
// reading the origin out of an existing working copy while one is still there.
func (s *RepositoryStore) UpdateRemoteURL(ctx context.Context, id uuid.UUID, remoteURL string) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET remote_url = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, remoteURL))
	if err != nil {
		return domain.Repository{}, fmt.Errorf("update repository remote url: %w", err)
	}
	return r, nil
}

// UpdateRootPath re-points a repository at the working copy that is really on
// disk, after it was restored onto the host currently serving this install.
//
// The written value is this host's absolute path, exactly as at registration:
// root_path stays advisory across hosts and the other host re-anchors it on
// read (localizeRootPath). Writing something host-neutral instead would mean
// inventing a second notion of "where the code is" for the one host that can
// answer it.
func (s *RepositoryStore) UpdateRootPath(ctx context.Context, id uuid.UUID, rootPath string) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET root_path = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, rootPath))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository root path: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository root path: %w", err)
	}
	return r, nil
}

func (s *RepositoryStore) Get(ctx context.Context, id uuid.UUID) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `SELECT `+repositoryCols+` FROM repositories WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("get repository: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("get repository: %w", err)
	}
	projectIDs, err := s.ListProjectIDs(ctx, id)
	if err != nil {
		return domain.Repository{}, err
	}
	r.ProjectIDs = projectIDs
	return r, nil
}

// GetByRootPath finds a repository by its working-copy path.
//
// The exact match is tried first and is the only thing a single-host
// deployment ever needs. The fallback exists because root_path is host
// specific: the Mac opening <DATA_DIR>/workspaces/repos/acme-web must still
// find the row the pod wrote as /data/workspaces/repos/acme-web. Without it,
// re-opening the same repository from the second host inserts a second row for
// one remote, and the board tasks, indexes and runs that hang off a repository
// id are then split across two ids that never converge.
//
// The fallback matches on the directory name, but only against rows whose
// stored path is foreign to this host: a row naming a path that is usable here
// is a different repository which merely shares a directory name, and
// conflating those would be worse than the duplicate. Name collision across
// hosts is the accepted residue — two hosts serving one database are expected
// to hold the same repositories.
func (s *RepositoryStore) GetByRootPath(ctx context.Context, rootPath string) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `SELECT `+repositoryCols+` FROM repositories WHERE root_path = $1`, rootPath))
	if err == nil {
		return r, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Repository{}, fmt.Errorf("get repository by root: %w", err)
	}
	name := repoDirName(rootPath)
	if name == "" {
		return domain.Repository{}, fmt.Errorf("get repository by root: %w", err)
	}
	// regexp_replace drops everything through the last separator, so this is an
	// equality test on the directory name — a bound parameter, no LIKE, and no
	// pattern metacharacters reachable from a caller-supplied path.
	rows, queryErr := s.pool.Query(ctx, `SELECT `+repositoryCols+` FROM repositories
		WHERE regexp_replace(root_path, '^.*/', '') = $1 AND root_path <> $2
		ORDER BY updated_at DESC`, name, rootPath)
	if queryErr != nil {
		return domain.Repository{}, fmt.Errorf("get repository by root: %w", queryErr)
	}
	defer rows.Close()
	for rows.Next() {
		// Scanned raw on purpose: the foreign-path test has to see the stored
		// path, which localizeRootPath would already have rewritten.
		candidate, scanErr := scanRepositoryRow(rows)
		if scanErr != nil {
			return domain.Repository{}, fmt.Errorf("get repository by root: %w", scanErr)
		}
		if s.hosts.usable(candidate.RootPath) {
			continue
		}
		log.Info().
			Str("repository", candidate.Name).
			Str("stored_root_path", candidate.RootPath).
			Str("requested_root_path", rootPath).
			Msg("repository matched by directory name; its root_path belongs to another host")
		s.localizeRootPath(&candidate)
		return candidate, nil
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return domain.Repository{}, fmt.Errorf("get repository by root: %w", rowsErr)
	}
	// Still not found: return the original pgx.ErrNoRows so callers that only
	// test "did this succeed" keep behaving exactly as before.
	return domain.Repository{}, fmt.Errorf("get repository by root: %w", err)
}

// repoDirName is the final segment of a root path — the repository's directory
// name, the only part of a foreign path that means anything on another host.
func repoDirName(rootPath string) string {
	trimmed := strings.TrimSpace(rootPath)
	if trimmed == "" {
		return ""
	}
	name := filepath.Base(filepath.Clean(trimmed))
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func (s *RepositoryStore) List(ctx context.Context) ([]domain.Repository, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+repositoryCols+` FROM repositories ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()
	repos, err := s.scanRepositories(ctx, rows)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return repos, nil
	}
	ids := make([]uuid.UUID, len(repos))
	for i, r := range repos {
		ids[i] = r.ID
	}
	byRepo, err := s.ListProjectIDsByRepositories(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range repos {
		repos[i].ProjectIDs = byRepo[repos[i].ID]
	}
	return repos, nil
}

func (s *RepositoryStore) scanRepositories(ctx context.Context, rows pgx.Rows) ([]domain.Repository, error) {
	var repos []domain.Repository
	for rows.Next() {
		r, err := s.scanRepository(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, r)
	}
	return repos, rows.Err()
}

func (s *RepositoryStore) Update(ctx context.Context, id uuid.UUID, name, description string, verifyCommand, buildCommand, testCommand *string, requireHumanReview *bool) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET
			name = COALESCE(NULLIF($2, ''), name),
			description = COALESCE($3, description),
			verify_command = COALESCE($4, verify_command),
			build_command = COALESCE($5, build_command),
			test_command = COALESCE($6, test_command),
			require_human_review = COALESCE($7, require_human_review),
			updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols,
		id, name, description, verifyCommand, buildCommand, testCommand, requireHumanReview))
	if err != nil {
		return domain.Repository{}, fmt.Errorf("update repository: %w", err)
	}
	return r, nil
}

// UpdateIncidentPolicy sets what a production incident on this repo triggers.
// It is separate from Update so the incident settings screen cannot blank an
// unrelated command field by omitting it.
func (s *RepositoryStore) UpdateIncidentPolicy(ctx context.Context, id uuid.UUID, policy domain.IncidentPolicy) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET incident_policy = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols,
		id, string(policy)))
	if err != nil {
		return domain.Repository{}, fmt.Errorf("update incident policy: %w", err)
	}
	return r, nil
}

// UpdateTestStrategy sets how the repo's tasks are verified (local / stage /
// per_step).
func (s *RepositoryStore) UpdateTestStrategy(ctx context.Context, id uuid.UUID, strategy string) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET test_strategy = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, strategy))
	if err != nil {
		return domain.Repository{}, fmt.Errorf("update test strategy: %w", err)
	}
	return r, nil
}

// UpdateLifecycleGates arms/disarms the done and released gates. Separate from
// Update for the same reason UpdateIncidentPolicy is: a settings screen that
// posts one toggle must not blank an unrelated command field by omitting it.
func (s *RepositoryStore) UpdateLifecycleGates(ctx context.Context, id uuid.UUID, requireReviewChain, requireReleaseDeploy, requirePipelineForReview, requireOverallCoverage *bool, coverageThreshold *float64) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET
			require_review_chain = COALESCE($2, require_review_chain),
			require_release_deploy = COALESCE($3, require_release_deploy),
			require_pipeline_for_review = COALESCE($4, require_pipeline_for_review),
			require_overall_coverage = COALESCE($5, require_overall_coverage),
			coverage_threshold = COALESCE($6, coverage_threshold),
			updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols,
		id, requireReviewChain, requireReleaseDeploy, requirePipelineForReview, requireOverallCoverage, coverageThreshold))
	if err != nil {
		return domain.Repository{}, fmt.Errorf("update lifecycle gates: %w", err)
	}
	return r, nil
}

// UpdateProfile replaces the agent-maintained project profile and stamps
// profile_updated_at. Separate from Update for the same reason the other
// single-purpose setters are: the tool that writes it must not be able to
// blank an unrelated field.
func (s *RepositoryStore) UpdateProfile(ctx context.Context, id uuid.UUID, profileMD string) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET profile_md = $2, profile_updated_at = now(), updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, profileMD))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository profile: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository profile: %w", err)
	}
	return r, nil
}

// UpdateDocs replaces the repository's own reference-doc pointers wholesale.
// Separate from Update for the same reason UpdateProfile is: a writer of one
// concern must not be able to blank an unrelated field by omitting it.
func (s *RepositoryStore) UpdateDocs(ctx context.Context, id uuid.UUID, docs domain.RepositoryDocs) (domain.Repository, error) {
	body, err := json.Marshal(docs)
	if err != nil {
		return domain.Repository{}, fmt.Errorf("marshal repository docs: %w", err)
	}
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET docs = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, body))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository docs: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository docs: %w", err)
	}
	return r, nil
}

// UpdateMobilePlatform records which platform a mobile repository targets.
// Its own setter for the reason every other single-purpose setter here is one:
// it is written by import detection and by one field of a settings form, and
// neither may blank an unrelated column by omitting it.
func (s *RepositoryStore) UpdateMobilePlatform(ctx context.Context, id uuid.UUID, platform string) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET mobile_platform = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, platform))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository mobile platform: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository mobile platform: %w", err)
	}
	return r, nil
}

// UpdateReleaseEngine records where this repository's mobile releases are
// built and uploaded. Its own setter for the same reason UpdateMobilePlatform
// is: it is written by one field of a settings form, and must not blank an
// unrelated column by omitting it.
func (s *RepositoryStore) UpdateReleaseEngine(ctx context.Context, id uuid.UUID, engine string) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET release_engine = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, engine))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository release engine: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository release engine: %w", err)
	}
	return r, nil
}

// UpdateDetectedAppIdentity records the bundle id / package name read off the
// working copy. Its own setter for the same reason UpdateMobilePlatform is:
// import detection writes it and nothing else may blank it in passing.
func (s *RepositoryStore) UpdateDetectedAppIdentity(ctx context.Context, id uuid.UUID, identity domain.AppIdentity) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET detected_bundle_id = $2, detected_package_name = $3, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, identity.BundleID, identity.PackageName))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository app identity: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository app identity: %w", err)
	}
	return r, nil
}

// UpdateDetectedBuildTargets records the Xcode scheme / Gradle module read off
// the working copy. Its own setter beside UpdateDetectedAppIdentity rather
// than a second pair of columns on it: a working copy can state one and not
// the other, and a detection pass that read no scheme must not blank a package
// name it never looked at.
func (s *RepositoryStore) UpdateDetectedBuildTargets(ctx context.Context, id uuid.UUID, targets domain.BuildTargets) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET detected_xcode_scheme = $2, detected_gradle_module = $3, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, targets.XcodeScheme, targets.GradleModule))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository build targets: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository build targets: %w", err)
	}
	return r, nil
}

// UpdateMutationGate arms or disarms the mutation-score bar and sets the
// number it is judged against; each is applied only when its pointer is
// non-nil. The coverage half of the same pair lives on UpdateLifecycleGates,
// which the operations settings screen owns.
func (s *RepositoryStore) UpdateMutationGate(ctx context.Context, id uuid.UUID, enabled *bool, threshold *float64) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET
			mutation_enabled = COALESCE($2, mutation_enabled),
			mutation_threshold = COALESCE($3, mutation_threshold),
			updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, enabled, threshold))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update mutation gate: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update mutation gate: %w", err)
	}
	return r, nil
}

// SetDocsTaskID records (or, with "", clears) the reference-doc bundle task
// this repository is waiting on.
func (s *RepositoryStore) SetDocsTaskID(ctx context.Context, id uuid.UUID, taskID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE repositories SET docs_task_id = $2, updated_at = now() WHERE id = $1
	`, id, taskID)
	if err != nil {
		return fmt.Errorf("set repository docs task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set repository docs task: %w", port.ErrNotFound)
	}
	return nil
}

// UpdateMeta applies kind / sub-repo kinds / auto-release only when the
// matching pointer is non-nil, leaving unspecified fields untouched.
func (s *RepositoryStore) UpdateMeta(ctx context.Context, id uuid.UUID, kind *string, subRepoKinds *[]string, autoReleaseOnDone *bool) (domain.Repository, error) {
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET
			kind = COALESCE($2, kind),
			sub_repo_kinds = COALESCE($3, sub_repo_kinds),
			auto_release_on_done = COALESCE($4, auto_release_on_done),
			updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols,
		id, kind, subRepoKinds, autoReleaseOnDone))
	if err != nil {
		return domain.Repository{}, fmt.Errorf("update repository meta: %w", err)
	}
	return r, nil
}

// UpdateSubProjects replaces the monorepo sub-project list wholesale (see
// port.RepositoryStore.UpdateSubProjects for why this is separate from
// UpdateMeta).
func (s *RepositoryStore) UpdateSubProjects(ctx context.Context, id uuid.UUID, subProjects []domain.RepoSubProject) (domain.Repository, error) {
	if subProjects == nil {
		subProjects = []domain.RepoSubProject{}
	}
	body, err := json.Marshal(subProjects)
	if err != nil {
		return domain.Repository{}, fmt.Errorf("marshal sub-projects: %w", err)
	}
	r, err := s.scanRepository(s.pool.QueryRow(ctx, `
		UPDATE repositories SET sub_projects = $2, updated_at = now()
		WHERE id = $1
		RETURNING `+repositoryCols, id, body))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Repository{}, fmt.Errorf("update repository sub-projects: %w", port.ErrNotFound)
		}
		return domain.Repository{}, fmt.Errorf("update repository sub-projects: %w", err)
	}
	return r, nil
}

func (s *RepositoryStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM repositories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("repository not found")
	}
	return nil
}

func (s *RepositoryStore) SetProjects(ctx context.Context, repositoryID uuid.UUID, projectIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM repository_projects WHERE repository_id = $1`, repositoryID); err != nil {
		return err
	}
	for _, pid := range projectIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO repository_projects (repository_id, project_id) VALUES ($1, $2)
		`, repositoryID, pid); err != nil {
			return fmt.Errorf("link repository project: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *RepositoryStore) ListProjectIDs(ctx context.Context, repositoryID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT project_id FROM repository_projects WHERE repository_id = $1 ORDER BY project_id
	`, repositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *RepositoryStore) ListProjectIDsByRepositories(ctx context.Context, repositoryIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	result := make(map[uuid.UUID][]uuid.UUID)
	if len(repositoryIDs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT repository_id, project_id FROM repository_projects
		WHERE repository_id = ANY($1) ORDER BY repository_id, project_id
	`, repositoryIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var repoID, projectID uuid.UUID
		if err := rows.Scan(&repoID, &projectID); err != nil {
			return nil, err
		}
		result[repoID] = append(result[repoID], projectID)
	}
	return result, rows.Err()
}

type BoardTaskStore struct {
	pool *DB
}

func NewBoardTaskStore(pool *DB) *BoardTaskStore {
	return &BoardTaskStore{pool: pool}
}

// boardTaskColumns is the INSERT/UPDATE RETURNING projection. It must stay
// column-for-column aligned with boardTaskSelect, because both feed
// scanBoardTask — a column added to one and not the other fails at runtime with
// a scan-arity error that no test catches, since the store is not unit-tested.
//
// column_entered_at and blocked_resume_at are NULL here rather than looked up:
// a freshly inserted task has no span yet (the dispatcher opens it just after)
// and cannot be parked on a quota it has never run against, and the board reads
// both from the list endpoints, which go through boardTaskSelect.
// taskKeySQL builds a task's key for the queries that need it as a plain column
// (relations, deploy packages) rather than as a scanned task. It must agree with
// domain.TaskKeyPrefix — the same key, whichever side computes it.
const taskKeySQL = `CASE bt.task_type WHEN 'bug' THEN 'B' WHEN 'analiz' THEN 'A' ELSE 'T' END || '-' || bt.task_number`

const boardTaskColumns = `id, repository_id, task_number, title, task_type,
		description, technical_description, initiative_project_id, board_column,
		position, priority, created_by, assignee_agent_id, created_at, updated_at,
		blocked_question, blocked_session_id, blocked_at, blocked_origin_column,
		blocked_resource,
		clarification_session_id,
		has_migration, stage_verified_at, verified_sha,
		before_deploy, after_deploy, rollback_plan,
		pr_url, pr_number, merge_commit_sha,
		NULL::timestamptz, NULL::timestamptz`

// The quota lateral is what puts "resumes at 14:30" on a parked card: the reset
// time lives on the RUN (task_agent_runs.quota_resume_at, migration 101), which
// is the only place it can survive a restart, so the task read has to reach for
// it. Guarded on blocked_resource inside the subquery rather than joined
// unconditionally — a board is overwhelmingly tasks that are not parked, and
// they must not each pay a lookup into the run history to be told so. It matches
// the row TakeQuotaResumable checks against the clock (latest by updated_at), so
// the time the card shows is the time the sweeper will act on.
const boardTaskSelect = `
	SELECT bt.id, bt.repository_id, bt.task_number, bt.title, bt.task_type,
		bt.description, bt.technical_description, bt.initiative_project_id, bt.board_column,
		bt.position, bt.priority, bt.created_by, bt.assignee_agent_id,
		bt.created_at, bt.updated_at,
		bt.blocked_question, bt.blocked_session_id, bt.blocked_at, bt.blocked_origin_column,
		bt.blocked_resource,
		bt.clarification_session_id,
		bt.has_migration, bt.stage_verified_at, bt.verified_sha,
		bt.before_deploy, bt.after_deploy, bt.rollback_plan,
		bt.pr_url, bt.pr_number, bt.merge_commit_sha,
		sp.entered_at, qr.quota_resume_at
	FROM board_tasks bt
	LEFT JOIN LATERAL (
		SELECT entered_at FROM task_column_spans
		WHERE task_id = bt.id AND left_at IS NULL
		ORDER BY entered_at DESC LIMIT 1
	) sp ON true
	LEFT JOIN LATERAL (
		SELECT r.quota_resume_at FROM task_agent_runs r
		WHERE bt.blocked_resource = '` + domain.ResourceClaudeCodeQuota + `'
		  AND r.task_id = bt.id AND r.quota_resume_at IS NOT NULL
		ORDER BY r.updated_at DESC LIMIT 1
	) qr ON true
`

func scanBoardTask(scanner interface {
	Scan(dest ...any) error
}) (domain.BoardTask, error) {
	var task domain.BoardTask
	var col, taskType, priority string
	var blockedQuestion, blockedOrigin, blockedResource, prURL, mergeCommitSHA *string
	var prNumber *int
	err := scanner.Scan(
		&task.ID, &task.RepositoryID, &task.TaskNumber, &task.Title, &taskType,
		&task.Description, &task.TechnicalDescription, &task.InitiativeProjectID, &col,
		&task.Position, &priority, &task.CreatedBy, &task.AssigneeAgentID,
		&task.CreatedAt, &task.UpdatedAt,
		&blockedQuestion, &task.BlockedSessionID, &task.BlockedAt, &blockedOrigin,
		&blockedResource,
		&task.ClarificationSessionID,
		&task.HasMigration, &task.StageVerifiedAt, &task.VerifiedSHA,
		&task.BeforeDeploy, &task.AfterDeploy, &task.RollbackPlan,
		&prURL, &prNumber, &mergeCommitSHA,
		&task.ColumnEnteredAt, &task.BlockedResumeAt,
	)
	if err != nil {
		return domain.BoardTask{}, err
	}
	// The PR columns are nullable but the domain fields are plain values: NULL
	// and "" both mean "no PR known", so the scan targets are pointers only
	// because pgx needs them to be.
	if prURL != nil {
		task.PRURL = *prURL
	}
	if prNumber != nil {
		task.PRNumber = *prNumber
	}
	// Same contract as the PR columns above: NULL and "" both mean "not merged
	// from here", so the domain field stays a plain string.
	if mergeCommitSHA != nil {
		task.MergeCommitSHA = *mergeCommitSHA
	}
	if blockedQuestion != nil {
		task.BlockedQuestion = *blockedQuestion
	}
	if blockedOrigin != nil {
		task.BlockedOriginColumn = domain.TaskColumn(*blockedOrigin)
	}
	if blockedResource != nil {
		task.BlockedResource = *blockedResource
	}
	task.Column = domain.TaskColumn(col)
	task.TaskType = domain.TaskType(taskType)
	task.Priority = domain.TaskPriority(priority)
	// The key is derived, not stored: the prefix is the task's type, so a type
	// change renames the task rather than leaving a key that lies about it.
	task.Key = domain.FormatTaskKey(task.TaskType, task.TaskNumber)
	return task, nil
}

func (s *BoardTaskStore) Create(ctx context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	var created domain.BoardTask
	row := s.pool.QueryRow(ctx, `
		INSERT INTO board_tasks (
			repository_id, task_number, title, task_type, description, technical_description,
			initiative_project_id, board_column, position, priority, created_by, assignee_agent_id,
			before_deploy, after_deploy, rollback_plan
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+boardTaskColumns+`
	`, task.RepositoryID, task.TaskNumber, task.Title, string(task.TaskType),
		task.Description, task.TechnicalDescription, task.InitiativeProjectID, string(task.Column),
		task.Position, string(task.Priority), task.CreatedBy, task.AssigneeAgentID,
		task.BeforeDeploy, task.AfterDeploy, task.RollbackPlan)
	created, err := scanBoardTask(row)
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("create board task: %w", err)
	}
	return created, nil
}

// Get reads one task, scoped by repository — which is also the ownership check
// every task-scoped route relies on: a task id from another repository comes back
// missing rather than readable.
//
// "No such row" is reported as domain.ErrBoardTaskNotFound so a transport can
// answer 404 for it (the task-chat route does) without having to treat every
// store failure, including a database outage, as "missing".
func (s *BoardTaskStore) Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error) {
	row := s.pool.QueryRow(ctx, boardTaskSelect+` WHERE bt.id = $1 AND bt.repository_id = $2`, taskID, repositoryID)
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, fmt.Errorf("%w: %s", domain.ErrBoardTaskNotFound, taskID)
	}
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("get board task: %w", err)
	}
	return task, nil
}

func (s *BoardTaskStore) GetByNumber(ctx context.Context, number int) (domain.BoardTask, error) {
	row := s.pool.QueryRow(ctx, boardTaskSelect+` WHERE bt.task_number = $1`, number)
	task, err := scanBoardTask(row)
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("get board task by number: %w", err)
	}
	return task, nil
}

func (s *BoardTaskStore) LookupByKey(ctx context.Context, keyPrefix string, number int) (domain.BoardTask, error) {
	// The prefix names the type now, so the lookup is by type and number. An
	// unknown letter matches nothing rather than falling through to "task",
	// which would answer a lookup for "X-1" with T-1.
	taskType, ok := domain.TaskTypeForKeyPrefix(keyPrefix)
	if !ok {
		return domain.BoardTask{}, fmt.Errorf("lookup board task: %w", port.ErrNotFound)
	}
	row := s.pool.QueryRow(ctx, boardTaskSelect+`
		WHERE bt.task_number = $1 AND bt.task_type = $2
	`, number, string(taskType))
	task, err := scanBoardTask(row)
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("lookup board task: %w", err)
	}
	return task, nil
}

func (s *BoardTaskStore) ListByRepository(ctx context.Context, repositoryID uuid.UUID) ([]domain.BoardTask, error) {
	rows, err := s.pool.Query(ctx, boardTaskSelect+`
		WHERE bt.repository_id = $1
		ORDER BY bt.board_column, bt.position ASC, bt.created_at ASC
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list board tasks: %w", err)
	}
	defer rows.Close()
	return scanBoardTasks(rows)
}

func (s *BoardTaskStore) ListAll(ctx context.Context) ([]domain.BoardTask, error) {
	rows, err := s.pool.Query(ctx, boardTaskSelect+`
		ORDER BY bt.board_column, bt.position ASC, bt.created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list all board tasks: %w", err)
	}
	defer rows.Close()
	return scanBoardTasks(rows)
}

// ListBoardVisible is ListAll with the stale half of the released column left
// out: a released task is on the board until releasedCutoff, then only in the
// archive. COALESCE because a task released before column spans existed has no
// span row, and its last write is the closest thing to a release time.
func (s *BoardTaskStore) ListBoardVisible(ctx context.Context, releasedCutoff time.Time) ([]domain.BoardTask, error) {
	rows, err := s.pool.Query(ctx, boardTaskSelect+`
		WHERE bt.board_column <> $1 OR COALESCE(sp.entered_at, bt.updated_at) >= $2
		ORDER BY bt.board_column, bt.position ASC, bt.created_at ASC
	`, string(domain.TaskColumnReleased), releasedCutoff)
	if err != nil {
		return nil, fmt.Errorf("list board tasks: %w", err)
	}
	defer rows.Close()
	return scanBoardTasks(rows)
}

// ListReleasedArchive returns released tasks newest-released first. An empty
// query returns the most recent page; a query matches the board key, the title
// or the description, which is what someone looking for work that shipped
// months ago actually remembers.
func (s *BoardTaskStore) ListReleasedArchive(ctx context.Context, query string, limit int) ([]domain.BoardTask, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	trimmed := strings.TrimSpace(query)
	sql := boardTaskSelect + `
		WHERE bt.board_column = $1
	`
	args := []any{string(domain.TaskColumnReleased)}
	if trimmed != "" {
		// The key is not a column: it is the prefix plus the number, so the
		// number is matched on its own text and the caller's "T-12" still finds
		// T-12 through the title/description fallback.
		sql += ` AND (bt.title ILIKE $2 OR bt.description ILIKE $2 OR bt.task_number::text = $3)`
		args = append(args, "%"+trimmed+"%", strings.TrimLeft(taskNumberFromQuery(trimmed), "0"))
	}
	sql += fmt.Sprintf(`
		ORDER BY COALESCE(sp.entered_at, bt.updated_at) DESC
		LIMIT %d
	`, limit)
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list released tasks: %w", err)
	}
	defer rows.Close()
	return scanBoardTasks(rows)
}

// taskNumberFromQuery pulls the numeric half out of a board key so "T-12", "t-12"
// and "12" all find task 12. Anything else returns a value that matches no row.
func taskNumberFromQuery(query string) string {
	if idx := strings.LastIndex(query, "-"); idx >= 0 && idx+1 < len(query) {
		query = query[idx+1:]
	}
	for _, r := range query {
		if r < '0' || r > '9' {
			return "-1"
		}
	}
	if query == "" {
		return "-1"
	}
	return query
}

func scanBoardTasks(rows pgx.Rows) ([]domain.BoardTask, error) {
	var tasks []domain.BoardTask
	for rows.Next() {
		task, err := scanBoardTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// Update writes the task back, and clears a *cancellation* block when the task
// is moved out of the blocked column.
//
// A question block (blocked_session_id NOT NULL) is left alone exactly as
// before: it is released only by the answer arriving in TakeBlockedBySession,
// so an agent shuffling the task around cannot drop a pending question. A
// cancellation block has no session and no answer to wait for — a human moving
// the task back to a column IS the release, and without clearing the stamp here
// the board would keep showing a blocked badge on a task that is running again.
func (s *BoardTaskStore) Update(ctx context.Context, task domain.BoardTask) (domain.BoardTask, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE board_tasks SET
			title = $3,
			task_type = $4,
			description = $5,
			technical_description = $6,
			initiative_project_id = $7,
			board_column = $8,
			position = $9,
			priority = $10,
			assignee_agent_id = $11,
			blocked_question = CASE WHEN blocked_session_id IS NULL AND $8 <> $12::text
				THEN NULL ELSE blocked_question END,
			blocked_at = CASE WHEN blocked_session_id IS NULL AND $8 <> $12::text
				THEN NULL ELSE blocked_at END,
			blocked_origin_column = CASE WHEN blocked_session_id IS NULL AND $8 <> $12::text
				THEN NULL ELSE blocked_origin_column END,
			-- A resource block has no session either, so it releases the same
			-- way a cancellation does: a human dragging the card out of blocked
			-- is them saying they no longer want to wait for the device. Left
			-- set, the card would keep claiming to be queued for hardware while
			-- an agent works on it.
			blocked_resource = CASE WHEN blocked_session_id IS NULL AND $8 <> $12::text
				THEN NULL ELSE blocked_resource END,
			-- The release gate's stamp travels with the move that earns (or
			-- withdraws) it, so a task can never be left in done carrying the
			-- verified commit of an older sign-off. Service.UpdateTask is this
			-- store's only Update caller and always writes a task it just read,
			-- so a non-column update round-trips the stored value unchanged.
			verified_sha = $13,
			-- Written unconditionally like every other field above: UpdateTask
			-- always hands this store a task it just read, so a request that
			-- did not touch the runbook round-trips the stored value unchanged.
			before_deploy = $14,
			after_deploy = $15,
			rollback_plan = $16,
			updated_at = now()
		WHERE id = $1 AND repository_id = $2
		RETURNING `+boardTaskColumns+`
	`, task.ID, task.RepositoryID, task.Title, string(task.TaskType), task.Description,
		task.TechnicalDescription, task.InitiativeProjectID, string(task.Column),
		task.Position, string(task.Priority), task.AssigneeAgentID,
		string(domain.TaskColumnBlocked), task.VerifiedSHA,
		task.BeforeDeploy, task.AfterDeploy, task.RollbackPlan)
	updated, err := scanBoardTask(row)
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("update board task: %w", err)
	}
	return updated, nil
}

// BlockOnQuestion parks a task until a human answers in sessionID. Update()
// deliberately leaves these columns alone, so an agent moving the task around
// cannot clear a pending question by accident.
// The task also moves into the blocked column, remembering where it came from,
// so work waiting on a human drops out of the in-progress KPIs. Re-blocking an
// already-blocked task must not overwrite the origin with 'blocked'.
func (s *BoardTaskStore) BlockOnQuestion(ctx context.Context, repositoryID, taskID, sessionID uuid.UUID, question string) error {
	// clarification_session_id is set here and never cleared: it is the task's
	// permanent question thread, whereas blocked_session_id only lives until the
	// answer arrives.
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks
		SET blocked_question         = $3,
		    blocked_session_id       = $4,
		    clarification_session_id = $4,
		    blocked_at               = now(),
		    blocked_origin_column    = CASE WHEN board_column = $5 THEN blocked_origin_column ELSE board_column END,
		    board_column             = $5,
		    updated_at               = now()
		WHERE id = $1 AND repository_id = $2
	`, taskID, repositoryID, question, sessionID, string(domain.TaskColumnBlocked))
	if err != nil {
		return fmt.Errorf("block board task: %w", err)
	}
	return nil
}

// BlockOnCancel parks a task whose run a human stopped. Mirrors
// BlockOnQuestion apart from the session: there is nobody to answer, so
// blocked_session_id stays NULL and the release is a human moving the task to a
// column (see Update). The origin column is remembered the same way, so the
// board can still show where the work was stopped.
func (s *BoardTaskStore) BlockOnCancel(ctx context.Context, repositoryID, taskID uuid.UUID, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks
		SET blocked_question      = $3,
		    blocked_at            = now(),
		    blocked_origin_column = CASE WHEN board_column = $4 THEN blocked_origin_column ELSE board_column END,
		    board_column          = $4,
		    updated_at            = now()
		WHERE id = $1 AND repository_id = $2
	`, taskID, repositoryID, reason, string(domain.TaskColumnBlocked))
	if err != nil {
		return fmt.Errorf("block board task on cancel: %w", err)
	}
	return nil
}

// BlockOnResource parks a task whose run stopped because a shared resource —
// the test device — was held by another run. Mirrors BlockOnCancel: no session,
// because there is nobody to answer. What makes it its own state is the release:
// blocked_resource is the key the device sweeper claims by, so the task resumes
// when the hardware frees up rather than when a human notices.
//
// It returns the column the task was in BEFORE the park, because the caller has
// to write a task.moved event for it and there is no second read that can be
// trusted to answer: by the time the park has landed the row says `blocked`, and
// a SELECT before the UPDATE would race any other mover. The pre-UPDATE value
// comes out of a CTE rather than a plain RETURNING for the same reason
// TakeBlockedByResource reads its snapshot that way — RETURNING hands back the
// row as it now stands, which is the one column that is no longer interesting —
// and it stays ONE statement, so the park is as atomic as it was before.
//
// An empty column means the task no longer exists (deleted mid-run). That was
// silently a no-op when this ran through Exec and stays one here.
func (s *BoardTaskStore) BlockOnResource(ctx context.Context, repositoryID, taskID uuid.UUID, resource, detail string) (domain.TaskColumn, error) {
	if !domain.ValidResource(resource) {
		return "", fmt.Errorf("unknown blocking resource %q", resource)
	}
	var previous string
	err := s.pool.QueryRow(ctx, `
		WITH prev AS (
			SELECT id, board_column FROM board_tasks
			WHERE id = $1 AND repository_id = $2
		)
		UPDATE board_tasks bt
		SET blocked_question      = $3,
		    blocked_resource      = $4,
		    blocked_at            = now(),
		    blocked_origin_column = CASE WHEN bt.board_column = $5 THEN bt.blocked_origin_column ELSE bt.board_column END,
		    board_column          = $5,
		    updated_at            = now()
		FROM prev
		WHERE bt.id = prev.id
		RETURNING prev.board_column
	`, taskID, repositoryID, detail, resource, string(domain.TaskColumnBlocked)).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("block board task on resource: %w", err)
	}
	return domain.TaskColumn(previous), nil
}

// MarkWorkOrderWaiting parks a task on the work_order resource WITHOUT moving
// board_column — the task stays exactly where it already is (todo or
// in_progress). Everything else about the write mirrors BlockOnResource:
// same columns except board_column/blocked_origin_column, same no-op-on-a-
// missing-task behaviour.
func (s *BoardTaskStore) MarkWorkOrderWaiting(ctx context.Context, repositoryID, taskID uuid.UUID, detail string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks
		SET blocked_question = $3,
		    blocked_resource = $4,
		    blocked_at       = now(),
		    updated_at       = now()
		WHERE id = $1 AND repository_id = $2
	`, taskID, repositoryID, detail, domain.ResourceWorkOrder)
	if err != nil {
		return fmt.Errorf("mark board task waiting on work order: %w", err)
	}
	return nil
}

// ClearWorkOrderWaiting releases a work_order park set by MarkWorkOrderWaiting,
// also without touching board_column — the column was never moved, so there is
// nothing to restore it to.
func (s *BoardTaskStore) ClearWorkOrderWaiting(ctx context.Context, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	row := s.pool.QueryRow(ctx, `
		WITH cleared AS (
			UPDATE board_tasks bt
			SET blocked_question = NULL,
			    blocked_resource = NULL,
			    blocked_at       = NULL,
			    updated_at       = now()
			WHERE bt.id = $1 AND bt.blocked_resource = $2
			RETURNING bt.id
		)
		`+boardTaskSelect+`
		WHERE bt.id = (SELECT id FROM cleared)
	`, taskID, domain.ResourceWorkOrder)
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, fmt.Errorf("clear work order wait: %w", err)
	}
	return task, true, nil
}

// TakeBlockedByResource claims the task that has waited longest on resource and
// returns it with the block cleared, exactly as TakeBlockedBySession does for
// an answered question — including the pre-UPDATE snapshot trick, so the caller
// still sees what the task was waiting for.
//
// It takes ONE task per call by design. The resource it releases is a single
// device: waking every parked task at once would put them all straight back
// into contention, and the loser of that race would be re-blocked having spent
// a run's setup for nothing. The sweeper calls this again on its next pass.
//
// Oldest first (blocked_at, then created_at) so a task cannot be starved by
// newer arrivals; FOR UPDATE SKIP LOCKED so two processes sweeping at once
// claim different tasks rather than the same one twice.
func (s *BoardTaskStore) TakeBlockedByResource(ctx context.Context, resource string) (domain.BoardTask, bool, error) {
	row := s.pool.QueryRow(ctx, `
		WITH claimed AS (
			SELECT id FROM board_tasks
			WHERE blocked_resource = $1 AND blocked_at IS NOT NULL
			ORDER BY blocked_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), cleared AS (
			UPDATE board_tasks bt
			SET blocked_question      = NULL,
			    blocked_resource      = NULL,
			    blocked_at            = NULL,
			    board_column          = COALESCE(NULLIF(bt.blocked_origin_column, ''), $2),
			    blocked_origin_column = NULL,
			    updated_at            = now()
			FROM claimed c WHERE bt.id = c.id
			RETURNING bt.id
		)
		`+boardTaskSelect+`
		WHERE bt.id = (SELECT id FROM claimed)
	`, resource, string(domain.TaskColumnTodo))
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, fmt.Errorf("take blocked board task by resource: %w", err)
	}
	// Same as TakeBlockedBySession: the snapshot still shows 'blocked', and
	// callers dispatch off Column to pick the agents for the stage.
	if task.BlockedOriginColumn != "" {
		task.Column = task.BlockedOriginColumn
	} else {
		task.Column = domain.TaskColumnTodo
	}
	return task, true, nil
}

// TakeQuotaResumable claims one task parked on the Claude Code usage limit
// whose reset time has passed, and returns it with the block cleared.
//
// It is TakeBlockedByResource with the clock as its probe. The device sweeper
// can ask the hardware whether it is free before claiming anything; nobody can
// ask a subscription whether it has reset, so the run that parked recorded when
// it expects to (task_agent_runs.quota_resume_at, migration 101) and the due
// check is that recorded time — which is why the claim and the check have to be
// one statement here rather than a probe followed by a take.
//
// The due row is the task's LATEST parked run, not any of them: a task parked
// twice keeps both rows, and matching on "any run is due" would resume on the
// FIRST park's expiry while the second one is still in force.
//
// The COALESCE is the escape hatch for a park with no recorded time at all. A
// NULL there compares to nothing, so the row would never be due and the card
// would sit in blocked until a human dragged it out — an unreleasable state
// created by any future caller that parks on this resource without stamping the
// run (a partially-written row, a code path added later that forgets). Falling
// back to blocked_at + the default window means the worst case is one early
// resume that re-parks, instead of a task lost to the board.
//
// One task per CALL, oldest first, FOR UPDATE SKIP LOCKED — all for the reasons
// on TakeBlockedByResource, and it is what keeps two pods sweeping at once from
// resuming the same card. It is not a limit on the sweep: QuotaSweeper.sweep
// calls this until it comes back empty (or hits its own batch cap), because
// every task whose recorded reset has passed became due at the same instant and
// there is no contention between them to pace.
func (s *BoardTaskStore) TakeQuotaResumable(ctx context.Context, now time.Time) (domain.BoardTask, bool, error) {
	row := s.pool.QueryRow(ctx, `
		WITH claimed AS (
			SELECT bt.id FROM board_tasks bt
			WHERE bt.blocked_resource = $1 AND bt.blocked_at IS NOT NULL
			  AND COALESCE((
				SELECT r.quota_resume_at FROM task_agent_runs r
				WHERE r.task_id = bt.id AND r.quota_resume_at IS NOT NULL
				ORDER BY r.updated_at DESC LIMIT 1
			  ), bt.blocked_at + interval '30 minutes') <= $2
			ORDER BY bt.blocked_at, bt.created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), cleared AS (
			UPDATE board_tasks bt
			SET blocked_question      = NULL,
			    blocked_resource      = NULL,
			    blocked_at            = NULL,
			    board_column          = COALESCE(NULLIF(bt.blocked_origin_column, ''), $3),
			    blocked_origin_column = NULL,
			    updated_at            = now()
			FROM claimed c WHERE bt.id = c.id
			RETURNING bt.id
		)
		`+boardTaskSelect+`
		WHERE bt.id = (SELECT id FROM claimed)
	`, domain.ResourceClaudeCodeQuota, now, string(domain.TaskColumnTodo))
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, fmt.Errorf("take quota-parked board task: %w", err)
	}
	// The pre-UPDATE snapshot still reads 'blocked'; callers dispatch off Column
	// to pick the agents for the stage, so it is restored here exactly as
	// TakeBlockedByResource does.
	if task.BlockedOriginColumn != "" {
		task.Column = task.BlockedOriginColumn
	} else {
		task.Column = domain.TaskColumnTodo
	}
	return task, true, nil
}

// TakeBlockedBySession clears the block on the task waiting for an answer in
// sessionID and returns it, question included.
//
// FOR UPDATE SKIP LOCKED is the claim: two answers arriving at once contend for
// the row and the loser sees no rows, so the task is re-dispatched exactly once.
// The final SELECT reads the pre-UPDATE snapshot on purpose — that is the only
// way to hand back blocked_question, which the UPDATE has just nulled out (a
// plain UPDATE ... RETURNING would hand back the NULL). The data-modifying CTE
// still runs to completion whether or not the outer query reads its output.
func (s *BoardTaskStore) TakeBlockedBySession(ctx context.Context, sessionID uuid.UUID) (domain.BoardTask, bool, error) {
	row := s.pool.QueryRow(ctx, `
		WITH claimed AS (
			SELECT id FROM board_tasks
			WHERE blocked_session_id = $1 AND blocked_at IS NOT NULL
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), cleared AS (
			UPDATE board_tasks bt
			SET blocked_question      = NULL,
			    blocked_session_id    = NULL,
			    blocked_at            = NULL,
			    board_column          = COALESCE(NULLIF(bt.blocked_origin_column, ''), $2),
			    blocked_origin_column = NULL,
			    updated_at            = now()
			FROM claimed c WHERE bt.id = c.id
			RETURNING bt.id
		)
		`+boardTaskSelect+`
		WHERE bt.id = (SELECT id FROM claimed)
	`, sessionID, string(domain.TaskColumnTodo))
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, fmt.Errorf("take blocked board task: %w", err)
	}
	// The outer SELECT reads the pre-UPDATE snapshot, so it still shows the task
	// parked in 'blocked'. Callers dispatch off Column to pick the agents for the
	// stage, so hand back the column the task was actually restored to.
	if task.BlockedOriginColumn != "" {
		task.Column = task.BlockedOriginColumn
	} else {
		task.Column = domain.TaskColumnTodo
	}
	return task, true, nil
}

func (s *BoardTaskStore) Delete(ctx context.Context, repositoryID, taskID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM board_tasks WHERE id = $1 AND repository_id = $2`, taskID, repositoryID)
	if err != nil {
		return fmt.Errorf("delete board task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("board task not found")
	}
	return nil
}

// MarkCompleted stamps the terminal verdict time-based KPIs read: when the task
// finished, and whether it got there without rework.
func (s *BoardTaskStore) MarkCompleted(ctx context.Context, taskID uuid.UUID, clean bool, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks SET clean_completion = $2, completed_at = $3, updated_at = now()
		WHERE id = $1
	`, taskID, clean, at)
	if err != nil {
		return fmt.Errorf("mark task completed: %w", err)
	}
	return nil
}

// SetMigrationFlag records whether the task's diff contains a schema change.
// It is written from the detected file list, so an agent cannot opt out of the
// stage gate by not mentioning its migration.
func (s *BoardTaskStore) SetMigrationFlag(ctx context.Context, taskID uuid.UUID, hasMigration bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks SET has_migration = $2, updated_at = now() WHERE id = $1
	`, taskID, hasMigration)
	if err != nil {
		return fmt.Errorf("set task migration flag: %w", err)
	}
	return nil
}

// SetTaskPullRequest records the pull request the task's branch is reviewed in.
//
// Deliberately not part of Update (like has_migration and stage_verified_at,
// which also have their own setters): Update is handed a task a caller read
// earlier, and folding the PR into it would let a stale read wipe a link that was
// written in between.
//
// A URL that did not parse arrives with number 0 and is stored as NULL rather
// than 0, so "PR #0" can never be rendered; the URL itself is kept either way.
func (s *BoardTaskStore) SetTaskPullRequest(ctx context.Context, taskID uuid.UUID, url string, number int) error {
	var prNumber *int
	if number > 0 {
		prNumber = &number
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks SET pr_url = $2, pr_number = $3, updated_at = now() WHERE id = $1
	`, taskID, url, prNumber)
	if err != nil {
		return fmt.Errorf("set task pull request: %w", err)
	}
	return nil
}

// SetTaskMergeCommit records the squash commit the task's pull request produced
// on the default branch.
//
// Its own setter for the same reason SetTaskPullRequest is: it is written after
// an irreversible action by a caller holding a task it read minutes earlier, and
// folding it into Update would let that stale read overwrite the record of a
// merge — the one field whose loss would let the dispatcher wake QA to merge a
// PR that is already merged.
//
// The empty string is stored as NULL so "" and "never merged" stay the same
// state on both sides of the wire.
func (s *BoardTaskStore) SetTaskMergeCommit(ctx context.Context, taskID uuid.UUID, sha string) error {
	var value *string
	if trimmed := strings.TrimSpace(sha); trimmed != "" {
		value = &trimmed
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks SET merge_commit_sha = $2, updated_at = now() WHERE id = $1
	`, taskID, value)
	if err != nil {
		return fmt.Errorf("set task merge commit: %w", err)
	}
	return nil
}

// FindTaskByMergeCommit resolves the task whose merge produced sha — the
// reverse of SetTaskMergeCommit, asked from production's end when an
// environment is unhealthy and the only thing known about it is which commit it
// is running.
//
// Scoped to the repository, so a SHA guessed or replayed from elsewhere cannot
// surface another repository's card. Backed by the partial index migration 105
// adds; without it this is a sequential scan of every task, run from the
// incident ingest path.
func (s *BoardTaskStore) FindTaskByMergeCommit(ctx context.Context, repositoryID uuid.UUID, sha string) (domain.BoardTask, error) {
	trimmed := strings.TrimSpace(sha)
	if trimmed == "" {
		return domain.BoardTask{}, port.ErrNotFound
	}
	row := s.pool.QueryRow(ctx, boardTaskSelect+`
		WHERE bt.repository_id = $1 AND bt.merge_commit_sha = $2
		ORDER BY bt.updated_at DESC LIMIT 1
	`, repositoryID, trimmed)
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, port.ErrNotFound
	}
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("find board task by merge commit: %w", err)
	}
	return task, nil
}

// ListBlockedByResource reads (and claims nothing of) the tasks parked on a
// resource. It is the deploy sweeper's first half: it has to ask about each
// parked task individually before it knows which one is ready, and claiming
// them to find out would unpark tasks whose deploy is still running.
func (s *BoardTaskStore) ListBlockedByResource(ctx context.Context, resource string, limit int) ([]domain.BoardTask, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, boardTaskSelect+`
		WHERE bt.blocked_resource = $1 AND bt.blocked_at IS NOT NULL
		ORDER BY bt.blocked_at, bt.created_at
		LIMIT $2
	`, resource, limit)
	if err != nil {
		return nil, fmt.Errorf("list blocked board tasks by resource: %w", err)
	}
	defer rows.Close()
	tasks, err := scanBoardTasks(rows)
	if err != nil {
		return nil, err
	}
	// The rows still read 'blocked'; callers dispatch off Column to pick the
	// agents for the stage, exactly as TakeBlockedByResource restores it. A
	// work_order park never wrote 'blocked' in the first place — restoring it
	// here would report an in_progress task as todo.
	if resource != domain.ResourceWorkOrder {
		for i := range tasks {
			tasks[i].Column = restoreBlockedOriginColumn(tasks[i])
		}
	}
	return tasks, nil
}

// TakeBlockedResourceTask claims ONE named parked task and returns it with the
// block cleared.
//
// It is TakeBlockedByResource narrowed from "oldest" to "this one". The deploy
// watch needs that narrowing because its park is per-task: the oldest parked
// task's deploy may still be running while a newer one's has already failed,
// and claiming the wrong one would resume a run that can only park again.
//
// The resource is still part of the WHERE clause even though the id alone
// identifies the row — so a stale caller cannot unpark a task that has since
// been re-parked on something else (a question, a device) by naming an id it
// remembers.
func (s *BoardTaskStore) TakeBlockedResourceTask(ctx context.Context, resource string, taskID uuid.UUID) (domain.BoardTask, bool, error) {
	row := s.pool.QueryRow(ctx, `
		WITH claimed AS (
			SELECT id FROM board_tasks
			WHERE id = $2 AND blocked_resource = $1 AND blocked_at IS NOT NULL
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		), cleared AS (
			UPDATE board_tasks bt
			SET blocked_question      = NULL,
			    blocked_resource      = NULL,
			    blocked_at            = NULL,
			    board_column          = COALESCE(NULLIF(bt.blocked_origin_column, ''), $3),
			    blocked_origin_column = NULL,
			    updated_at            = now()
			FROM claimed c WHERE bt.id = c.id
			RETURNING bt.id
		)
		`+boardTaskSelect+`
		WHERE bt.id = (SELECT id FROM claimed)
	`, resource, taskID, string(domain.TaskColumnTodo))
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, false, nil
	}
	if err != nil {
		return domain.BoardTask{}, false, fmt.Errorf("take blocked board task by id: %w", err)
	}
	task.Column = restoreBlockedOriginColumn(task)
	return task, true, nil
}

// restoreBlockedOriginColumn returns the column a parked task belongs in. The
// pre-UPDATE snapshot every claim reads still says 'blocked', and callers
// dispatch off Column to pick the agents for the stage.
func restoreBlockedOriginColumn(task domain.BoardTask) domain.TaskColumn {
	if task.BlockedOriginColumn != "" {
		return task.BlockedOriginColumn
	}
	return domain.TaskColumnTodo
}

// MarkStageVerified stamps the successful stage deploy that proved this task's
// change (migration included) applies to a real environment.
func (s *BoardTaskStore) MarkStageVerified(ctx context.Context, taskID uuid.UUID, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks SET stage_verified_at = $2, updated_at = now() WHERE id = $1
	`, taskID, at)
	if err != nil {
		return fmt.Errorf("mark task stage verified: %w", err)
	}
	return nil
}

// ClearStageVerification removes the stage stamp so the migration gate blocks
// again until a fresh stage deploy proves the task's current commits.
func (s *BoardTaskStore) ClearStageVerification(ctx context.Context, taskID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE board_tasks SET stage_verified_at = NULL, updated_at = now() WHERE id = $1
	`, taskID)
	if err != nil {
		return fmt.Errorf("clear task stage verification: %w", err)
	}
	return nil
}

// ClaimAssignee assigns the task to agentID if nobody else holds it. The WHERE
// clause is the whole claim — unassigned or already ours — so a competing
// claim loses at the row, not in application code.
//
// "No rows" is not one answer. It used to surface verbatim ("no rows in result
// set") and the agent could not tell a task that does not exist from one a
// colleague had just taken; the refusal is now typed by re-reading the row.
func (s *BoardTaskStore) ClaimAssignee(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) (domain.BoardTask, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE board_tasks SET assignee_agent_id = $3, updated_at = now()
		WHERE id = $1 AND repository_id = $2 AND (assignee_agent_id IS NULL OR assignee_agent_id = $3)
		RETURNING `+boardTaskColumns+`
	`, taskID, repositoryID, agentID)
	task, err := scanBoardTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BoardTask{}, s.explainClaimRefusal(ctx, repositoryID, taskID, agentID)
	}
	if err != nil {
		return domain.BoardTask{}, fmt.Errorf("claim assignee: %w", err)
	}
	return task, nil
}

// explainClaimRefusal names why the claim update matched nothing: the task is
// not in this repository (domain.ErrBoardTaskNotFound, via Get) or another
// agent holds it (domain.ErrTaskAlreadyClaimed). A row that reads as claimable
// on the re-read was released between the two statements; the caller can
// simply claim again.
func (s *BoardTaskStore) explainClaimRefusal(ctx context.Context, repositoryID, taskID, agentID uuid.UUID) error {
	current, err := s.Get(ctx, repositoryID, taskID)
	if err != nil {
		return fmt.Errorf("claim assignee: %w", err)
	}
	if current.AssigneeAgentID != nil && *current.AssigneeAgentID != agentID {
		return fmt.Errorf("claim assignee: %w: %s", domain.ErrTaskAlreadyClaimed, current.Key)
	}
	return fmt.Errorf("claim assignee: task %s changed while it was being claimed; retry", current.Key)
}

// NextTaskNumber hands out the next number for a task type and never hands the
// same one out twice.
//
// It used to be MAX(task_number) + 1 over the live rows, which reused the
// number of a deleted task — and the key is what a branch, a PR title, a commit
// trailer and every chat refer to the task by, so two tasks answering to "T-7"
// is a bookkeeping bug with a long tail. The counter row survives the delete.
func (s *BoardTaskStore) NextTaskNumber(ctx context.Context, taskType domain.TaskType) (int, error) {
	var next int
	// One statement, so two concurrent creations of the same type serialise on
	// the counter row instead of racing to read the same maximum.
	err := s.pool.QueryRow(ctx, `
		INSERT INTO board_task_counters (task_type, last_number) VALUES ($1, 1)
		ON CONFLICT (task_type) DO UPDATE SET last_number = board_task_counters.last_number + 1
		RETURNING last_number
	`, string(taskType)).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("next task number: %w", err)
	}
	return next, nil
}

type AcceptanceCriterionStore struct {
	pool *DB
}

func NewAcceptanceCriterionStore(pool *DB) *AcceptanceCriterionStore {
	return &AcceptanceCriterionStore{pool: pool}
}

const criterionColumns = `id, task_id, text, position, completed, canceled, cancel_reason, created_at`

// ReplaceForTask diffs the incoming texts against the task's current
// criteria instead of deleting and reinserting every row: a criterion whose
// text is unchanged keeps its id, its completed/canceled state and any
// review verdict recorded on it (task_criterion_checks cascades on delete,
// so reinserting it would silently wipe QA/PM sign-off for no reason). Only
// a criterion genuinely absent from the incoming list is deleted, and only a
// genuinely new text gets a fresh row — that is what lets an agent resend
// "the full list" after an unrelated edit without invalidating ids it is
// still holding.
func (s *AcceptanceCriterionStore) ReplaceForTask(ctx context.Context, taskID uuid.UUID, items []domain.AcceptanceCriterionInput) ([]domain.AcceptanceCriterion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT `+criterionColumns+`
		FROM task_acceptance_criteria WHERE task_id = $1
	`, taskID)
	if err != nil {
		return nil, err
	}
	existingByText := make(map[string]domain.AcceptanceCriterion)
	for rows.Next() {
		var c domain.AcceptanceCriterion
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Text, &c.Position, &c.Completed, &c.Canceled, &c.CancelReason, &c.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		existingByText[c.Text] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	matched := make(map[string]bool, len(items))
	out := make([]domain.AcceptanceCriterion, 0, len(items))
	for i, item := range items {
		pos := item.Position
		if pos == 0 {
			pos = i
		}
		if current, ok := existingByText[item.Text]; ok {
			matched[item.Text] = true
			if current.Position != pos {
				if _, err := tx.Exec(ctx, `UPDATE task_acceptance_criteria SET position = $2 WHERE id = $1`, current.ID, pos); err != nil {
					return nil, fmt.Errorf("update acceptance criterion position: %w", err)
				}
				current.Position = pos
			}
			out = append(out, current)
			continue
		}
		var c domain.AcceptanceCriterion
		err := tx.QueryRow(ctx, `
			INSERT INTO task_acceptance_criteria (task_id, text, position, completed)
			VALUES ($1, $2, $3, $4)
			RETURNING `+criterionColumns, taskID, item.Text, pos, item.Completed).Scan(
			&c.ID, &c.TaskID, &c.Text, &c.Position, &c.Completed, &c.Canceled, &c.CancelReason, &c.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("insert acceptance criterion: %w", err)
		}
		out = append(out, c)
	}

	for text, c := range existingByText {
		if matched[text] {
			continue
		}
		if _, err := tx.Exec(ctx, `DELETE FROM task_acceptance_criteria WHERE id = $1`, c.ID); err != nil {
			return nil, fmt.Errorf("delete acceptance criterion: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *AcceptanceCriterionStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AcceptanceCriterion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+criterionColumns+`
		FROM task_acceptance_criteria WHERE task_id = $1 ORDER BY position ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []domain.AcceptanceCriterion
	for rows.Next() {
		var c domain.AcceptanceCriterion
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Text, &c.Position, &c.Completed, &c.Canceled, &c.CancelReason, &c.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachChecks(ctx, taskID, items); err != nil {
		return nil, err
	}
	return items, nil
}

// attachChecks hydrates the reviewer verdicts onto the task's criteria in one
// query, so every reader of the criteria list (board tools, HTTP handlers,
// move gates) sees the same per-role state without extra round trips.
func (s *AcceptanceCriterionStore) attachChecks(ctx context.Context, taskID uuid.UUID, items []domain.AcceptanceCriterion) error {
	if len(items) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ch.id, ch.criterion_id, ch.role, ch.agent_id, ch.approved, ch.note, ch.checked_at, ch.verified_sha
		FROM task_criterion_checks ch
		JOIN task_acceptance_criteria c ON c.id = ch.criterion_id
		WHERE c.task_id = $1
	`, taskID)
	if err != nil {
		return err
	}
	defer rows.Close()
	byCriterion := make(map[uuid.UUID][]domain.CriterionCheck)
	for rows.Next() {
		var ch domain.CriterionCheck
		if err := rows.Scan(&ch.ID, &ch.CriterionID, &ch.Role, &ch.AgentID, &ch.Approved, &ch.Note, &ch.CheckedAt, &ch.VerifiedSHA); err != nil {
			return err
		}
		byCriterion[ch.CriterionID] = append(byCriterion[ch.CriterionID], ch)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		items[i].Checks = byCriterion[items[i].ID]
	}
	return nil
}

func (s *AcceptanceCriterionStore) GetCriterion(ctx context.Context, criterionID uuid.UUID) (domain.AcceptanceCriterion, error) {
	var c domain.AcceptanceCriterion
	err := s.pool.QueryRow(ctx, `
		SELECT `+criterionColumns+`
		FROM task_acceptance_criteria WHERE id = $1
	`, criterionID).Scan(&c.ID, &c.TaskID, &c.Text, &c.Position, &c.Completed, &c.Canceled, &c.CancelReason, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AcceptanceCriterion{}, domain.ErrCriterionNotFound
		}
		return domain.AcceptanceCriterion{}, err
	}
	return c, nil
}

func (s *AcceptanceCriterionStore) UpsertCheck(ctx context.Context, check domain.CriterionCheck) (domain.CriterionCheck, error) {
	var out domain.CriterionCheck
	err := s.pool.QueryRow(ctx, `
		INSERT INTO task_criterion_checks (criterion_id, role, agent_id, approved, note, checked_at, verified_sha)
		VALUES ($1, $2, $3, $4, $5, now(), $6)
		ON CONFLICT (criterion_id, role) DO UPDATE
		SET agent_id = EXCLUDED.agent_id, approved = EXCLUDED.approved, note = EXCLUDED.note, checked_at = now(), verified_sha = EXCLUDED.verified_sha
		RETURNING id, criterion_id, role, agent_id, approved, note, checked_at, verified_sha
	`, check.CriterionID, check.Role, check.AgentID, check.Approved, check.Note, check.VerifiedSHA).Scan(
		&out.ID, &out.CriterionID, &out.Role, &out.AgentID, &out.Approved, &out.Note, &out.CheckedAt, &out.VerifiedSHA,
	)
	if err != nil {
		return domain.CriterionCheck{}, err
	}
	return out, nil
}

// UpdateCompleted ticks a criterion, and un-cancels it on the way: work that
// was actually done is the strongest possible retraction of "we dropped this",
// and leaving both flags set would show the card a struck-through line with a
// checkmark on it.
func (s *AcceptanceCriterionStore) UpdateCompleted(ctx context.Context, criterionID uuid.UUID, completed bool) (domain.AcceptanceCriterion, error) {
	var c domain.AcceptanceCriterion
	err := s.pool.QueryRow(ctx, `
		UPDATE task_acceptance_criteria
		SET completed = $2,
		    canceled = CASE WHEN $2 THEN false ELSE canceled END,
		    cancel_reason = CASE WHEN $2 THEN '' ELSE cancel_reason END
		WHERE id = $1
		RETURNING `+criterionColumns, criterionID, completed).
		Scan(&c.ID, &c.TaskID, &c.Text, &c.Position, &c.Completed, &c.Canceled, &c.CancelReason, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AcceptanceCriterion{}, domain.ErrCriterionNotFound
		}
		return domain.AcceptanceCriterion{}, err
	}
	return c, nil
}

func (s *AcceptanceCriterionStore) UpdateCanceled(ctx context.Context, criterionID uuid.UUID, canceled bool, reason string) (domain.AcceptanceCriterion, error) {
	var c domain.AcceptanceCriterion
	err := s.pool.QueryRow(ctx, `
		UPDATE task_acceptance_criteria
		SET canceled = $2,
		    cancel_reason = CASE WHEN $2 THEN $3 ELSE '' END,
		    completed = CASE WHEN $2 THEN false ELSE completed END
		WHERE id = $1
		RETURNING `+criterionColumns, criterionID, canceled, reason).
		Scan(&c.ID, &c.TaskID, &c.Text, &c.Position, &c.Completed, &c.Canceled, &c.CancelReason, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AcceptanceCriterion{}, domain.ErrCriterionNotFound
		}
		return domain.AcceptanceCriterion{}, err
	}
	return c, nil
}

type TaskRelationStore struct {
	pool *DB
}

func NewTaskRelationStore(pool *DB) *TaskRelationStore {
	return &TaskRelationStore{pool: pool}
}

func (s *TaskRelationStore) ReplaceForTask(ctx context.Context, sourceTaskID uuid.UUID, relations []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM task_relations WHERE source_task_id = $1`, sourceTaskID); err != nil {
		return nil, err
	}
	var out []domain.TaskRelation
	for _, rel := range relations {
		targetID := rel.TargetTaskID
		if targetID == uuid.Nil {
			continue
		}
		var r domain.TaskRelation
		err := tx.QueryRow(ctx, `
			INSERT INTO task_relations (source_task_id, target_task_id, relation_type)
			VALUES ($1, $2, $3)
			RETURNING id, source_task_id, target_task_id, relation_type, created_at
		`, sourceTaskID, targetID, string(rel.RelationType)).Scan(
			&r.ID, &r.SourceTaskID, &r.TargetTaskID, &r.RelationType, &r.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("insert task relation: %w", err)
		}
		out = append(out, r)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// ReplaceForTaskOfType swaps the task's relations of ONE type and leaves the
// rest alone. Editing deploy order through the full ReplaceForTask would delete
// the task's blocks relations as a side effect, because that method starts by
// clearing everything with this source.
func (s *TaskRelationStore) ReplaceForTaskOfType(ctx context.Context, sourceTaskID uuid.UUID, relationType domain.TaskRelationType, relations []domain.TaskRelationInput) ([]domain.TaskRelation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		DELETE FROM task_relations WHERE source_task_id = $1 AND relation_type = $2
	`, sourceTaskID, string(relationType)); err != nil {
		return nil, err
	}
	var out []domain.TaskRelation
	for _, rel := range relations {
		if rel.TargetTaskID == uuid.Nil {
			continue
		}
		var r domain.TaskRelation
		err := tx.QueryRow(ctx, `
			INSERT INTO task_relations (source_task_id, target_task_id, relation_type)
			VALUES ($1, $2, $3)
			ON CONFLICT (source_task_id, target_task_id, relation_type) DO NOTHING
			RETURNING id, source_task_id, target_task_id, relation_type, created_at
		`, sourceTaskID, rel.TargetTaskID, string(relationType)).Scan(
			&r.ID, &r.SourceTaskID, &r.TargetTaskID, &r.RelationType, &r.CreatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			// The same target listed twice in one payload. Not an error: the
			// caller asked for that dependency and it exists.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("insert task relation: %w", err)
		}
		out = append(out, r)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *TaskRelationStore) ListBySource(ctx context.Context, sourceTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tr.id, tr.source_task_id, tr.target_task_id, tr.relation_type, tr.created_at,
			`+taskKeySQL+`, bt.title
		FROM task_relations tr
		JOIN board_tasks bt ON bt.id = tr.target_task_id
		WHERE tr.source_task_id = $1
		ORDER BY tr.created_at
	`, sourceTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rels []domain.TaskRelation
	for rows.Next() {
		var r domain.TaskRelation
		if err := rows.Scan(&r.ID, &r.SourceTaskID, &r.TargetTaskID, &r.RelationType, &r.CreatedAt, &r.TargetKey, &r.TargetTitle); err != nil {
			return nil, err
		}
		rels = append(rels, r)
	}
	return rels, rows.Err()
}

// ListBlockedBy reads the relations pointing AT this task, with the source's key
// and title joined in — the far end here is the source, not the target, which is
// why it fills the SourceKey/SourceTitle pair instead.
//
// Unfiltered by column on purpose. ListBlockingSources answers "may this start"
// and so must exclude finished blockers; this answers "what is this task's
// order", which does not stop being true once the blocker lands.
func (s *TaskRelationStore) ListBlockedBy(ctx context.Context, targetTaskID uuid.UUID) ([]domain.TaskRelation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tr.id, tr.source_task_id, tr.target_task_id, tr.relation_type, tr.created_at,
			`+taskKeySQL+`, bt.title
		FROM task_relations tr
		JOIN board_tasks bt ON bt.id = tr.source_task_id
		WHERE tr.target_task_id = $1 AND tr.relation_type = 'blocks'
		ORDER BY tr.created_at
	`, targetTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rels []domain.TaskRelation
	for rows.Next() {
		var r domain.TaskRelation
		if err := rows.Scan(&r.ID, &r.SourceTaskID, &r.TargetTaskID, &r.RelationType, &r.CreatedAt, &r.SourceKey, &r.SourceTitle); err != nil {
			return nil, err
		}
		rels = append(rels, r)
	}
	return rels, rows.Err()
}

// AddBlockers records that each source must be finished before targetTaskID may
// be worked on.
//
// ON CONFLICT DO NOTHING rather than an error: declaring the same blocker twice
// (a retried tool call, a plan that lists it in two places) asked for a state
// that already holds. A self-edge is dropped here rather than left to the
// table's CHECK, so the caller gets the rest of its blockers written instead of
// a constraint violation that loses all of them.
func (s *TaskRelationStore) AddBlockers(ctx context.Context, targetTaskID uuid.UUID, sourceTaskIDs []uuid.UUID) ([]domain.TaskRelation, error) {
	if len(sourceTaskIDs) == 0 {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var out []domain.TaskRelation
	for _, sourceID := range sourceTaskIDs {
		if sourceID == uuid.Nil || sourceID == targetTaskID {
			continue
		}
		var r domain.TaskRelation
		err := tx.QueryRow(ctx, `
			INSERT INTO task_relations (source_task_id, target_task_id, relation_type)
			VALUES ($1, $2, 'blocks')
			ON CONFLICT (source_task_id, target_task_id, relation_type) DO NOTHING
			RETURNING id, source_task_id, target_task_id, relation_type, created_at
		`, sourceID, targetTaskID).Scan(
			&r.ID, &r.SourceTaskID, &r.TargetTaskID, &r.RelationType, &r.CreatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("insert blocking relation: %w", err)
		}
		out = append(out, r)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *TaskRelationStore) ListBlockingSources(ctx context.Context, targetTaskID uuid.UUID) ([]domain.BoardTask, error) {
	rows, err := s.pool.Query(ctx, boardTaskSelect+`
		JOIN task_relations tr ON tr.source_task_id = bt.id
		WHERE tr.target_task_id = $1 AND tr.relation_type = 'blocks'
		AND bt.board_column NOT IN ('done', 'released')
	`, targetTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBoardTasks(rows)
}

// ListUnfinishedBlockers is ListBlockingSources without the $1 — every unfinished
// `blocks` edge on the board in one query, keyed by SourceTaskID/TargetTaskID
// rather than by board task, which is what lets a caller answer "is task X ready"
// for the whole board with a single pass over the result instead of one query
// per candidate task.
func (s *TaskRelationStore) ListUnfinishedBlockers(ctx context.Context) ([]domain.TaskRelation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tr.id, tr.source_task_id, tr.target_task_id, tr.relation_type, tr.created_at,
			`+taskKeySQL+`, bt.title
		FROM task_relations tr
		JOIN board_tasks bt ON bt.id = tr.source_task_id
		WHERE tr.relation_type = 'blocks' AND bt.board_column NOT IN ('done', 'released')
		ORDER BY tr.created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rels []domain.TaskRelation
	for rows.Next() {
		var r domain.TaskRelation
		if err := rows.Scan(&r.ID, &r.SourceTaskID, &r.TargetTaskID, &r.RelationType, &r.CreatedAt, &r.SourceKey, &r.SourceTitle); err != nil {
			return nil, err
		}
		rels = append(rels, r)
	}
	return rels, rows.Err()
}

type TaskDocumentStore struct {
	pool *DB
}

func NewTaskDocumentStore(pool *DB) *TaskDocumentStore {
	return &TaskDocumentStore{pool: pool}
}

func (s *TaskDocumentStore) Create(ctx context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	var created domain.TaskDocument
	err := s.pool.QueryRow(ctx, `
		INSERT INTO task_documents (task_id, title, content, position, created_by_type, created_by_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, task_id, title, content, position, created_by_type, created_by_id, created_at, updated_at
	`, doc.TaskID, doc.Title, doc.Content, doc.Position, doc.CreatedByType, doc.CreatedByID).Scan(
		&created.ID, &created.TaskID, &created.Title, &created.Content, &created.Position,
		&created.CreatedByType, &created.CreatedByID, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return domain.TaskDocument{}, fmt.Errorf("create task document: %w", err)
	}
	return created, nil
}

func (s *TaskDocumentStore) Get(ctx context.Context, taskID, docID uuid.UUID) (domain.TaskDocument, error) {
	var doc domain.TaskDocument
	err := s.pool.QueryRow(ctx, `
		SELECT id, task_id, title, content, position, created_by_type, created_by_id, created_at, updated_at
		FROM task_documents WHERE id = $1 AND task_id = $2
	`, docID, taskID).Scan(
		&doc.ID, &doc.TaskID, &doc.Title, &doc.Content, &doc.Position,
		&doc.CreatedByType, &doc.CreatedByID, &doc.CreatedAt, &doc.UpdatedAt,
	)
	if err != nil {
		return domain.TaskDocument{}, fmt.Errorf("get task document: %w", err)
	}
	return doc, nil
}

func (s *TaskDocumentStore) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.TaskDocument, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, task_id, title, content, position, created_by_type, created_by_id, created_at, updated_at
		FROM task_documents WHERE task_id = $1 ORDER BY position ASC, created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var docs []domain.TaskDocument
	for rows.Next() {
		var doc domain.TaskDocument
		if err := rows.Scan(
			&doc.ID, &doc.TaskID, &doc.Title, &doc.Content, &doc.Position,
			&doc.CreatedByType, &doc.CreatedByID, &doc.CreatedAt, &doc.UpdatedAt,
		); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func (s *TaskDocumentStore) Update(ctx context.Context, doc domain.TaskDocument) (domain.TaskDocument, error) {
	var updated domain.TaskDocument
	err := s.pool.QueryRow(ctx, `
		UPDATE task_documents SET title = $3, content = $4, position = $5, updated_at = now()
		WHERE id = $1 AND task_id = $2
		RETURNING id, task_id, title, content, position, created_by_type, created_by_id, created_at, updated_at
	`, doc.ID, doc.TaskID, doc.Title, doc.Content, doc.Position).Scan(
		&updated.ID, &updated.TaskID, &updated.Title, &updated.Content, &updated.Position,
		&updated.CreatedByType, &updated.CreatedByID, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if err != nil {
		return domain.TaskDocument{}, fmt.Errorf("update task document: %w", err)
	}
	return updated, nil
}

func (s *TaskDocumentStore) Delete(ctx context.Context, taskID, docID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM task_documents WHERE id = $1 AND task_id = $2`, docID, taskID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task document not found")
	}
	return nil
}

type InitiativeProjectStore struct {
	pool *DB
}

func NewInitiativeProjectStore(pool *DB) *InitiativeProjectStore {
	return &InitiativeProjectStore{pool: pool}
}

func (s *InitiativeProjectStore) Create(ctx context.Context, name, description string) (domain.InitiativeProject, error) {
	var p domain.InitiativeProject
	err := s.pool.QueryRow(ctx, `
		INSERT INTO projects (name, description)
		VALUES ($1, $2)
		RETURNING id, name, description, created_at, updated_at
	`, name, description).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.InitiativeProject{}, fmt.Errorf("create initiative project: %w", err)
	}
	return p, nil
}

func (s *InitiativeProjectStore) Get(ctx context.Context, id uuid.UUID) (domain.InitiativeProject, error) {
	var p domain.InitiativeProject
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, created_at, updated_at FROM projects WHERE id = $1
	`, id).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.InitiativeProject{}, fmt.Errorf("get initiative project: %w", err)
	}
	return p, nil
}

func (s *InitiativeProjectStore) List(ctx context.Context) ([]domain.InitiativeProject, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM projects ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []domain.InitiativeProject
	for rows.Next() {
		var p domain.InitiativeProject
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *InitiativeProjectStore) Update(ctx context.Context, id uuid.UUID, name, description string) (domain.InitiativeProject, error) {
	var p domain.InitiativeProject
	err := s.pool.QueryRow(ctx, `
		UPDATE projects SET
			name = COALESCE(NULLIF($2, ''), name),
			description = COALESCE($3, description),
			updated_at = now()
		WHERE id = $1
		RETURNING id, name, description, created_at, updated_at
	`, id, name, description).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.InitiativeProject{}, fmt.Errorf("update initiative project: %w", err)
	}
	return p, nil
}

func (s *InitiativeProjectStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("initiative project not found")
	}
	return nil
}
