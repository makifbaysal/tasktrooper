package cloud

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Boot carries an install's pre-cloud-accounts state forward: an existing
// Vercel/Google Cloud connection becomes a cloud_accounts row, and every
// legacy project/hosting/resource link or deploy target with an address
// becomes a confirmed environment. It runs in the background and is
// idempotent by construction — every step's own guard (an account of that
// provider already exists, a (component, environment) row already exists) is
// what "once" means here, not a call-site sync.Once — so calling it more than
// once, or racing it against a fresh scan's MatchScan, only ever no-ops on
// what already exists.
func (s *Service) Boot(ctx context.Context) {
	go s.boot(context.WithoutCancel(ctx))
}

func (s *Service) boot(ctx context.Context) {
	if s.legacy == nil {
		return
	}
	s.backfillAccounts(ctx)
	s.backfillEnvironmentsFromLegacyLinks(ctx)
	s.backfillEnvironmentsFromDeployTargets(ctx)
}

func (s *Service) backfillAccounts(ctx context.Context) {
	existing, err := s.accounts.ListCloudAccounts(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: backfill: listing accounts failed")
		return
	}
	hasProvider := make(map[domain.CloudProviderKind]bool, len(existing))
	for _, a := range existing {
		hasProvider[a.Provider] = true
	}

	if !hasProvider[domain.CloudVercel] {
		cred, ok, err := s.legacy.LegacyVercelCredential(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("cloud: backfill: reading legacy vercel credential failed")
		} else if ok && strings.TrimSpace(cred.Token) != "" {
			fields := map[string]string{"token": cred.Token}
			if cred.TeamID != "" {
				fields["team_id"] = cred.TeamID
			}
			s.backfillAccount(ctx, domain.CloudVercel, fields)
		}
	}

	if !hasProvider[domain.CloudGCP] {
		cred, ok, err := s.legacy.LegacyGCloudCredential(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("cloud: backfill: reading legacy gcloud credential failed")
		} else if ok {
			fields := cred.Fields
			if fields == nil {
				fields = map[string]string{}
			}
			s.backfillAccount(ctx, domain.CloudGCP, fields)
		}
	}
}

// backfillAccount verifies best-effort: unlike CreateAccount, a failed verify
// still stores the account (status unverified, the error recorded) rather
// than discarding a connection the operator already made and trusted.
func (s *Service) backfillAccount(ctx context.Context, providerKind domain.CloudProviderKind, fields map[string]string) {
	acct := domain.CloudAccount{Provider: providerKind, Label: string(providerKind)}

	if provider, ok := s.providerFor(providerKind); ok {
		if meta, err := provider.Verify(ctx, domain.CloudCredential{Provider: providerKind, Fields: fields}); err != nil {
			acct.Status = domain.CloudAccountUnverified
			acct.StatusDetail = err.Error()
		} else {
			now := s.now()
			acct.Status = domain.CloudAccountOK
			acct.Meta = meta
			acct.VerifiedAt = &now
			if label := defaultAccountLabel(providerKind, meta); label != "" {
				acct.Label = label
			}
		}
	} else {
		acct.Status = domain.CloudAccountUnverified
		acct.StatusDetail = "no adapter configured for this provider"
	}

	if _, err := s.accounts.CreateCloudAccount(ctx, acct, fields); err != nil {
		log.Warn().Err(err).Str("provider", string(providerKind)).Msg("cloud: backfill: creating account failed")
	}
}

type componentEnvKey struct {
	component uuid.UUID
	env       domain.DeployEnvironment
}

func (s *Service) existingEnvironmentKeys(ctx context.Context) (map[componentEnvKey]bool, error) {
	envs, err := s.environments.ListAllEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[componentEnvKey]bool, len(envs))
	for _, e := range envs {
		out[componentEnvKey{e.ComponentID, e.Environment}] = true
	}
	return out, nil
}

func (s *Service) findAccountID(ctx context.Context, provider domain.CloudProviderKind) *uuid.UUID {
	accounts, err := s.accounts.ListCloudAccounts(ctx)
	if err != nil {
		return nil
	}
	for _, a := range accounts {
		if a.Provider == provider {
			id := a.ID
			return &id
		}
	}
	return nil
}

type componentLister func(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error)

// cachingComponentLister memoizes ListComponents across the many legacy rows
// of the same repository the backfill walks.
func (s *Service) cachingComponentLister() componentLister {
	cache := map[uuid.UUID][]domain.Component{}
	return func(ctx context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
		if cs, ok := cache[repositoryID]; ok {
			return cs, nil
		}
		cs, err := s.components.ListComponents(ctx, repositoryID)
		if err != nil {
			return nil, err
		}
		cache[repositoryID] = cs
		return cs, nil
	}
}

