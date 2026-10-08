package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type MCPSecretRecord struct {
	Location string
	Key      string
	Value    []byte
}

type MCPStore interface {
	List(ctx context.Context) ([]domain.MCPServer, error)
	Get(ctx context.Context, id string) (domain.MCPServer, error)
	Create(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error)
	Update(ctx context.Context, server domain.MCPServer) (domain.MCPServer, error)
	Delete(ctx context.Context, id string) error
	// SeededTemplateIDs lists the shipped templates this install has already
	// been given, whether or not the server is still on file.
	SeededTemplateIDs(ctx context.Context) ([]string, error)
	MarkTemplatesSeeded(ctx context.Context, ids []string) error
	ListSecrets(ctx context.Context, serverID string) ([]MCPSecretRecord, error)
	SetSecret(ctx context.Context, serverID, location, key string, encrypted []byte) error
	DeleteSecret(ctx context.Context, serverID, location, key string) error
	DeleteSecretsForServer(ctx context.Context, serverID string) error
}
