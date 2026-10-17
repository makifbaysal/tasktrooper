package alerting

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// CreateAIDraft stores a rule the release engineer proposed. It is always
// disabled, whatever the request says, so nothing evaluates or notifies
// until a person has read it and turned it on.
func (s *Service) CreateAIDraft(ctx context.Context, req RuleRequest) (domain.AlertRule, error) {
	disabled := false
	req.Enabled = &disabled
	spec, err := s.specFrom(ctx, defaultSpec(), req, true)
	if err != nil {
		return domain.AlertRule{}, err
	}
	return s.rules.CreateAlertRule(ctx, domain.AlertRule{
		AlertRuleSpec: spec,
		State:         domain.AlertStateOK,
		StateSince:    s.now(),
		CreatedBy:     domain.AlertRuleByAI,
	})
}