func activeComponentsOnly(components []domain.Component) []domain.Component {
	out := make([]domain.Component, 0, len(components))
	for _, c := range components {
		if c.Status == domain.ComponentStatusActive {
			out = append(out, c)
		}
	}
	return out
}

// fallbackComponent is "the root/only component" every legacy resolver falls
// back to when its specific match (a path, an area kind) does not resolve.
func fallbackComponent(active []domain.Component) (domain.Component, bool) {
	if len(active) == 1 {
		return active[0], true
	}
	for _, c := range active {
		if c.Path == "." {
			return c, true
		}
	}
	return domain.Component{}, false
}

// resolveLegacyComponentByPath is for the legacy tables keyed by a literal
// sub-project path ("" = the repository itself): repository_vercel_projects,
// repository_gcloud_resources, repository_deploy_targets.
func resolveLegacyComponentByPath(components []domain.Component, subProjectPath string) (domain.Component, bool) {
	active := activeComponentsOnly(components)
	if len(active) == 0 {
		return domain.Component{}, false
	}
	target := subProjectPath
	if target == "" {
		target = "."
	}
	for _, c := range active {
		if c.Path == target {
			return c, true
		}
	}
	return fallbackComponent(active)
}

// resolveLegacyComponentByArea is for repository_hosting_links, keyed by
// domain.HostingAreaRoot ("") or a sub-repo kind (RepoKindFrontend, …).
func resolveLegacyComponentByArea(components []domain.Component, area string) (domain.Component, bool) {
	active := activeComponentsOnly(components)
	if len(active) == 0 {
		return domain.Component{}, false
	}
	if area == domain.HostingAreaRoot {
		return fallbackComponent(active)
	}
	for _, c := range active {
		if c.Role.Get().LegacyRepoKind() == area {
			return c, true
		}
	}
	return fallbackComponent(active)
}

func gcloudLegacyResourceKind(resourceType string) domain.CloudResourceKind {
	switch resourceType {
	case domain.GCloudResourceCloudRun:
		return domain.CloudResourceCloudRunService
	case domain.GCloudResourceGKECluster:
		return domain.CloudResourceGKEWorkload
	}
	return ""
}

// backfillEnvironmentsFromLegacyLinks is part (b) of Boot: every legacy
// Vercel project link / Vercel hosting link / gcloud resource binding becomes
// a confirmed production environment, skipping any (component, environment)
// pair that already has a row.
func (s *Service) backfillEnvironmentsFromLegacyLinks(ctx context.Context) {
	vercelAccountID := s.findAccountID(ctx, domain.CloudVercel)
	gcpAccountID := s.findAccountID(ctx, domain.CloudGCP)
	componentsFor := s.cachingComponentLister()

	existing, err := s.existingEnvironmentKeys(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: backfill: listing existing environments failed")
		return
	}

	if projectLinks, err := s.legacy.ListLegacyVercelProjectLinks(ctx); err != nil {
		log.Warn().Err(err).Msg("cloud: backfill: listing legacy vercel project links failed")
	} else {
		for _, l := range projectLinks {
			comps, err := componentsFor(ctx, l.RepositoryID)
			if err != nil {
				log.Warn().Err(err).Str("repository_id", l.RepositoryID.String()).Msg("cloud: backfill: listing components failed")
				continue
			}
			comp, ok := resolveLegacyComponentByPath(comps, l.SubProjectPath)
			if !ok {
				continue
			}
			s.backfillEnvironment(ctx, existing, comp, domain.EnvironmentProduction, vercelAccountID,
				domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: l.ProjectID, Name: l.ProjectName}, l.ProductionURL)
		}
	}

	if hostingLinks, err := s.legacy.ListLegacyVercelHostingLinks(ctx); err != nil {
		log.Warn().Err(err).Msg("cloud: backfill: listing legacy hosting links failed")
	} else {
		for _, l := range hostingLinks {
			comps, err := componentsFor(ctx, l.RepositoryID)
			if err != nil {
				log.Warn().Err(err).Str("repository_id", l.RepositoryID.String()).Msg("cloud: backfill: listing components failed")
				continue
			}
			comp, ok := resolveLegacyComponentByArea(comps, l.Area)
			if !ok {
				continue
			}
			s.backfillEnvironment(ctx, existing, comp, domain.EnvironmentProduction, vercelAccountID,
				domain.CloudResourceRef{Kind: domain.CloudResourceVercelProject, ID: l.ExternalID, Name: l.ExternalName}, l.ProductionURL)
		}
	}

	if bindings, err := s.legacy.ListLegacyGCloudResourceBindings(ctx); err != nil {
		log.Warn().Err(err).Msg("cloud: backfill: listing legacy gcloud resource bindings failed")
	} else {
		for _, b := range bindings {
			kind := gcloudLegacyResourceKind(b.ResourceType)
			if kind == "" {
				continue
			}
			comps, err := componentsFor(ctx, b.RepositoryID)
			if err != nil {
				log.Warn().Err(err).Str("repository_id", b.RepositoryID.String()).Msg("cloud: backfill: listing components failed")
				continue
			}
			comp, ok := resolveLegacyComponentByPath(comps, b.SubProjectPath)
			if !ok {
				continue
			}
			s.backfillEnvironment(ctx, existing, comp, domain.EnvironmentProduction, gcpAccountID,
				domain.CloudResourceRef{Kind: kind, ID: b.ResourceName, Name: b.DisplayName, Region: b.Location}, "")
		}
	}
}

