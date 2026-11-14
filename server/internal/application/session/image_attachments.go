package session

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const maxLLMImagesPerRequest = 6

const maxLLMImageBytes = 5 << 20

const maxLLMFilesPerRequest = 6

const maxLLMFileBytes = 5 << 20

func attachImageAttachments(ctx context.Context, store AttachmentLinker, rows []domain.SessionMessage, history []domain.Message) {
	if store == nil || len(rows) == 0 || len(rows) != len(history) {
		return
	}

	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.Role == domain.RoleUser && r.ID != uuid.Nil {
			ids = append(ids, r.ID)
		}
	}
	if len(ids) == 0 {
		return
	}

	metas, err := store.ListMetaByMessageIDs(ctx, ids)
	if err != nil {
		log.Warn().Err(err).Msg("chat attachment metadata unreadable, sending the conversation without its images")
		return
	}
	if len(metas) == 0 {
		return
	}

	type pick struct {
		index int
		meta  domain.AttachmentMeta
	}

	imagePicks := make([]pick, 0, maxLLMImagesPerRequest)
	filePicks := make([]pick, 0, maxLLMFilesPerRequest)
	for i := len(rows) - 1; i >= 0 && (len(imagePicks) < maxLLMImagesPerRequest || len(filePicks) < maxLLMFilesPerRequest); i-- {
		if rows[i].Role != domain.RoleUser {
			continue
		}
		for _, meta := range metas[rows[i].ID] {
			if strings.HasPrefix(meta.ContentType, "image/") {
				if meta.SizeBytes > maxLLMImageBytes || len(imagePicks) >= maxLLMImagesPerRequest {
					continue
				}
				imagePicks = append(imagePicks, pick{index: i, meta: meta})
				continue
			}
			if meta.SizeBytes > maxLLMFileBytes || len(filePicks) >= maxLLMFilesPerRequest {
				continue
			}
			filePicks = append(filePicks, pick{index: i, meta: meta})
		}
	}

	for _, p := range imagePicks {
		att, err := store.Get(ctx, p.meta.ID)
		if err != nil {
			log.Warn().Err(err).Str("attachment_id", p.meta.ID.String()).
				Msg("chat image attachment unreadable, skipping it")
			continue
		}
		if len(att.Data) == 0 {
			continue
		}
		history[p.index].Images = append(history[p.index].Images, domain.ToolResultImage{
			MediaType: p.meta.ContentType,
			Data:      base64.StdEncoding.EncodeToString(att.Data),
		})
	}

	for _, p := range filePicks {
		att, err := store.Get(ctx, p.meta.ID)
		if err != nil {
			log.Warn().Err(err).Str("attachment_id", p.meta.ID.String()).
				Msg("chat file attachment unreadable, skipping it")
			continue
		}
		if len(att.Data) == 0 {
			continue
		}
		history[p.index].Files = append(history[p.index].Files, domain.AttachedFile{
			Filename:  p.meta.Filename,
			MediaType: p.meta.ContentType,
			Data:      base64.StdEncoding.EncodeToString(att.Data),
		})
	}
}
