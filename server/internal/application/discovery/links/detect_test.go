package links

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func mapFile(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content)}
}

func linkBySignal(links []domain.DetectedLink, componentPath, signalKey string) (domain.DetectedLink, bool) {
	for _, l := range links {
		if l.ComponentPath == componentPath && l.SignalKey == signalKey {
			return l, true
		}
	}
	return domain.DetectedLink{}, false
}

func deploySignal(signals []domain.DeploySignal, componentPath, provider string) (domain.DeploySignal, bool) {
	for _, s := range signals {
		if s.ComponentPath == componentPath && s.Provider == provider {
			return s, true
		}
	}
	return domain.DeploySignal{}, false
}

// 1. Go service with pgx + go-redis + stripe-go, and their env vars in
// .env.example: three links, each env var merged into its manifest link.
func TestDetect_GoServiceDependenciesAndEnv(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod": mapFile(`module github.com/acme/api

go 1.21

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/redis/go-redis/v9 v9.5.1
	github.com/stripe/stripe-go/v76 v76.0.0
)
`),
		".env.example": mapFile("DATABASE_URL=postgres://user:pass@localhost:5432/acme\nREDIS_URL=redis://localhost:6379\nSTRIPE_SECRET_KEY=sk_test_123\n"),
	}
	tree := inventory.FromFS(fsys)
	components := []domain.DetectedComponent{{Path: ".", PackageName: "github.com/acme/api"}}

	result := Detect(tree, components)

	require.Len(t, result.Links, 3, "expected exactly 3 links, got %+v", result.Links)

	pg, ok := linkBySignal(result.Links, ".", "dep:postgres")
	require.True(t, ok)
	require.Equal(t, domain.ConfidenceHigh, pg.Confidence)
	require.Equal(t, "postgres", pg.Target.Vendor)
	require.Contains(t, pg.EnvVars, "DATABASE_URL")

	redis, ok := linkBySignal(result.Links, ".", "dep:redis")
	require.True(t, ok)
	require.Equal(t, "redis", redis.Target.Vendor)
	require.Contains(t, redis.EnvVars, "REDIS_URL")

	stripe, ok := linkBySignal(result.Links, ".", "dep:stripe")
	require.True(t, ok)
	require.Equal(t, "stripe", stripe.Target.Vendor)
	require.Contains(t, stripe.EnvVars, "STRIPE_SECRET_KEY")
}

// 2. Monorepo: a same-repo localhost URL resolves to a component link, an
// unresolved port becomes a ServiceHint suggestion.
func TestDetect_MonorepoServiceURLs(t *testing.T) {
	fsys := fstest.MapFS{
		"apps/web/.env.example": mapFile("NEXT_PUBLIC_API_URL=http://localhost:8080\nBILLING_SVC_URL=http://localhost:4000\n"),
	}
	tree := inventory.FromFS(fsys)
	components := []domain.DetectedComponent{
		{Path: "apps/web", DevPort: 3000},
		{Path: "services/api", DevPort: 8080},
	}

	result := Detect(tree, components)

	apiLink, ok := linkBySignal(result.Links, "apps/web", "env:NEXT_PUBLIC_API_URL")
	require.True(t, ok)
	require.Equal(t, domain.LinkTargetComponent, apiLink.Target.Kind)
	require.Equal(t, "services/api", apiLink.Target.ComponentPath)
	require.Equal(t, domain.ConfidenceHigh, apiLink.Confidence)

	billing, ok := linkBySignal(result.Links, "apps/web", "env:BILLING_SVC_URL")
	require.True(t, ok)
	require.Equal(t, domain.LinkTargetUnresolved, billing.Target.Kind)
	require.Equal(t, "billing-svc", billing.Target.ServiceHint)
	require.Equal(t, 4000, billing.Target.Port)
	require.Equal(t, domain.ConfidenceMedium, billing.Confidence)
}

// 3. A secret-bearing .env is never read.
func TestDetect_RealEnvFileNotRead(t *testing.T) {
	fsys := fstest.MapFS{
		".env": mapFile("DATABASE_URL=postgres://user:pass@localhost:5432/acme\n"),
	}
	tree := inventory.FromFS(fsys)
	components := []domain.DetectedComponent{{Path: "."}}

	result := Detect(tree, components)

	require.Empty(t, result.Links)
}

// 4. docker-compose: a built service depending on an infra image gets a
// resource link, depending on another built service gets a component link.
func TestDetect_ComposeTopology(t *testing.T) {
	fsys := fstest.MapFS{
		"docker-compose.yml": mapFile(`services:
  db:
    image: postgres:15
  api:
    build:
      context: ./services/api
    depends_on:
      - db
  web:
    build:
      context: ./services/web
    depends_on:
      - api