func refProviderKind(kind domain.CloudResourceKind) domain.CloudProviderKind {
	switch kind {
	case domain.CloudResourceVercelProject:
		return domain.CloudVercel
	case domain.CloudResourceCloudRunService, domain.CloudResourceCloudRunJob, domain.CloudResourceAppEngineService,
		domain.CloudResourceCloudFunction, domain.CloudResourceGKEWorkload:
		return domain.CloudGCP
	case domain.CloudResourceECSService, domain.CloudResourceLambdaFunction, domain.CloudResourceAppRunnerService:
		return domain.CloudAWS
	}
	return ""
}

func (s *Service) backfillEnvironment(ctx context.Context, existing map[componentEnvKey]bool, comp domain.Component, env domain.DeployEnvironment, accountID *uuid.UUID, ref domain.CloudResourceRef, url string) {
	if accountID == nil || ref.ID == "" {
		return
	}
	key := componentEnvKey{comp.ID, env}
	if existing[key] {
		return
	}
	row := domain.ComponentEnvironment{
		ID:           uuid.New(),
		RepositoryID: comp.RepositoryID,
		ComponentID:  comp.ID,
		Environment:  env,
		Provider:     refProviderKind(ref.Kind),
		AccountID:    accountID,
		Resource:     &ref,
		URL:          url,
		Status:       domain.LinkConfirmed,
		Source:       domain.LinkSourceUser,
		Confidence:   domain.ConfidenceExact,
	}
	if _, err := s.environments.SaveEnvironment(ctx, row); err != nil {
		log.Warn().Err(err).Str("component_id", comp.ID.String()).Msg("cloud: backfill: saving environment failed")
		return
	}
	existing[key] = true
}

func legacyEnvToDomain(env string) domain.DeployEnvironment {
	switch env {
	case domain.DeployEnvProd:
		return domain.EnvironmentProduction
	case domain.DeployEnvStage:
		return domain.EnvironmentStaging
	}
	return ""
}

// backfillEnvironmentsFromDeployTargets is part (c) of Boot: a legacy deploy
// target with an address and no environment yet becomes a custom environment
// (no account, no resource — just the address the target already recorded).
// preprod and local have no cloud-environment equivalent and are ignored.
func (s *Service) backfillEnvironmentsFromDeployTargets(ctx context.Context) {
	targets, err := s.deployTargets.ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: backfill: listing deploy targets failed")
		return
	}
	if len(targets) == 0 {
		return
	}

	existing, err := s.existingEnvironmentKeys(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("cloud: backfill: listing existing environments failed")
		return
	}
	componentsFor := s.cachingComponentLister()

	for _, t := range targets {
		env := legacyEnvToDomain(t.Env)
		if env == "" {
			continue
		}
		baseURL, healthURL := strings.TrimSpace(t.BaseURL), strings.TrimSpace(t.HealthURL)
		if baseURL == "" && healthURL == "" {
			continue
		}
		comps, err := componentsFor(ctx, t.RepositoryID)
		if err != nil {
			log.Warn().Err(err).Str("repository_id", t.RepositoryID.String()).Msg("cloud: backfill: listing components failed")
			continue
		}
		comp, ok := resolveLegacyComponentByPath(comps, t.SubProjectPath)
		if !ok {
			continue
		}
		key := componentEnvKey{comp.ID, env}
		if existing[key] {
			continue
		}

		url := baseURL
		if url == "" {
			url = healthURL
		}
		row := domain.ComponentEnvironment{
			ID:           uuid.New(),
			RepositoryID: comp.RepositoryID,
			ComponentID:  comp.ID,
			Environment:  env,
			URL:          url,
			HealthURL:    healthURL,
			Status:       domain.LinkConfirmed,
			Source:       domain.LinkSourceUser,
			Confidence:   domain.ConfidenceExact,
		}
		if _, err := s.environments.SaveEnvironment(ctx, row); err != nil {
			log.Warn().Err(err).Str("component_id", comp.ID.String()).Msg("cloud: backfill: saving environment failed")
			continue
		}
		existing[key] = true
	}
}
