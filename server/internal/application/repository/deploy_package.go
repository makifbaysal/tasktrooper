package repository

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

func (s *Service) SetDeployPackages(store port.DeployPackageStore) {
	s.deployPackages = store
}

func (s *Service) ListDeployPackages(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return nil, fmt.Errorf("deploy package store unavailable")
	}
	packages, err := s.deployPackages.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DeployPackage, 0, len(packages))
	for _, pkg := range packages {
		advanced, aerr := s.AdvancePackage(ctx, repositoryID, pkg)
		if aerr != nil {

			log.Warn().Err(aerr).Str("package_id", pkg.ID.String()).Msg("advance deploy package failed")
			pkg.Tasks, _ = s.deployPackages.ListTasks(ctx, pkg.ID)
			out = append(out, pkg)
			continue
		}
		out = append(out, advanced)
	}
	return out, nil
}

func (s *Service) GetDeployPackage(ctx context.Context, repositoryID, packageID uuid.UUID) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	pkg, err := s.deployPackages.Get(ctx, repositoryID, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}
	return s.AdvancePackage(ctx, repositoryID, pkg)
}

func (s *Service) CreateDeployPackage(ctx context.Context, repositoryID uuid.UUID, name, description string) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.DeployPackage{}, fmt.Errorf("deploy package name is required")
	}
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return domain.DeployPackage{}, err
	}
	return s.deployPackages.Create(ctx, domain.DeployPackage{
		RepositoryID: repositoryID,
		Name:         name,
		Description:  strings.TrimSpace(description),
		Status:       domain.DeployPackageStatusDraft,
	})
}

func (s *Service) UpdateDeployPackage(ctx context.Context, repositoryID, packageID uuid.UUID, name, description, status *string) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	if status != nil {
		if *status != domain.DeployPackageStatusCancelled {
			return domain.DeployPackage{}, fmt.Errorf(
				"a deploy package's status is set by its release, not by the client; only %q may be requested",
				domain.DeployPackageStatusCancelled)
		}
		current, err := s.deployPackages.Get(ctx, repositoryID, packageID)
		if err != nil {
			return domain.DeployPackage{}, err
		}
		if current.Status == domain.DeployPackageStatusReleased {
			return domain.DeployPackage{}, fmt.Errorf("this package is already released; cancelling it would not un-deploy anything")
		}
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return domain.DeployPackage{}, fmt.Errorf("deploy package name is required")
		}
		name = &trimmed
	}
	return s.deployPackages.Update(ctx, repositoryID, packageID, name, description, status, nil)
}

func (s *Service) DeleteDeployPackage(ctx context.Context, repositoryID, packageID uuid.UUID) error {
	if s.deployPackages == nil {
		return fmt.Errorf("deploy package store unavailable")
	}
	return s.deployPackages.Delete(ctx, repositoryID, packageID)
}

func (s *Service) SetDeployPackageTasks(ctx context.Context, repositoryID, packageID uuid.UUID, taskIDs []uuid.UUID) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	pkg, err := s.deployPackages.Get(ctx, repositoryID, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}
	if pkg.Status == domain.DeployPackageStatusReleasing || pkg.Status == domain.DeployPackageStatusReleased {
		return domain.DeployPackage{}, fmt.Errorf("cannot change the membership of a %s package", pkg.Status)
	}

	for _, taskID := range taskIDs {
		if _, terr := s.tasks.Get(ctx, repositoryID, taskID); terr != nil {
			return domain.DeployPackage{}, fmt.Errorf("task %s does not belong to this repository", taskID)
		}
	}
	if err := s.deployPackages.ReplaceTasks(ctx, packageID, taskIDs); err != nil {
		return domain.DeployPackage{}, err
	}
	return s.AdvancePackage(ctx, repositoryID, pkg)
}

func (s *Service) ReleasePackage(ctx context.Context, repositoryID, packageID uuid.UUID) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	pkg, err := s.deployPackages.Get(ctx, repositoryID, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}

	if pkg.Status != domain.DeployPackageStatusDraft && pkg.Status != domain.DeployPackageStatusFailed {
		return domain.DeployPackage{}, fmt.Errorf("a %s deploy package cannot be released", pkg.Status)
	}
	members, err := s.deployPackages.ListTasks(ctx, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}
	if len(members) == 0 {
		return domain.DeployPackage{}, fmt.Errorf("this deploy package has no tasks")
	}

	if _, err := s.orderMembers(ctx, members); err != nil {
		note := err.Error()
		return s.failPackage(ctx, repositoryID, packageID, note)
	}

	updated, err := s.deployPackages.Update(ctx, repositoryID, packageID,
		nil, nil, ptr(domain.DeployPackageStatusReleasing), ptr(""))
	if err != nil {
		return domain.DeployPackage{}, err
	}
	return s.AdvancePackage(ctx, repositoryID, updated)
}

