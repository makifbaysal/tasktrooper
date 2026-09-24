package domain

import "github.com/google/uuid"

// LegacyVercelCredential is the pre-cloud-accounts Vercel connection
// (app_settings vercel_token/vercel_team_id), read once by cloud.Service.Boot
// so an existing connection carries forward as a cloud_accounts row instead
// of asking the operator to reconnect.
type LegacyVercelCredential struct {
	Token  string
	TeamID string
}

// LegacyGCloudCredential is the pre-cloud-accounts Google Cloud service
// account (gcloud_credentials), read the same way. Fields is the decrypted
// payload gcloudops stored, always {"service_account_json": "..."}.
type LegacyGCloudCredential struct {
	ProjectID string
	Fields    map[string]string
}

// LegacyVercelProjectLink mirrors repository_vercel_projects: a monorepo's
// sub-project is SubProjectPath, the repository itself is "".
type LegacyVercelProjectLink struct {
	RepositoryID   uuid.UUID
	SubProjectPath string
	ProjectID      string
	ProjectName    string
	ProductionURL  string
}

// LegacyHostingLink mirrors repository_hosting_links. Area is HostingAreaRoot
// ("") for the repository itself, or a sub-repo kind (RepoKindFrontend,
// RepoKindBackend, …) for a monorepo area.
type LegacyHostingLink struct {
	RepositoryID  uuid.UUID
	Area          string
	Provider      string
	ExternalID    string
	ExternalName  string
	ProductionURL string
}

// LegacyGCloudResourceBinding mirrors repository_gcloud_resources.
type LegacyGCloudResourceBinding struct {
	RepositoryID   uuid.UUID
	SubProjectPath string
	ResourceType   string
	ResourceName   string
	DisplayName    string
	Location       string
}
