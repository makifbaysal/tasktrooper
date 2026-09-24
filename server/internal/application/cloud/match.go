package cloud

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const maxCandidates = 10

// mapSignalProvider is the only place a scanner deploy-signal provider string
// (see application/discovery/links/deploy.go) is translated into the cloud
// resource kinds MatchScan will look for. A provider absent here (fly,
// netlify, firebase, kubernetes, …) records nothing: this package only spans
// vercel/gcp/aws.
func mapSignalProvider(p string) (domain.CloudProviderKind, []domain.CloudResourceKind, bool) {
	switch p {
	case "vercel":
		return domain.CloudVercel, []domain.CloudResourceKind{domain.CloudResourceVercelProject}, true
	case "gcp_cloud_run":
		return domain.CloudGCP, []domain.CloudResourceKind{domain.CloudResourceCloudRunService, domain.CloudResourceCloudRunJob}, true
	case "aws_ecs":
		return domain.CloudAWS, []domain.CloudResourceKind{domain.CloudResourceECSService}, true
	case "aws_lambda":
		return domain.CloudAWS, []domain.CloudResourceKind{domain.CloudResourceLambdaFunction}, true
	case "aws_app_runner":
		return domain.CloudAWS, []domain.CloudResourceKind{domain.CloudResourceAppRunnerService}, true
	}
	return "", nil, false
}

type envKey struct {
	component uuid.UUID
	env       domain.DeployEnvironment
}

// matchableForRematch is the "never touch" rule: a human-owned row (Source
// user), a dismissed row, or a row a human confirmed by hand (confirmed but
// not AutoConfirmed) is left exactly as it is by every later MatchScan.
func matchableForRematch(row domain.ComponentEnvironment) bool {
	if row.Source == domain.LinkSourceUser {
		return false
	}
	if row.Status == domain.LinkDismissed {
		return false
	}
	if row.Status == domain.LinkConfirmed && !row.AutoConfirmed {
		return false
	}
	return true
}

// MatchScan turns one scan's deploy signals into suggested or auto-confirmed
// environment bindings. It is nil-safe to call with no signals (a no-op) and
// idempotent: rerunning it against the same signals reproduces the same rows,
// because SaveEnvironment upserts by (component, environment).
func (s *Service) MatchScan(ctx context.Context, repoID uuid.UUID, result domain.ScanResult) error {
	if len(result.DeploySignals) == 0 {
		return nil
	}
	repo, err := s.repos.Get(ctx, repoID)
	if err != nil {
		return fmt.Errorf("match scan: %w", err)
	}
	components, err := s.components.ListComponents(ctx, repoID)
	if err != nil {
		return fmt.Errorf("match scan: %w", err)
	}
	existing, err := s.environments.ListEnvironments(ctx, repoID)
	if err != nil {
		return fmt.Errorf("match scan: %w", err)
	}

	componentByPath := make(map[string]domain.Component, len(components))
	for _, c := range components {
		if c.Status == domain.ComponentStatusActive {
			componentByPath[c.Path] = c
		}
	}
	existingByKey := make(map[envKey]domain.ComponentEnvironment, len(existing))
	for _, e := range existing {
		existingByKey[envKey{e.ComponentID, e.Environment}] = e
	}

	for _, signal := range result.DeploySignals {
		comp, ok := componentByPath[signal.ComponentPath]
		if !ok {
			continue
		}
		env := signal.Environment
		if env == "" {
			env = domain.EnvironmentProduction
		}
		providerKind, kinds, ok := mapSignalProvider(signal.Provider)
		if !ok {
			continue
		}

		key := envKey{comp.ID, env}
		row, hasExisting := existingByKey[key]
		if hasExisting && !matchableForRematch(row) {
			continue
		}

		outcome := s.matchSignal(ctx, providerKind, kinds, signal, comp, repo, result)
		next := applyMatchOutcome(row, hasExisting, repoID, comp.ID, env, providerKind, signal, outcome)
		saved, err := s.environments.SaveEnvironment(ctx, next)
		if err != nil {
			log.Warn().Err(err).Str("repository_id", repoID.String()).Str("component_path", comp.Path).Msg("cloud: match: saving environment failed")
			continue
		}
		existingByKey[key] = saved
	}

	if err := s.project(ctx, repoID); err != nil {
		log.Warn().Err(err).Str("repository_id", repoID.String()).Msg("cloud: match: projection failed")
	}
	s.triggerRelink()
	return nil
}

