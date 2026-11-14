package session_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/session"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeAttachmentStore struct {
	metas   map[uuid.UUID][]domain.AttachmentMeta
	data    map[uuid.UUID][]byte
	listErr error
	failGet map[uuid.UUID]bool
	gets    int
}

func (f *fakeAttachmentStore) LinkMessage(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeAttachmentStore) ListMetaByMessageIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]domain.AttachmentMeta, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make(map[uuid.UUID][]domain.AttachmentMeta, len(ids))
	for _, id := range ids {
		if metas, ok := f.metas[id]; ok {
			out[id] = metas
		}
	}
	return out, nil
}

func (f *fakeAttachmentStore) Get(_ context.Context, id uuid.UUID) (domain.Attachment, error) {
	f.gets++
	if f.failGet[id] {
		return domain.Attachment{}, fmt.Errorf("bytes unavailable")
	}
	return domain.Attachment{ID: id, Data: f.data[id]}, nil
}

func (f *fakeAttachmentStore) image(msgID uuid.UUID, contentType string, size int64, payload string) uuid.UUID {
	if f.metas == nil {
		f.metas = map[uuid.UUID][]domain.AttachmentMeta{}
		f.data = map[uuid.UUID][]byte{}
	}
	id := uuid.New()
	f.metas[msgID] = append(f.metas[msgID], domain.AttachmentMeta{
		ID: id, ContentType: contentType, SizeBytes: size, Filename: payload,
	})
	f.data[id] = []byte(payload)
	return id
}

type ImageAttachmentsSuite struct {
	suite.Suite
}

func transcript(roles ...domain.Role) ([]domain.SessionMessage, []domain.Message) {
	rows := make([]domain.SessionMessage, 0, len(roles))
	history := make([]domain.Message, 0, len(roles))
	for i, r := range roles {
		rows = append(rows, domain.SessionMessage{ID: uuid.New(), Role: r, Content: fmt.Sprintf("m%d", i)})
		history = append(history, domain.Message{Role: r, Content: fmt.Sprintf("m%d", i)})
	}
	return rows, history
}

func (s *ImageAttachmentsSuite) TestImageRidesAlongAsBase64() {
	rows, history := transcript(domain.RoleUser)
	store := &fakeAttachmentStore{}
	store.image(rows[0].ID, "image/png", 1024, "pixels")

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	s.Require().Len(history[0].Images, 1)
	s.Equal("image/png", history[0].Images[0].MediaType)
	s.Equal(base64.StdEncoding.EncodeToString([]byte("pixels")), history[0].Images[0].Data)
}

func (s *ImageAttachmentsSuite) TestOnlyImageContentTypesGoInImagesOthersGoInFiles() {
	rows, history := transcript(domain.RoleUser)
	store := &fakeAttachmentStore{}
	store.image(rows[0].ID, "application/pdf", 1024, "doc")
	store.image(rows[0].ID, "text/plain", 10, "notes")
	store.image(rows[0].ID, "image/webp", 2048, "pixels")

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	s.Require().Len(history[0].Images, 1)
	s.Equal("image/webp", history[0].Images[0].MediaType)

	s.Require().Len(history[0].Files, 2)
	s.Equal("application/pdf", history[0].Files[0].MediaType)
	s.Equal(base64.StdEncoding.EncodeToString([]byte("doc")), history[0].Files[0].Data)
	s.Equal("text/plain", history[0].Files[1].MediaType)

	s.Equal(3, store.gets)
}

func (s *ImageAttachmentsSuite) TestFilesOverTheSizeCapAreSkipped() {
	rows, history := transcript(domain.RoleUser)
	store := &fakeAttachmentStore{}
	store.image(rows[0].ID, "application/pdf", (5<<20)+1, "too big")
	store.image(rows[0].ID, "text/plain", 5<<20, "exactly at the cap")

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	s.Require().Len(history[0].Files, 1)
	s.Equal(base64.StdEncoding.EncodeToString([]byte("exactly at the cap")), history[0].Files[0].Data)
}

