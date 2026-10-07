package http

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/storeops"
)

func TestStoreTestBuildRoutesRefuseAMalformedBuildID(t *testing.T) {
	app := fiber.New()
	h := &Handler{storeOpsSvc: storeops.NewService(storeops.Deps{})}
	h.registerStoreTestBuildRoutes(app)

	for _, path := range []string{
		"/v1/repositories/" + uuid.NewString() + "/store/test-builds/not-a-uuid",
		"/v1/repositories/not-a-uuid/store/test-builds/" + uuid.NewString(),
	} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", path, resp.StatusCode)
		}
	}
}