// triggerRelink runs Relink in the background under the service's own
// lifetime context, so a BindEnvironment/PatchEnvironment request or a scan's
// MatchScan call never waits on a re-scan of every other repository's
// dangling link targets.
func (s *Service) triggerRelink() {
	if s.relinker == nil {
		return
	}
	go func() {
		if err := s.relinker.Relink(s.bgCtx); err != nil {
			log.Warn().Err(err).Msg("cloud: relink after environment change failed")
		}
	}()
}

type matchOutcome struct {
	accountID     *uuid.UUID
	resource      *domain.CloudResourceRef
	url           string
	status        domain.LinkStatus
	confidence    domain.Confidence
	autoConfirmed bool
	candidates    []domain.CloudResource
	reason        string
}

func (s *Service) matchSignal(ctx context.Context, providerKind domain.CloudProviderKind, kinds []domain.CloudResourceKind, signal domain.DeploySignal, comp domain.Component, repo domain.Repository, result domain.ScanResult) matchOutcome {
	accounts, err := s.accounts.ListCloudAccounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: match: listing accounts failed")
	}
	hasAccount := false
	for _, a := range accounts {
		if a.Provider == providerKind && a.Status == domain.CloudAccountOK {
			hasAccount = true
			break
		}
	}
	if !hasAccount {
		return matchOutcome{
			status:     domain.LinkSuggested,
			confidence: domain.ConfidenceMedium,
			reason:     fmt.Sprintf("connect a %s account", providerKind),
		}
	}

	resources := s.resourcesOfKinds(ctx, providerKind, kinds)
	var candidates []domain.CloudResource
	var tierExact bool
	switch providerKind {
	case domain.CloudVercel:
		candidates, tierExact = vercelCandidates(resources, signal, comp, result.Git.RemoteSlug, repo.Name)
	case domain.CloudGCP:
		candidates, tierExact = gcpCandidates(resources, signal)
	case domain.CloudAWS:
		candidates, tierExact = awsCandidates(resources, signal)
	}

	switch {
	case len(candidates) == 0:
		return matchOutcome{status: domain.LinkSuggested, confidence: domain.ConfidenceMedium, reason: "no matching resource found"}
	case tierExact && len(candidates) == 1:
		r := candidates[0]
		ref := r.Ref
		accountID := r.AccountID
		return matchOutcome{
			accountID:     &accountID,
			resource:      &ref,
			url:           r.URL,
			status:        domain.LinkConfirmed,
			confidence:    domain.ConfidenceExact,
			autoConfirmed: true,
		}
	default:
		return matchOutcome{status: domain.LinkSuggested, confidence: domain.ConfidenceMedium, candidates: capCandidates(candidates, maxCandidates)}
	}
}

func capCandidates(c []domain.CloudResource, n int) []domain.CloudResource {
	if len(c) <= n {
		return c
	}
	return c[:n]
}

func (s *Service) resourcesOfKinds(ctx context.Context, provider domain.CloudProviderKind, kinds []domain.CloudResourceKind) []domain.CloudResource {
	all := s.allResources(ctx, provider)
	kindSet := make(map[domain.CloudResourceKind]bool, len(kinds))
	for _, k := range kinds {
		kindSet[k] = true
	}
	out := make([]domain.CloudResource, 0, len(all))
	for _, r := range all {
		if kindSet[r.Ref.Kind] {
			out = append(out, r)
		}
	}
	return out
}

// vercelCandidates tries the project id ref first (an exact provider-native
// identifier), then the git repo + root directory the resource's own labels
// carry (exact once it resolves to exactly one resource), and only falls back
// to a bare name match (always medium) when neither resolves anything.
func vercelCandidates(resources []domain.CloudResource, signal domain.DeploySignal, comp domain.Component, remoteSlug, repoName string) ([]domain.CloudResource, bool) {
	if pid := signal.Ref["project_id"]; pid != "" {
		var exact []domain.CloudResource
		for _, r := range resources {
			if r.Ref.ID == pid {
				exact = append(exact, r)
			}
		}
		if len(exact) > 0 {
			return exact, true
		}
	}
	if remoteSlug != "" {
		var byRepo []domain.CloudResource
		for _, r := range resources {
			if strings.EqualFold(r.Labels["git_repo"], remoteSlug) && rootDirMatchesComponent(r.Labels["root_directory"], comp.Path) {
				byRepo = append(byRepo, r)
			}
		}
		if len(byRepo) > 0 {
			return byRepo, true
		}
	}
	name := comp.DisplayName()
	var byName []domain.CloudResource
	for _, r := range resources {
		if strings.EqualFold(r.Ref.Name, name) || strings.EqualFold(r.Ref.Name, repoName) {
			byName = append(byName, r)
		}
	}
	return byName, false
}

