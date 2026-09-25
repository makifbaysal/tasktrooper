package projectmodel

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"strings"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// detectDelivery infers a component's delivery profile from its own CI
// checks and bound environments, in the same priority order a human would
// read them in: a mobile app ships through a store, a workflow that runs on
// every push to main IS the deploy, a dispatchable workflow needs someone (or
// something) to fire it, a Vercel git integration deploys on merge without a
// workflow at all, a tag-triggered release job cuts a batch, and anything
// left over deploys nothing. checks and envs are expected to already be
// scoped to this component.
func detectDelivery(component domain.Component, checks []domain.ComponentCheck, envs []domain.ComponentEnvironment) (domain.Fact[domain.ComponentDelivery], bool) {
	if component.Mobile != nil {
		evidence := component.Mobile.Evidence
		if len(evidence) == 0 {
			evidence = []domain.SourceEvidence{{Note: "mobile component"}}
		}
		return domain.Fact[domain.ComponentDelivery]{
			Detected: &domain.ComponentDelivery{
				Mode:         domain.DeliveryBatch,
				Executor:     domain.ExecutorStore,
				AutoRollback: true,
				Verify:       domain.DeliveryVerify{SoakMinutes: domain.DefaultSoakMinutes},
			},
			Confidence: domain.ConfidenceMedium,
			Evidence:   evidence,
		}, true
	}

	if check, ok := firstProductionDeployCheck(checks, pushesToDefaultBranch); ok {
		return domain.Fact[domain.ComponentDelivery]{
			Detected: &domain.ComponentDelivery{
				Mode:         domain.DeliveryOnMerge,
				Executor:     domain.ExecutorGitHubActions,
				Workflow:     path.Base(check.Workflow),
				AutoRollback: true,
				Verify:       domain.DeliveryVerify{SoakMinutes: domain.DefaultSoakMinutes},
			},
			Confidence: domain.ConfidenceHigh,
			Evidence:   checkEvidence(check),
		}, true
	}

	if check, ok := firstProductionDeployCheck(checks, isDispatchable); ok {
		return domain.Fact[domain.ComponentDelivery]{
			Detected: &domain.ComponentDelivery{
				Mode:         domain.DeliveryDispatch,
				Executor:     domain.ExecutorGitHubActions,
				Workflow:     path.Base(check.Workflow),
				AutoRollback: true,
				Verify:       domain.DeliveryVerify{SoakMinutes: domain.DefaultSoakMinutes},
			},
			Confidence: domain.ConfidenceMedium,
			Evidence:   checkEvidence(check),
		}, true
	}

	if env, confidence, ok := productionVercelEnvironment(envs); ok {
		verify := domain.DeliveryVerify{SoakMinutes: domain.DefaultSoakMinutes}
		if component.Role.Get() == domain.ComponentRoleFrontend {
			verify.Smoke = []domain.SmokeCheck{{Method: "GET", Path: "/"}}
		}
		return domain.Fact[domain.ComponentDelivery]{
			Detected: &domain.ComponentDelivery{
				Mode:         domain.DeliveryOnMerge,
				Executor:     domain.ExecutorVercel,
				AutoRollback: true,
				Verify:       verify,
			},
			Confidence: confidence,
			Evidence:   envEvidence(env),
		}, true
	}

	if check, ok := firstReleaseCheck(checks); ok {
		return domain.Fact[domain.ComponentDelivery]{
			Detected: &domain.ComponentDelivery{
				Mode: domain.DeliveryBatch,
				// The trigger encoding (application/discovery/ci
				// renderEvent) only ever records that push is tag-scoped
				// ("push:tags"), never the glob itself, so "v*" — the only
				// pattern this repository or any other real-world workflow
				// in the fixtures uses — is the best a scan can assert;
				// ComponentDelivery.Normalized() would default to the same
				// pattern anyway.
				Executor:     domain.ExecutorGitHubActions,
				TagPattern:   "v{version}",
				AutoRollback: true,
				Verify:       domain.DeliveryVerify{SoakMinutes: domain.DefaultSoakMinutes},
			},
			Confidence: domain.ConfidenceMedium,
			Evidence:   checkEvidence(check),
		}, true
	}

	return domain.Fact[domain.ComponentDelivery]{
		Detected: &domain.ComponentDelivery{
			Mode:         domain.DeliveryNone,
			AutoRollback: true,
			Verify:       domain.DeliveryVerify{SoakMinutes: domain.DefaultSoakMinutes},
		},
		Confidence: domain.ConfidenceHigh,
	}, true
}

// firstProductionDeployCheck returns the first active, non-missing deploy
// check targeting production that also satisfies extra (a push-to-main test,
// or "is dispatchable"), preserving check order so the result is stable.
func firstProductionDeployCheck(checks []domain.ComponentCheck, extra func(domain.ComponentCheck) bool) (domain.ComponentCheck, bool) {
	for _, c := range checks {
		if !isProductionDeployCheck(c) {
			continue
		}
		if extra(c) {
			return c, true
		}
	}
	return domain.ComponentCheck{}, false
}

func isProductionDeployCheck(c domain.ComponentCheck) bool {
	return c.Status == domain.ModelStatusActive && !c.Missing &&
		c.Purpose.Get() == domain.CheckDeploy && c.Environment == domain.EnvironmentProduction
}

func pushesToDefaultBranch(c domain.ComponentCheck) bool {
	return pushToBranch(c.Triggers, "main", "master")
}

