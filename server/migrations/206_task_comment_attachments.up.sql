CREATE TABLE IF NOT EXISTS task_comment_attachments (
    comment_id    UUID NOT NULL REFERENCES task_comments(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
    PRIMARY KEY (comment_id, attachment_id)
);

CREATE INDEX IF NOT EXISTS idx_task_comment_attachments_attachment ON task_comment_attachments(attachment_id);
