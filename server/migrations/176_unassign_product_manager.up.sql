-- The product manager never works a board task as its assignee: it reaches
-- pm_uat through its column subscription, and repository.Service now refuses
-- it as an assignee (domain.ErrAssigneeNotAssignable). A card already assigned
-- to it would otherwise keep waking the PM from the reconciler while nobody
-- does the work, so open cards fall back to their column's agents. done and
-- released keep the assignee they shipped with.
UPDATE board_tasks bt
SET assignee_agent_id = NULL,
    updated_at = now()
WHERE bt.board_column NOT IN ('done', 'released')
  AND bt.assignee_agent_id IN (
      SELECT ara.agent_id
      FROM agent_role_assignments ara
      JOIN roles r ON r.id = ara.role_id
      WHERE r.key = 'product_manager'
  );