func (s *Service) AdvancePackage(ctx context.Context, repositoryID uuid.UUID, pkg domain.DeployPackage) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return pkg, fmt.Errorf("deploy package store unavailable")
	}
	members, err := s.deployPackages.ListTasks(ctx, pkg.ID)
	if err != nil {
		return pkg, err
	}
	released, err := s.markReleasedMembers(ctx, repositoryID, members)
	if err != nil {
		return pkg, err
	}
	pkg.Tasks = members

	if pkg.Status != domain.DeployPackageStatusReleasing {
		return pkg, nil
	}
	if len(members) == 0 {
		return pkg, nil
	}
	if len(released) == len(members) {
		done, uerr := s.deployPackages.Update(ctx, repositoryID, pkg.ID,
			nil, nil, ptr(domain.DeployPackageStatusReleased), ptr(""))
		if uerr != nil {
			return pkg, uerr
		}
		done.Tasks = members
		return done, nil
	}

	ordered, err := s.orderMembers(ctx, members)
	if err != nil {
		failed, ferr := s.failPackage(ctx, repositoryID, pkg.ID, err.Error())
		if ferr != nil {
			return pkg, ferr
		}
		failed.Tasks = members
		return failed, nil
	}

	for _, member := range ordered {
		if released[member.TaskID] {
			continue
		}

		ready, rerr := s.memberDependenciesSatisfied(ctx, repositoryID, member, released)
		if rerr != nil {
			return pkg, rerr
		}
		if !ready {
			continue
		}
		inFlight, ierr := s.hasDeployInFlight(ctx, member.TaskID)
		if ierr != nil {
			return pkg, ierr
		}
		if inFlight {
			continue
		}
		if _, derr := s.triggerRelease(ctx, repositoryID, member.TaskID); derr != nil {
			note := fmt.Sprintf("%s: %v", memberLabel(member), derr)
			failed, ferr := s.failPackage(ctx, repositoryID, pkg.ID, note)
			if ferr != nil {
				return pkg, ferr
			}
			failed.Tasks = members
			return failed, nil
		}
	}
	return pkg, nil
}

func (s *Service) markReleasedMembers(ctx context.Context, repositoryID uuid.UUID, members []domain.DeployPackageTask) (map[uuid.UUID]bool, error) {
	released := make(map[uuid.UUID]bool, len(members))
	for i := range members {
		task, err := s.tasks.Get(ctx, repositoryID, members[i].TaskID)
		if err != nil {
			return nil, fmt.Errorf("read package member %s: %w", memberLabel(members[i]), err)
		}
		live, lerr := s.taskIsLive(ctx, repositoryID, task)
		if lerr != nil {
			return nil, fmt.Errorf("read deploy history of %s: %w", memberLabel(members[i]), lerr)
		}
		members[i].Released = live
		members[i].Column = task.Column
		if live {
			released[members[i].TaskID] = true
		}
	}
	return released, nil
}

func (s *Service) memberDependenciesSatisfied(ctx context.Context, repositoryID uuid.UUID, member domain.DeployPackageTask, released map[uuid.UUID]bool) (bool, error) {
	if s.relations == nil {
		return true, nil
	}
	rels, err := s.relations.ListBySource(ctx, member.TaskID)
	if err != nil {
		return false, err
	}
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationDeployDependsOn {
			continue
		}
		if released[rel.TargetTaskID] {
			continue
		}

		target, terr := s.tasks.Get(ctx, repositoryID, rel.TargetTaskID)
		if terr != nil {
			return false, nil
		}
		live, lerr := s.taskIsLive(ctx, repositoryID, target)
		if lerr != nil {
			return false, lerr
		}
		if !live {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) hasDeployInFlight(ctx context.Context, taskID uuid.UUID) (bool, error) {
	if s.pipelineStore == nil {
		return false, nil
	}
	runs, err := s.pipelineStore.ListByTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if run.Trigger != domain.PipelineTriggerProdDeploy && run.Trigger != domain.PipelineTriggerPreProdDeploy {
			continue
		}
		if run.Status == domain.PipelineStatusPending || run.Status == domain.PipelineStatusRunning {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) failPackage(ctx context.Context, repositoryID, packageID uuid.UUID, note string) (domain.DeployPackage, error) {
	log.Warn().Str("package_id", packageID.String()).Str("note", note).Msg("deploy package failed")
	return s.deployPackages.Update(ctx, repositoryID, packageID,
		nil, nil, ptr(domain.DeployPackageStatusFailed), &note)
}

func (s *Service) orderMembers(ctx context.Context, members []domain.DeployPackageTask) ([]domain.DeployPackageTask, error) {
	inPackage := make(map[uuid.UUID]bool, len(members))
	for _, m := range members {
		inPackage[m.TaskID] = true
	}

	deps := make(map[uuid.UUID]map[uuid.UUID]bool, len(members))
	for _, m := range members {
		deps[m.TaskID] = map[uuid.UUID]bool{}
	}
	if s.relations != nil {
		for _, m := range members {
			rels, err := s.relations.ListBySource(ctx, m.TaskID)
			if err != nil {
				return nil, err
			}
			for _, rel := range rels {
				if rel.RelationType != domain.TaskRelationDeployDependsOn {
					continue
				}
				if !inPackage[rel.TargetTaskID] || rel.TargetTaskID == m.TaskID {
					continue
				}
				deps[m.TaskID][rel.TargetTaskID] = true
			}
		}
	}

	pending := make([]domain.DeployPackageTask, len(members))
	copy(pending, members)
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].Position < pending[j].Position })

	done := make(map[uuid.UUID]bool, len(members))
	ordered := make([]domain.DeployPackageTask, 0, len(members))
	for len(ordered) < len(pending) {
		progressed := false
		for _, m := range pending {
			if done[m.TaskID] {
				continue
			}
			ready := true
			for dep := range deps[m.TaskID] {
				if !done[dep] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			done[m.TaskID] = true
			ordered = append(ordered, m)
			progressed = true
			break
		}
		if !progressed {
			var stuck []string
			for _, m := range pending {
				if !done[m.TaskID] {
					stuck = append(stuck, memberLabel(m))
				}
			}
			return nil, fmt.Errorf("%w: %s", domain.ErrDeployPackageCycle, strings.Join(stuck, " ↔ "))
		}
	}
	return ordered, nil
}

func memberLabel(m domain.DeployPackageTask) string {
	if strings.TrimSpace(m.Key) != "" {
		return m.Key
	}
	return m.TaskID.String()
}

func ptr[T any](v T) *T { return &v }

func IsDeployPackageNotFound(err error) bool {
	return errors.Is(err, port.ErrNotFound)
}
