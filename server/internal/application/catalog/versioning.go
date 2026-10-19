package catalog

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// Source travels on the context so callers never pass what they do not care about; absence means a user edit.
type versionSourceKey struct{}

type VersionSource struct {
	Source       string
	Reason       string
	ReflectionID *uuid.UUID
}

func WithVersionSource(ctx context.Context, src VersionSource) context.Context {
	if src.Source == "" {
		src.Source = domain.CatalogVersionSourceUser
	}
	return context.WithValue(ctx, versionSourceKey{}, src)
}

func versionSourceFrom(ctx context.Context) VersionSource {
	if src, ok := ctx.Value(versionSourceKey{}).(VersionSource); ok {
		return src
	}
	return VersionSource{Source: domain.CatalogVersionSourceUser}
}

func VersionSourceFrom(ctx context.Context) VersionSource {
	return versionSourceFrom(ctx)
}

func (s *Service) SetVersionStore(store port.CatalogVersionStore) {
	s.versions = store
}

// Best-effort: a failed append must not fail the write the user asked for, only costs a rollback step.
func (s *Service) recordSkillVersion(ctx context.Context, action string, sk domain.Skill) {
	if s.versions == nil {
		return
	}
	src := versionSourceFrom(ctx)
	if _, err := s.versions.AppendVersion(ctx, domain.CatalogVersion{
		AgentID: sk.AgentID, TargetKind: domain.CatalogVersionKindSkill, TargetID: sk.ID,
		Action: action, Name: sk.Name, Description: sk.Description, Category: sk.Category,
		Tags: sk.Tags, Content: sk.Content, Enabled: sk.Enabled,
		Source: src.Source, Reason: src.Reason, ReflectionID: src.ReflectionID,
	}); err != nil {
		log.Warn().Err(err).Str("skill", sk.Name).Msg("skill version append failed")
	}
}

func (s *Service) recordRuleVersion(ctx context.Context, action string, r domain.OrchestratorRule) {
	if s.versions == nil {
		return
	}
	src := versionSourceFrom(ctx)
	if _, err := s.versions.AppendVersion(ctx, domain.CatalogVersion{
		AgentID: r.AgentID, TargetKind: domain.CatalogVersionKindRule, TargetID: r.ID,
		Action: action, Name: r.Name, Content: r.Content, Priority: r.Priority, Enabled: r.Enabled,
		Source: src.Source, Reason: src.Reason, ReflectionID: src.ReflectionID,
	}); err != nil {
		log.Warn().Err(err).Str("rule", r.Name).Msg("rule version append failed")
	}
}

func (s *Service) ListSkillVersions(ctx context.Context, agentID, skillID uuid.UUID, limit int) ([]domain.CatalogVersion, error) {
	if s.versions == nil {
		return []domain.CatalogVersion{}, nil
	}
	if _, err := s.GetSkillForAgent(ctx, agentID, skillID); err != nil {
		return nil, err
	}
	return s.versions.ListVersions(ctx, domain.CatalogVersionKindSkill, skillID, limit)
}

func (s *Service) ListRuleVersions(ctx context.Context, agentID, ruleID uuid.UUID, limit int) ([]domain.CatalogVersion, error) {
	if s.versions == nil {
		return []domain.CatalogVersion{}, nil
	}
	if _, err := s.GetRuleForAgent(ctx, agentID, ruleID); err != nil {
		return nil, err
	}
	return s.versions.ListVersions(ctx, domain.CatalogVersionKindRule, ruleID, limit)
}

// The restore is itself a new version, so rolling back a rollback is the same operation again.
func (s *Service) RestoreSkillVersion(ctx context.Context, agentID, skillID uuid.UUID, version int) (domain.Skill, error) {
	if s.versions == nil {
		return domain.Skill{}, fmt.Errorf("version history is not available")
	}
	current, err := s.GetSkillForAgent(ctx, agentID, skillID)
	if err != nil {
		return domain.Skill{}, err
	}
	snap, err := s.versions.GetVersion(ctx, domain.CatalogVersionKindSkill, skillID, version)
	if err != nil {
		return domain.Skill{}, err
	}
	if snap.AgentID != agentID {
		return domain.Skill{}, fmt.Errorf("version does not belong to this agent")
	}
	src := versionSourceFrom(ctx)
	src.Source = domain.CatalogVersionSourceUser
	if src.Reason == "" {
		src.Reason = fmt.Sprintf("restored from version %d", version)
	}
	restoreCtx := context.WithValue(WithVersionSource(ctx, src), restoreActionKey{}, true)
	// TechStackID comes off the live skill: history predates tech stacks and does not record one.
	return s.UpdateSkillForAgent(restoreCtx, agentID, skillID, domain.UpdateSkillRequest{
		Name: snap.Name, Description: snap.Description, Category: snap.Category,
		Tags: snap.Tags, Content: snap.Content, Enabled: snap.Enabled,
		TechStackID: current.TechStackID,
	})
}

func (s *Service) RestoreRuleVersion(ctx context.Context, agentID, ruleID uuid.UUID, version int) (domain.OrchestratorRule, error) {
	if s.versions == nil {
		return domain.OrchestratorRule{}, fmt.Errorf("version history is not available")
	}
	if _, err := s.GetRuleForAgent(ctx, agentID, ruleID); err != nil {
		return domain.OrchestratorRule{}, err
	}
	snap, err := s.versions.GetVersion(ctx, domain.CatalogVersionKindRule, ruleID, version)
	if err != nil {
		return domain.OrchestratorRule{}, err
	}
	if snap.AgentID != agentID {
		return domain.OrchestratorRule{}, fmt.Errorf("version does not belong to this agent")
	}
	src := versionSourceFrom(ctx)
	src.Source = domain.CatalogVersionSourceUser
	if src.Reason == "" {
		src.Reason = fmt.Sprintf("restored from version %d", version)
	}
	restoreCtx := context.WithValue(WithVersionSource(ctx, src), restoreActionKey{}, true)
	return s.UpdateRuleForAgent(restoreCtx, agentID, ruleID, domain.UpdateOrchestratorRuleRequest{
		Name: snap.Name, Content: snap.Content, Priority: snap.Priority, Enabled: snap.Enabled,
	})
}

// Marks the update a restore performs so its history row says "restore".
type restoreActionKey struct{}

func updateAction(ctx context.Context) string {
	if restoring, ok := ctx.Value(restoreActionKey{}).(bool); ok && restoring {
		return domain.CatalogVersionActionRestore
	}
	return domain.CatalogVersionActionUpdate
}