func (s *ImageAttachmentsSuite) TestOnlyTheSixNewestFilesSurvive() {
	rows, history := transcript(
		domain.RoleUser, domain.RoleAssistant,
		domain.RoleUser, domain.RoleAssistant,
		domain.RoleUser, domain.RoleAssistant,
		domain.RoleUser,
	)
	store := &fakeAttachmentStore{}
	for _, idx := range []int{0, 2, 4, 6} {
		store.image(rows[idx].ID, "text/plain", 100, fmt.Sprintf("a%d", idx))
		store.image(rows[idx].ID, "text/plain", 100, fmt.Sprintf("b%d", idx))
	}

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	total := 0
	for _, m := range history {
		total += len(m.Files)
	}
	s.Equal(6, total, "the cap must hold across the whole history")
	s.Empty(history[0].Files, "the oldest turn's files are the ones dropped")
	s.Len(history[6].Files, 2)
}

func (s *ImageAttachmentsSuite) TestImagesOverTheSizeCapAreSkipped() {
	rows, history := transcript(domain.RoleUser)
	store := &fakeAttachmentStore{}
	store.image(rows[0].ID, "image/png", (5<<20)+1, "too big")
	store.image(rows[0].ID, "image/png", 5<<20, "exactly at the cap")

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	s.Require().Len(history[0].Images, 1)
	s.Equal(base64.StdEncoding.EncodeToString([]byte("exactly at the cap")), history[0].Images[0].Data)
}

func (s *ImageAttachmentsSuite) TestOnlyTheSixNewestImagesSurvive() {
	rows, history := transcript(
		domain.RoleUser, domain.RoleAssistant,
		domain.RoleUser, domain.RoleAssistant,
		domain.RoleUser, domain.RoleAssistant,
		domain.RoleUser,
	)
	store := &fakeAttachmentStore{}
	for _, idx := range []int{0, 2, 4, 6} {
		store.image(rows[idx].ID, "image/png", 100, fmt.Sprintf("a%d", idx))
		store.image(rows[idx].ID, "image/png", 100, fmt.Sprintf("b%d", idx))
	}

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	total := 0
	for _, m := range history {
		total += len(m.Images)
	}
	s.Equal(6, total, "the cap must hold across the whole history")
	s.Empty(history[0].Images, "the oldest turn's images are the ones dropped")
	s.Len(history[2].Images, 2)
	s.Len(history[4].Images, 2)
	s.Len(history[6].Images, 2)

	s.Equal(base64.StdEncoding.EncodeToString([]byte("a6")), history[6].Images[0].Data)
	s.Equal(base64.StdEncoding.EncodeToString([]byte("b6")), history[6].Images[1].Data)
}

func (s *ImageAttachmentsSuite) TestUnreadableImageIsSkippedNotFatal() {
	rows, history := transcript(domain.RoleUser)
	store := &fakeAttachmentStore{}
	broken := store.image(rows[0].ID, "image/png", 100, "gone")
	store.image(rows[0].ID, "image/png", 100, "fine")
	store.failGet = map[uuid.UUID]bool{broken: true}

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	s.Require().Len(history[0].Images, 1)
	s.Equal(base64.StdEncoding.EncodeToString([]byte("fine")), history[0].Images[0].Data)
}

func (s *ImageAttachmentsSuite) TestMetadataReadFailureLeavesTheHistoryIntact() {
	rows, history := transcript(domain.RoleUser)
	store := &fakeAttachmentStore{listErr: context.DeadlineExceeded}
	store.image(rows[0].ID, "image/png", 100, "pixels")

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	s.Empty(history[0].Images)
	s.Equal("m0", history[0].Content)
}

func (s *ImageAttachmentsSuite) TestNonUserMessagesAreUntouched() {
	rows, history := transcript(domain.RoleAssistant, domain.RoleTool, domain.RoleSystem)
	store := &fakeAttachmentStore{}
	for i := range rows {
		store.image(rows[i].ID, "image/png", 100, "pixels")
	}

	session.AttachImageAttachmentsForTest(context.Background(), store, rows, history)

	for i := range history {
		s.Empty(history[i].Images, "role %s gained images", history[i].Role)
	}
	s.Zero(store.gets)
}

func (s *ImageAttachmentsSuite) TestNilStoreIsANoOp() {
	rows, history := transcript(domain.RoleUser)

	session.AttachImageAttachmentsForTest(context.Background(), nil, rows, history)

	s.Empty(history[0].Images)
}

func TestImageAttachmentsSuite(t *testing.T) {
	suite.Run(t, new(ImageAttachmentsSuite))
}
