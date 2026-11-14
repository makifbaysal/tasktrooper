package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AttachmentStore struct {
	pool *DB
}

func NewAttachmentStore(pool *DB) *AttachmentStore {
	return &AttachmentStore{pool: pool}
}

const attachmentMetaColumns = `id, repository_id, filename, content_type, size_bytes, sha256, created_by_type, COALESCE(created_by_id, ''), created_at`

func (s *AttachmentStore) Create(ctx context.Context, att domain.Attachment) (domain.AttachmentMeta, error) {
	var meta domain.AttachmentMeta
	err := s.pool.QueryRow(ctx, `
		INSERT INTO attachments (repository_id, filename, content_type, size_bytes, sha256, data, created_by_type, created_by_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))
		RETURNING `+attachmentMetaColumns+`
	`, att.RepositoryID, att.Filename, att.ContentType, att.SizeBytes, att.SHA256, att.Data, att.CreatedByType, att.CreatedByID).Scan(
		&meta.ID, &meta.RepositoryID, &meta.Filename, &meta.ContentType, &meta.SizeBytes, &meta.SHA256, &meta.CreatedByType, &meta.CreatedByID, &meta.CreatedAt,
	)
	if err != nil {
		return domain.AttachmentMeta{}, fmt.Errorf("create attachment: %w", err)
	}
	return meta, nil
}

func (s *AttachmentStore) Get(ctx context.Context, id uuid.UUID) (domain.Attachment, error) {
	var att domain.Attachment
	err := s.pool.QueryRow(ctx, `
		SELECT id, repository_id, filename, content_type, size_bytes, sha256, data, created_by_type, COALESCE(created_by_id, ''), created_at
		FROM attachments WHERE id = $1
	`, id).Scan(
		&att.ID, &att.RepositoryID, &att.Filename, &att.ContentType, &att.SizeBytes, &att.SHA256, &att.Data, &att.CreatedByType, &att.CreatedByID, &att.CreatedAt,
	)
	if err != nil {
		return domain.Attachment{}, fmt.Errorf("get attachment: %w", err)
	}
	return att, nil
}

func (s *AttachmentStore) GetMeta(ctx context.Context, id uuid.UUID) (domain.AttachmentMeta, error) {
	var meta domain.AttachmentMeta
	err := s.pool.QueryRow(ctx, `
		SELECT `+attachmentMetaColumns+` FROM attachments WHERE id = $1
	`, id).Scan(
		&meta.ID, &meta.RepositoryID, &meta.Filename, &meta.ContentType, &meta.SizeBytes, &meta.SHA256, &meta.CreatedByType, &meta.CreatedByID, &meta.CreatedAt,
	)
	if err != nil {
		return domain.AttachmentMeta{}, fmt.Errorf("get attachment meta: %w", err)
	}
	return meta, nil
}

func (s *AttachmentStore) ListMetaByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AttachmentMeta, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.repository_id, a.filename, a.content_type, a.size_bytes, a.sha256, a.created_by_type, COALESCE(a.created_by_id, ''), a.created_at
		FROM task_attachments ta
		JOIN attachments a ON a.id = ta.attachment_id
		WHERE ta.task_id = $1
		ORDER BY ta.position ASC, a.created_at ASC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task attachments: %w", err)
	}
	defer rows.Close()
	var metas []domain.AttachmentMeta
	for rows.Next() {
		var meta domain.AttachmentMeta
		if err := rows.Scan(&meta.ID, &meta.RepositoryID, &meta.Filename, &meta.ContentType, &meta.SizeBytes, &meta.SHA256, &meta.CreatedByType, &meta.CreatedByID, &meta.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan task attachment: %w", err)
		}
		metas = append(metas, meta)
	}
	return metas, rows.Err()
}

func (s *AttachmentStore) ListMetaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	if len(messageIDs) == 0 {
		return map[uuid.UUID][]domain.AttachmentMeta{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT sma.message_id, a.id, a.repository_id, a.filename, a.content_type, a.size_bytes, a.sha256, a.created_by_type, COALESCE(a.created_by_id, ''), a.created_at
		FROM session_message_attachments sma
		JOIN attachments a ON a.id = sma.attachment_id
		WHERE sma.message_id = ANY($1)
		ORDER BY a.created_at ASC
	`, messageIDs)
	if err != nil {
		return nil, fmt.Errorf("list message attachments: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]domain.AttachmentMeta)
	for rows.Next() {
		var messageID uuid.UUID
		var meta domain.AttachmentMeta
		if err := rows.Scan(&messageID, &meta.ID, &meta.RepositoryID, &meta.Filename, &meta.ContentType, &meta.SizeBytes, &meta.SHA256, &meta.CreatedByType, &meta.CreatedByID, &meta.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message attachment: %w", err)
		}
		out[messageID] = append(out[messageID], meta)
	}
	return out, rows.Err()
}

func (s *AttachmentStore) LinkTask(ctx context.Context, taskID, attachmentID uuid.UUID, position int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO task_attachments (task_id, attachment_id, position)
		VALUES ($1, $2, $3)
		ON CONFLICT (task_id, attachment_id) DO NOTHING
	`, taskID, attachmentID, position)
	if err != nil {
		return fmt.Errorf("link task attachment: %w", err)
	}
	return nil
}

func (s *AttachmentStore) UnlinkTask(ctx context.Context, taskID, attachmentID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM task_attachments WHERE task_id = $1 AND attachment_id = $2
	`, taskID, attachmentID)
	if err != nil {
		return fmt.Errorf("unlink task attachment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task attachment link not found")
	}
	return nil
}

func (s *AttachmentStore) LinkMessage(ctx context.Context, messageID, attachmentID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO session_message_attachments (message_id, attachment_id)
		VALUES ($1, $2)
		ON CONFLICT (message_id, attachment_id) DO NOTHING
	`, messageID, attachmentID)
	if err != nil {
		return fmt.Errorf("link message attachment: %w", err)
	}
	return nil
}

func (s *AttachmentStore) ListMetaByCommentIDs(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	if len(commentIDs) == 0 {
		return map[uuid.UUID][]domain.AttachmentMeta{}, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT tca.comment_id, a.id, a.repository_id, a.filename, a.content_type, a.size_bytes, a.sha256, a.created_by_type, COALESCE(a.created_by_id, ''), a.created_at
		FROM task_comment_attachments tca
		JOIN attachments a ON a.id = tca.attachment_id
		WHERE tca.comment_id = ANY($1)
		ORDER BY a.created_at ASC
	`, commentIDs)
	if err != nil {
		return nil, fmt.Errorf("list comment attachments: %w", err)
	}
	defer rows.Close()
	out := make(map[uuid.UUID][]domain.AttachmentMeta)
	for rows.Next() {
		var commentID uuid.UUID
		var meta domain.AttachmentMeta
		if err := rows.Scan(&commentID, &meta.ID, &meta.RepositoryID, &meta.Filename, &meta.ContentType, &meta.SizeBytes, &meta.SHA256, &meta.CreatedByType, &meta.CreatedByID, &meta.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan comment attachment: %w", err)
		}
		out[commentID] = append(out[commentID], meta)
	}
	return out, rows.Err()
}

func (s *AttachmentStore) LinkComment(ctx context.Context, commentID, attachmentID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO task_comment_attachments (comment_id, attachment_id)
		VALUES ($1, $2)
		ON CONFLICT (comment_id, attachment_id) DO NOTHING
	`, commentID, attachmentID)
	if err != nil {
		return fmt.Errorf("link comment attachment: %w", err)
	}
	return nil
}

func (s *AttachmentStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM attachments WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("attachment not found")
	}
	return nil
}
