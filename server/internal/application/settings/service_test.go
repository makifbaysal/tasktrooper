package settings_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/settings"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeSettingsStore struct {
	got        domain.AppSettings
	updateReq  domain.UpdateSettingsRequest
	updateCall int
}

func (f *fakeSettingsStore) Get(context.Context) (domain.AppSettings, error) { return f.got, nil }

func (f *fakeSettingsStore) Update(_ context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error) {
	f.updateCall++
	f.updateReq = req
	return f.got, nil
}

func TestGetReturnsStoreSettings(t *testing.T) {
	store := &fakeSettingsStore{got: domain.AppSettings{WorkspaceRoot: "/tmp/ws"}}
	svc := settings.NewService(store)

	out, err := svc.Get(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "/tmp/ws", out.WorkspaceRoot)
}

func TestUpdateDelegatesToStore(t *testing.T) {
	store := &fakeSettingsStore{}
	svc := settings.NewService(store)

	_, err := svc.Update(context.Background(), domain.UpdateSettingsRequest{DefaultLanguage: "tr"})

	require.NoError(t, err)
	assert.Equal(t, 1, store.updateCall)
	assert.Equal(t, "tr", store.updateReq.DefaultLanguage)
}