func rootDirMatchesComponent(rootDir, componentPath string) bool {
	rootDir = strings.Trim(rootDir, "/")
	if rootDir == "" {
		rootDir = "."
	}
	if componentPath == "" {
		componentPath = "."
	}
	return rootDir == componentPath
}

// gcpCandidates matches the deploy signal's service (+ region, when the
// signal and the resource both state one) against the resource's own name;
// it is the only tier for gcp, exact once it resolves to one resource.
func gcpCandidates(resources []domain.CloudResource, signal domain.DeploySignal) ([]domain.CloudResource, bool) {
	service := strings.TrimSpace(signal.Ref["service"])
	if service == "" {
		return nil, false
	}
	region := strings.TrimSpace(signal.Ref["region"])
	var out []domain.CloudResource
	for _, r := range resources {
		if !strings.EqualFold(r.Ref.Name, service) {
			continue
		}
		if region != "" && r.Ref.Region != "" && !strings.EqualFold(r.Ref.Region, region) {
			continue
		}
		out = append(out, r)
	}
	return out, true
}

// awsCandidates matches the ECS service / Lambda function name the scanner
// read (+ the ECS cluster name, when both sides state one — see
// application/discovery/links/deploy.go for the exact ref keys a signal
// carries) against the resource's own name; the cluster comparison is a soft
// disambiguator, not a requirement, since the adapter side may not always
// populate Extra with a cluster name.
func awsCandidates(resources []domain.CloudResource, signal domain.DeploySignal) ([]domain.CloudResource, bool) {
	name := firstNonEmpty(signal.Ref["service"], signal.Ref["function"], signal.Ref["family"])
	if name == "" {
		return nil, false
	}
	cluster := firstNonEmpty(signal.Ref["cluster"], signal.Ref["cluster_name"])
	var out []domain.CloudResource
	for _, r := range resources {
		if !strings.EqualFold(r.Ref.Name, name) {
			continue
		}
		if cluster != "" {
			if rc := firstNonEmpty(r.Ref.Extra["cluster"], r.Ref.Extra["cluster_name"]); rc != "" && !strings.EqualFold(rc, cluster) {
				continue
			}
		}
		out = append(out, r)
	}
	return out, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func applyMatchOutcome(existing domain.ComponentEnvironment, hasExisting bool, repoID, componentID uuid.UUID, env domain.DeployEnvironment, providerKind domain.CloudProviderKind, signal domain.DeploySignal, out matchOutcome) domain.ComponentEnvironment {
	row := existing
	if !hasExisting {
		row = domain.ComponentEnvironment{ID: uuid.New(), RepositoryID: repoID, ComponentID: componentID, Environment: env}
	}
	row.Provider = providerKind
	row.AccountID = out.accountID
	row.Resource = out.resource
	row.URL = out.url
	row.Status = out.status
	row.Source = domain.LinkSourceScan
	row.Confidence = out.confidence
	row.AutoConfirmed = out.autoConfirmed
	row.Candidates = out.candidates
	row.Reason = out.reason
	row.SignalKey = signalKey(signal)
	return row
}

// signalKey is the scan's natural key for this binding: provider + the
// signal's ref values, sorted, so a rescan with the same ref updates this row
// instead of leaving a stale duplicate.
func signalKey(signal domain.DeploySignal) string {
	keys := make([]string, 0, len(signal.Ref))
	for k := range signal.Ref {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+signal.Ref[k])
	}
	return signal.Provider + ":" + strings.Join(parts, ",")
}

// rematchAll re-runs MatchScan against every repository's latest scan result,
// used after an account is created or re-verified so a newly connected (or
// fixed) provider immediately resolves any environments that were waiting on it.
func (s *Service) rematchAll(ctx context.Context) {
	repos, err := s.repos.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: rematch all: listing repositories failed")
		return
	}
	for _, r := range repos {
		latest, err := s.scans.LatestScan(ctx, r.ID)
		if err != nil {
			if !errors.Is(err, port.ErrNotFound) {
				log.Warn().Err(err).Str("repository_id", r.ID.String()).Msg("cloud: rematch: latest scan lookup failed")
			}
			continue
		}
		full, err := s.scans.GetScan(ctx, latest.ID)
		if err != nil {
			log.Warn().Err(err).Str("repository_id", r.ID.String()).Msg("cloud: rematch: reading scan result failed")
			continue
		}
		if full.Result == nil {
			continue
		}
		if err := s.MatchScan(ctx, r.ID, *full.Result); err != nil {
			log.Warn().Err(err).Str("repository_id", r.ID.String()).Msg("cloud: rematch: match scan failed")
		}
	}
}
