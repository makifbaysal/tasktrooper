package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	store port.AttachmentStore
	tasks port.BoardTaskStore
}

func NewService(store port.AttachmentStore, tasks port.BoardTaskStore) *Service {
	return &Service{store: store, tasks: tasks}
}

func (s *Service) Upload(ctx context.Context, filename, contentType string, data []byte, repositoryID *uuid.UUID, createdByType, createdByID string) (domain.AttachmentMeta, error) {
	if len(data) == 0 {
		return domain.AttachmentMeta{}, fmt.Errorf("attachment is empty")
	}
	if int64(len(data)) > domain.MaxAttachmentBytes {
		return domain.AttachmentMeta{}, domain.ErrAttachmentTooLarge
	}
	resolved, err := resolveContentType(contentType, data)
	if err != nil {
		return domain.AttachmentMeta{}, err
	}
	if createdByType == "" {
		createdByType = "user"
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "attachment"
	}
	sum := sha256.Sum256(data)
	return s.store.Create(ctx, domain.Attachment{
		RepositoryID:  repositoryID,
		Filename:      filename,
		ContentType:   resolved,
		SizeBytes:     int64(len(data)),
		SHA256:        hex.EncodeToString(sum[:]),
		Data:          data,
		CreatedByType: createdByType,
		CreatedByID:   createdByID,
	})
}

func resolveContentType(declared string, data []byte) (string, error) {
	base := ""
	if declared != "" {
		if parsed, _, err := mime.ParseMediaType(declared); err == nil {
			base = parsed
		}
	}
	if base == "" || base == "application/octet-stream" {
		sniffed, _, err := mime.ParseMediaType(http.DetectContentType(data))
		if err != nil {
			return "", fmt.Errorf("%w: undetectable content type", domain.ErrAttachmentTypeNotAllowed)
		}
		base = sniffed
	}
	if !domain.AllowedAttachmentTypes[base] {
		return "", fmt.Errorf("%w: %s", domain.ErrAttachmentTypeNotAllowed, base)
	}
	return base, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Attachment, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) GetMeta(ctx context.Context, id uuid.UUID) (domain.AttachmentMeta, error) {
	return s.store.GetMeta(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.store.Delete(ctx, id)
}

func (s *Service) ListByTask(ctx context.Context, taskID uuid.UUID) ([]domain.AttachmentMeta, error) {
	return s.store.ListMetaByTask(ctx, taskID)
}

func (s *Service) ListMetaByMessageIDs(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	return s.store.ListMetaByMessageIDs(ctx, messageIDs)
}

func (s *Service) LinkTask(ctx context.Context, repositoryID, taskID, attachmentID uuid.UUID) (domain.AttachmentMeta, error) {
	if s.tasks != nil {
		if _, err := s.tasks.Get(ctx, repositoryID, taskID); err != nil {
			return domain.AttachmentMeta{}, fmt.Errorf("task not found: %w", err)
		}
	}
	meta, err := s.store.GetMeta(ctx, attachmentID)
	if err != nil {
		return domain.AttachmentMeta{}, fmt.Errorf("attachment not found: %w", err)
	}
	existing, err := s.store.ListMetaByTask(ctx, taskID)
	if err != nil {
		return domain.AttachmentMeta{}, err
	}
	if err := s.store.LinkTask(ctx, taskID, attachmentID, len(existing)); err != nil {
		return domain.AttachmentMeta{}, err
	}
	return meta, nil
}

func (s *Service) UnlinkTask(ctx context.Context, taskID, attachmentID uuid.UUID) error {
	return s.store.UnlinkTask(ctx, taskID, attachmentID)
}

func (s *Service) LinkMessage(ctx context.Context, messageID, attachmentID uuid.UUID) error {
	return s.store.LinkMessage(ctx, messageID, attachmentID)
}

// AttachToTask stores a tool's image (a screenshot) and links it to the task
// the run is working.
func (s *Service) AttachToTask(ctx context.Context, repositoryID, taskID uuid.UUID, filename, contentType string, data []byte) (domain.AttachmentMeta, error) {
	meta, err := s.Upload(ctx, filename, contentType, data, &repositoryID, "agent", "")
	if err != nil {
		return domain.AttachmentMeta{}, err
	}
	return s.LinkTask(ctx, repositoryID, taskID, meta.ID)
}
