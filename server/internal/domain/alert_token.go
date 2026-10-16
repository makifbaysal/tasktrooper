package domain

import (
	"time"

	"github.com/google/uuid"
)

// AlertIngestToken lets an external alerter post to one repository's incident
// ingest without the server API key. Only the token's sha256 is stored; the
// plaintext exists once, in the create response.
type AlertIngestToken struct {
	ID           uuid.UUID  `json:"id"`
	RepositoryID uuid.UUID  `json:"-"`
	Label        string     `json:"label"`
	DefaultEnv   string     `json:"default_env"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}
