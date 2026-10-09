package executor

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type imageRegistry struct {
	port.ToolRegistry
	images []domain.ToolResultImage
}

func (r imageRegistry) ExecuteWithPolicy(context.Context, domain.ToolCall, domain.ToolPolicy) domain.ToolResult {
	return domain.ToolResult{Content: "ok", Images: r.images}
}

func attachments(t *testing.T, images []domain.ToolResultImage) []*AttachmentEvent {
	t.Helper()
	sink := &recordingSink{}
	reg := &eventingRegistry{ToolRegistry: imageRegistry{images: images}, em: newEmitter(sink, nil)}
	reg.Execute(context.Background(), domain.ToolCall{ID: "c1", Function: domain.FunctionCall{Name: "screenshot"}})
	var out []*AttachmentEvent
	for _, ev := range sink.events {
		if a, ok := ev.(*AttachmentEvent); ok {
			out = append(out, a)
		}
	}
	return out
}

func TestAttachmentEventCarriesImage(t *testing.T) {
	raw := []byte("png-bytes!")
	data := base64.StdEncoding.EncodeToString(raw)
	got := attachments(t, []domain.ToolResultImage{{MediaType: "image/png", Data: data}})
	require.Len(t, got, 1)
	assert.Equal(t, "attachment", got[0].Kind)
	assert.Equal(t, "c1", got[0].CallID)
	assert.Equal(t, "image/png", got[0].MIME)
	assert.Equal(t, len(raw), got[0].Size)
	assert.Equal(t, data, got[0].DataBase64)
	assert.False(t, got[0].TooLarge)
}

func TestAttachmentEventCapsAtTwoMB(t *testing.T) {
	atCap := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", attachmentSizeLimit)))
	over := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", attachmentSizeLimit+1)))
	got := attachments(t, []domain.ToolResultImage{
		{MediaType: "image/png", Data: atCap},
		{MediaType: "image/jpeg", Data: over},
	})
	require.Len(t, got, 2)
	assert.False(t, got[0].TooLarge)
	assert.NotEmpty(t, got[0].DataBase64)
	assert.True(t, got[1].TooLarge)
	assert.Equal(t, attachmentSizeLimit+1, got[1].Size)
	assert.Empty(t, got[1].DataBase64)
}

func TestNoAttachmentWithoutImages(t *testing.T) {
	assert.Empty(t, attachments(t, nil))
}