`),
	}
	tree := inventory.FromFS(fsys)
	components := []domain.DetectedComponent{
		{Path: "services/api"},
		{Path: "services/web"},
	}

	result := Detect(tree, components)

	apiToDB, ok := linkBySignal(result.Links, "services/api", "compose:db")
	require.True(t, ok)
	require.Equal(t, domain.LinkTargetResource, apiToDB.Target.Kind)
	require.Equal(t, domain.ResourceDatabase, apiToDB.Target.ResourceKind)
	require.Equal(t, "postgres", apiToDB.Target.Vendor)
	require.Equal(t, domain.ConfidenceHigh, apiToDB.Confidence)

	webToAPI, ok := linkBySignal(result.Links, "services/web", "compose:api")
	require.True(t, ok)
	require.Equal(t, domain.LinkTargetComponent, webToAPI.Target.Kind)
	require.Equal(t, "services/api", webToAPI.Target.ComponentPath)
	require.Equal(t, domain.LinkHTTP, webToAPI.Protocol)
	require.Equal(t, domain.ConfidenceHigh, webToAPI.Confidence)
}

// 5. schema.prisma names the engine even without the prisma package present.
func TestDetect_PrismaSchemaEngine(t *testing.T) {
	fsys := fstest.MapFS{
		"prisma/schema.prisma": mapFile(`datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

generator client {
  provider = "prisma-client-js"
}
`),
	}
	tree := inventory.FromFS(fsys)
	components := []domain.DetectedComponent{{Path: "."}}

	result := Detect(tree, components)

	link, ok := linkBySignal(result.Links, ".", "dep:prisma")
	require.True(t, ok)
	require.Equal(t, domain.ResourceDatabase, link.Target.ResourceKind)
	require.Equal(t, "postgres", link.Target.Vendor)
	require.Equal(t, domain.ConfidenceHigh, link.Confidence)
}

// 6. A pnpm workspace dependency links the two components directly.
func TestDetect_WorkspacePackageLink(t *testing.T) {
	fsys := fstest.MapFS{
		"apps/web/package.json":    mapFile(`{"dependencies": {"@acme/ui": "workspace:*"}}`),
		"packages/ui/package.json": mapFile(`{"name": "@acme/ui"}`),
	}
	tree := inventory.FromFS(fsys)
	components := []domain.DetectedComponent{
		{Path: "apps/web", PackageName: "@acme/web"},
		{Path: "packages/ui", PackageName: "@acme/ui"},
	}

	result := Detect(tree, components)

	link, ok := linkBySignal(result.Links, "apps/web", "pkg:@acme/ui")
	require.True(t, ok)
	require.Equal(t, domain.LinkTargetComponent, link.Target.Kind)
	require.Equal(t, "packages/ui", link.Target.ComponentPath)
	require.Equal(t, domain.LinkPackage, link.Protocol)
	require.Equal(t, domain.ConfidenceHigh, link.Confidence)
}

// 7. Deploy markers: a linked Vercel project, a Cloud Run workflow step,
// a gcloud run line behind a cd prefix, serverless.yml and a Terraform ECS
// service resource.
func TestDetect_DeploySignals(t *testing.T) {
	fsys := fstest.MapFS{
		".vercel/project.json": mapFile(`{"projectId": "prj_123", "orgId": "org_456"}`),
		".github/workflows/deploy.yml": mapFile(`name: Deploy