func isDispatchable(c domain.ComponentCheck) bool { return c.Dispatchable }

func firstReleaseCheck(checks []domain.ComponentCheck) (domain.ComponentCheck, bool) {
	for _, c := range checks {
		if c.Status != domain.ModelStatusActive || c.Missing || c.Purpose.Get() != domain.CheckRelease {
			continue
		}
		if tagTriggered(c.Triggers) {
			return c, true
		}
	}
	return domain.ComponentCheck{}, false
}

// pushToBranch mirrors ci.pushToMain's "push:main,master" trigger encoding
// (application/discovery/ci/workflow.go renderEvent): application packages
// keep this kind of parsing local rather than exporting it across packages
// just for one caller.
func pushToBranch(triggers []string, branches ...string) bool {
	for _, t := range triggers {
		rest, ok := strings.CutPrefix(t, "push:")
		if !ok {
			continue
		}
		for _, b := range strings.Split(rest, ",") {
			for _, want := range branches {
				if b == want {
					return true
				}
			}
		}
	}
	return false
}

func tagTriggered(triggers []string) bool {
	for _, t := range triggers {
		if t == "push:tags" {
			return true
		}
	}
	return false
}

// productionVercelEnvironment picks a component's production environment
// bound (or suggested) to Vercel: high confidence once the platform has
// confirmed it (a human pick or an exact-signal auto-confirm), medium while
// it is still an open suggestion nobody has looked at.
func productionVercelEnvironment(envs []domain.ComponentEnvironment) (domain.ComponentEnvironment, domain.Confidence, bool) {
	for _, e := range envs {
		if e.Environment != domain.EnvironmentProduction || e.Provider != domain.CloudVercel {
			continue
		}
		switch e.Status {
		case domain.LinkConfirmed:
			return e, domain.ConfidenceHigh, true
		case domain.LinkSuggested:
			if e.AutoConfirmed {
				return e, domain.ConfidenceHigh, true
			}
			return e, domain.ConfidenceMedium, true
		}
	}
	return domain.ComponentEnvironment{}, "", false
}

func checkEvidence(c domain.ComponentCheck) []domain.SourceEvidence {
	if c.Workflow == "" {
		return nil
	}
	note := "job " + c.JobKey
	if c.JobName != "" {
		note = "job " + c.JobName
	}
	return []domain.SourceEvidence{{Path: c.Workflow, Note: note}}
}

func envEvidence(e domain.ComponentEnvironment) []domain.SourceEvidence {
	const note = "production environment bound to vercel"
	if e.URL != "" {
		return []domain.SourceEvidence{{Path: e.URL, Note: note}}
	}
	return []domain.SourceEvidence{{Note: note}}
}

// refreshDeliveryDetection recomputes every active component's detected
// delivery profile after a scan's checks are reconciled and its deploy
// signals are matched into environments. It runs as its own pass after
// reconcileScan rather than inside it: the Vercel on_merge rule needs a
// CONFIRMED production environment, and MatchScan only writes those once
// reconcileScan has already returned (see runScan) — recomputing here, once,
// with checks and environments both settled, avoids detecting twice and
// getting the environment-dependent modes wrong on the first pass.
func (s *Service) refreshDeliveryDetection(ctx context.Context, repositoryID uuid.UUID) error {
	components, err := s.store.ListComponents(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("list components: %w", err)
	}
	checks, err := s.store.ListChecks(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("list checks: %w", err)
	}
	var envs []domain.ComponentEnvironment
	if s.environments != nil {
		envs, err = s.environments.ListEnvironments(ctx, repositoryID)
		if err != nil {
			return fmt.Errorf("list environments: %w", err)
		}
	}

	checksByComponent := make(map[uuid.UUID][]domain.ComponentCheck, len(components))
	for _, c := range checks {
		checksByComponent[c.ComponentID] = append(checksByComponent[c.ComponentID], c)
	}
	envsByComponent := make(map[uuid.UUID][]domain.ComponentEnvironment, len(components))
	for _, e := range envs {
		envsByComponent[e.ComponentID] = append(envsByComponent[e.ComponentID], e)
	}

	for _, c := range components {
		if c.Status != domain.ComponentStatusActive {
			continue
		}
		detected, ok := detectDelivery(c, checksByComponent[c.ID], envsByComponent[c.ID])
		if !ok {
			continue
		}
		_, wasConfirmed := domain.DeliveryConfirmed(c.Delivery)
		next := c.Delivery.WithDetected(detected)
		if reflect.DeepEqual(next, c.Delivery) {
			continue
		}
		c.Delivery = next
		if _, err := s.store.SaveComponent(ctx, c); err != nil {
			return fmt.Errorf("save component %s delivery: %w", c.ID, err)
		}
		// Tasks merged while the profile was unconfirmed wait in done; a
		// detection that is now confident enough releases them like a human
		// confirmation would.
		if _, nowConfirmed := domain.DeliveryConfirmed(c.Delivery); nowConfirmed && !wasConfirmed && s.deliveryConfirmedHook != nil {
			s.deliveryConfirmedHook(ctx, repositoryID, c.ID)
		}
	}
	return nil
}

// RefreshDelivery re-detects delivery profiles outside a scan — after an
// environment is bound, confirmed or removed, which changes what the Vercel
// rule can see.
func (s *Service) RefreshDelivery(ctx context.Context, repositoryID uuid.UUID) error {
	return s.refreshDeliveryDetection(ctx, repositoryID)
}
