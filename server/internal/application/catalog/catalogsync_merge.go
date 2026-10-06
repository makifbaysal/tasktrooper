package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var mergeSkillSystemKey = prompt.Define[struct{}]("catalog.merge_skill_system", struct{}{})

type mergeSkillUserInput struct {
	Name, Local, Upstream string
}

var mergeSkillUserKey = prompt.Define("catalog.merge_skill_user", mergeSkillUserInput{
	Name: "sample-skill", Local: "local body", Upstream: "upstream body",
})

// mergeSkillSystemPrompt is the fixed instruction for reconciling two SKILL.md
// versions; see catalog/system/prompts/catalog/merge_skill_system.md.
func mergeSkillSystemPrompt() string {
	return prompt.Text(mergeSkillSystemKey)
}

// mergeSkillUserMessage carries the two document bodies the LLM must
// reconcile; see catalog/system/prompts/catalog/merge_skill_user.md.
func mergeSkillUserMessage(skillName, local, upstream string) string {
	return mergeSkillUserKey.Render(mergeSkillUserInput{Name: skillName, Local: local, Upstream: upstream})
}

// Asks the LLM to merge a skill changed both locally and upstream; the local copy's tags and tech stack win, and the model reconciles the prose.
func (s *Service) mergeSkill(ctx context.Context, agent domain.Agent, local domain.Skill, usk domain.UpstreamSkill) error {
	system := mergeSkillSystemPrompt()
	user := mergeSkillUserMessage(local.Name, local.Content, usk.Content)

	model := agent.Model
	if model == "" {
		model = agent.ModelHeavy
	}
	resp, err := s.llm.Chat(ctx, domain.AgentRequest{
		Messages: []domain.Message{
			{Role: domain.RoleSystem, Content: system},
			{Role: domain.RoleUser, Content: user},
		},
		Model: model,
	})
	if err != nil {
		return fmt.Errorf("LLM merge call: %w", err)
	}
	meta, body, err := parseSeedDoc(resp.Message.Content)
	if err != nil {
		return fmt.Errorf("LLM merge output is not a SKILL.md: %w", err)
	}
	name := strings.TrimSpace(meta["name"])
	if name == "" {
		name = local.Name
	}
	ctx = WithVersionSource(ctx, VersionSource{Source: domain.CatalogVersionSourceMerge})
	_, err = s.UpdateSkillForAgent(ctx, agent.ID, local.ID, domain.UpdateSkillRequest{
		Name: name, Description: meta["description"], Category: meta["category"],
		Tags: local.Tags, Content: body, Enabled: usk.Enabled, TechStackID: local.TechStackID,
	})
	if err != nil {
		return err
	}
	updated, err := s.store.GetSkill(ctx, local.ID)
	if err != nil {
		return err
	}
	return s.stampSkillSHA(ctx, updated, usk.Sha)
}