on: push
jobs:
  deploy-api:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - name: Deploy Cloud Run
        uses: google-github-actions/deploy-cloudrun@v2
        with:
          service: acme-api
          region: europe-west1
        working-directory: services/api
  deploy-worker:
    runs-on: ubuntu-latest
    steps:
      - name: Deploy worker
        run: |
          cd services/worker
          gcloud run deploy acme-worker --region europe-west1
`),
		"serverless.yml": mapFile(`service: acme-serverless
provider:
  name: aws
`),
		"main.tf": mapFile(`resource "aws_ecs_service" "worker" {
  name    = "acme-worker-svc"
  cluster = "acme-cluster"
}
`),
	}
	tree := inventory.FromFS(fsys)
	components := []domain.DetectedComponent{
		{Path: "."},
		{Path: "services/api"},
		{Path: "services/worker"},
	}

	result := Detect(tree, components)

	vercel, ok := deploySignal(result.DeploySignals, ".", "vercel")
	require.True(t, ok)
	require.Equal(t, "prj_123", vercel.Ref["project_id"])
	require.Equal(t, "org_456", vercel.Ref["org_id"])
	require.Equal(t, domain.ConfidenceHigh, vercel.Confidence)

	cloudRun, ok := deploySignal(result.DeploySignals, "services/api", "gcp_cloud_run")
	require.True(t, ok)
	require.Equal(t, "acme-api", cloudRun.Ref["service"])
	require.Equal(t, "europe-west1", cloudRun.Ref["region"])
	require.Equal(t, domain.EnvironmentProduction, cloudRun.Environment)
	require.Equal(t, domain.ConfidenceHigh, cloudRun.Confidence)

	worker, ok := deploySignal(result.DeploySignals, "services/worker", "gcp_cloud_run")
	require.True(t, ok)
	require.Equal(t, "acme-worker", worker.Ref["service"])
	require.Equal(t, "europe-west1", worker.Ref["region"])

	serverless, ok := deploySignal(result.DeploySignals, ".", "aws_lambda")
	require.True(t, ok)
	require.Equal(t, "acme-serverless", serverless.Ref["service"])

	ecs, ok := deploySignal(result.DeploySignals, ".", "aws_ecs")
	require.True(t, ok)
	require.Equal(t, "worker", ecs.Ref["resource"])
	require.Equal(t, "acme-worker-svc", ecs.Ref["name"])
	require.Equal(t, domain.ConfidenceHigh, ecs.Confidence)
}

// 8. The embedded catalog parses and every entry names a valid resource kind
// and link protocol.
func TestCatalog_EntriesAreValid(t *testing.T) {
	require.NotEmpty(t, catalog.entries)
	seen := map[string]bool{}
	for _, e := range catalog.entries {
		require.NotEmpty(t, e.ID)
		require.False(t, seen[e.ID], "duplicate catalog id %q", e.ID)
		seen[e.ID] = true
		require.True(t, domain.ValidResourceKind(e.Resource.Kind), "entry %q has invalid resource kind %q", e.ID, e.Resource.Kind)
		require.True(t, domain.ValidLinkProtocol(e.Protocol), "entry %q has invalid protocol %q", e.ID, e.Protocol)
		require.NotEmpty(t, e.Resource.Name, "entry %q has no resource name", e.ID)
		require.True(t, e.Scope == "global" || e.Scope == "repo", "entry %q has invalid scope %q", e.ID, e.Scope)
	}
}

func TestDetect_GenericDatabaseURLJoinsManifestLink(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod":       mapFile("module github.com/acme/api\n\ngo 1.21\n\nrequire github.com/jackc/pgx/v5 v5.10.0\n"),
		".env.example": mapFile("DATABASE_URL=\nVITE_DEV_SERVER_URL=http://localhost:5173\n"),
	}
	result := Detect(inventory.FromFS(fsys), []domain.DetectedComponent{{Path: "."}})

	require.Len(t, result.Links, 1, "got %+v", result.Links)
	require.Equal(t, "dep:postgres", result.Links[0].SignalKey)
	require.Contains(t, result.Links[0].EnvVars, "DATABASE_URL")
}
