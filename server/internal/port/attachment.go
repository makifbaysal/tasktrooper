package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type AttachmentStore interface {
	Create(ctx context.Context, att domain.Attachment) (domain.AttachmentMeta, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Attachment, error)
	GetMeta(ctx context.Context, id uuid.UUID) (domain.AttachmentMeta, error)
	ListMetaByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AttachmentMeta, error)
	ListMetaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error)
	LinkTask(ctx context.Context, taskID, attachmentID uuid.UUID, position int) error
	UnlinkTask(ctx context.Context, taskID, attachmentID uuid.UUID) error
	LinkMessage(ctx context.Context, messageID, attachmentID uuid.UUID) error
	ListMetaByCommentIDs(ctx context.Context, commentIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error)
	LinkComment(ctx context.Context, commentID, attachmentID uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}
