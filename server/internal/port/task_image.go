package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// TaskImageAttacher stores an image a tool produced as an attachment of the
// board task a run is working, where the human sees it on the task.
type TaskImageAttacher interface {
	AttachToTask(ctx context.Context, repositoryID, taskID uuid.UUID, filename, contentType string, data []byte) (domain.AttachmentMeta, error)
}
