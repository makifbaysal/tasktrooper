CREATE INDEX IF NOT EXISTS idx_agent_score_events_task_event
    ON agent_score_events (task_id, event_type)
    WHERE task_id IS NOT NULL;
