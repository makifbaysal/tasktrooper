package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestStoreTestSourceBuildsAPushedBranchWhenTheCheckoutIsElsewhere(t *testing.T) {
	task := &domain.BoardTask{ID: uuid.New(), Key: "T-54"}
	asked := ""
	src := &storeTestSource{
		workspaceRoot: t.TempDir(),
		remoteHead: func(_ context.Context, _ domain.Repository, branch string) (string, error) {
			asked = branch
			return "abc123", nil
		},
	}

	sha, branch, err := src.Publish(context.Background(), domain.Repository{}, task)
	if err != nil {
		t.Fatal(err)
	}
	if sha != "abc123" || branch != "feature/t-54" || asked != "feature/t-54" {
		t.Fatalf("got %s %s (asked %s)", sha, branch, asked)
	}

	if _, err := src.Checkout(context.Background(), domain.Repository{}, task); !errors.Is(err, errNoLocalCheckout) {
		t.Fatalf("Checkout err = %v, want errNoLocalCheckout: a local build needs the files here", err)
	}
}

func TestStoreTestSourceSaysWhyWhenTheBranchIsNotPushedEither(t *testing.T) {
	task := &domain.BoardTask{ID: uuid.New(), Key: "T-55"}
	src := &storeTestSource{
		workspaceRoot: t.TempDir(),
		remoteHead: func(context.Context, domain.Repository, string) (string, error) {
			return "", errors.New("404 Not Found")
		},
	}
	_, _, err := src.Head(context.Background(), domain.Repository{}, task)
	if !errors.Is(err, errNoLocalCheckout) {
		t.Fatalf("err = %v, want it to keep the missing-checkout cause", err)
	}
}
