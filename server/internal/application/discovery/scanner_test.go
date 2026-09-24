package discovery_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery"
	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func treeFromFiles(files map[string]string) *inventory.Tree {
	mapFS := fstest.MapFS{}
	for p, c := range files {
		mapFS[p] = &fstest.MapFile{Data: []byte(c)}
	}
	return inventory.FromFS(mapFS)
}

func componentsByPath(result domain.ScanResult) map[string]domain.DetectedComponent {
	out := map[string]domain.DetectedComponent{}
	for _, c := range result.Components {
		out[c.Path] = c
	}
	return out
}

func TestScanTreeFixtures(t *testing.T) {
	t.Run("single Go service: backend, commands from Makefile", func(t *testing.T) {
		files := map[string]string{
			"go.mod":   "module example.com/api\n\ngo 1.26\n",
			"main.go":  "package main\n\nimport \"net/http\"\n\nfunc main() {\n\thttp.ListenAndServe(\":8080\", nil)\n}\n",
			"Makefile": "build:\n\tgo build ./...\n\ntest:\n\tgo test ./...\n",
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Equal(t, domain.RepoShapeSingle, result.Shape)
		require.Len(t, result.Components, 1)
		c := result.Components[0]
		require.Equal(t, ".", c.Path)
		require.Equal(t, domain.ComponentRoleBackend, c.Role)

		byPurpose := map[domain.CommandPurpose]string{}
		for _, cmd := range c.Commands {
			byPurpose[cmd.Purpose] = cmd.Command
		}
		require.Equal(t, "make build", byPurpose[domain.CommandBuild])
		require.Equal(t, "make test", byPurpose[domain.CommandTest])
	})

	t.Run("Next.js app: frontend, pnpm, dev port 3000", func(t *testing.T) {
		files := map[string]string{
			"package.json": `{
				"name": "web",
				"scripts": {"dev": "next dev", "build": "next build"},
				"dependencies": {"next": "14.2.0", "react": "18.3.0", "react-dom": "18.3.0"}
			}`,
			"pnpm-lock.yaml": "lockfileVersion: '6.0'\n",
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Equal(t, domain.RepoShapeSingle, result.Shape)
		require.Len(t, result.Components, 1)
		c := result.Components[0]
		require.Equal(t, domain.ComponentRoleFrontend, c.Role)
		require.Equal(t, "pnpm", c.Stack.PackageManager)
		require.Equal(t, 3000, c.DevPort)
	})

	t.Run("pnpm monorepo: 3 components, root not a component", func(t *testing.T) {
		files := map[string]string{
			"pnpm-workspace.yaml": "packages:\n  - 'apps/*'\n  - 'packages/*'\n",
			"package.json": `{
				"name": "monorepo", "private": true,
				"workspaces": ["apps/*", "packages/*"],
				"devDependencies": {"turbo": "2.0.0"},
				"scripts": {"build": "turbo run build"}
			}`,
			"apps/web/package.json":    `{"name":"web","scripts":{"dev":"next dev"},"dependencies":{"next":"14.2.0"}}`,
			"apps/api/package.json":    `{"name":"api","scripts":{"start:dev":"nest start --watch"},"dependencies":{"@nestjs/core":"10.0.0"}}`,
			"packages/ui/package.json": `{"name":"@acme/ui","main":"index.js","dependencies":{},"scripts":{}}`,
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Equal(t, domain.RepoShapeMonorepo, result.Shape)
		require.Len(t, result.Components, 3)

		byPath := componentsByPath(result)
		_, rootIsComponent := byPath["."]
		require.False(t, rootIsComponent, "a root package.json that only declares workspaces is not a component")

		require.Equal(t, domain.ComponentRoleFrontend, byPath["apps/web"].Role)
		require.Equal(t, domain.ComponentRoleBackend, byPath["apps/api"].Role)
		require.Equal(t, domain.ComponentRoleLibrary, byPath["packages/ui"].Role)

		var sawWorkspaceEvidence bool
		for _, ev := range result.ShapeEvidence {
			if ev.Path == "pnpm-workspace.yaml" {
				sawWorkspaceEvidence = true
			}
		}
		require.True(t, sawWorkspaceEvidence, "expected pnpm-workspace.yaml in the shape evidence: %+v", result.ShapeEvidence)
	})

	t.Run("Flutter app: ios/android absorbed into one mobile component", func(t *testing.T) {
		files := map[string]string{
			"pubspec.yaml":                         "name: acme\ndependencies:\n  flutter:\n    sdk: flutter\n",
			"ios/Runner.xcodeproj/project.pbxproj": "// !$*UTF8*$!\n{\n\tobjects = {\n\t\tbuildSettings = {\n\t\t\tPRODUCT_BUNDLE_IDENTIFIER = com.acme.app;\n\t\t};\n\t};\n}\n",
			"android/app/build.gradle":             "android {\n    defaultConfig {\n        applicationId \"com.acme.app\"\n    }\n}\n",
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Equal(t, domain.RepoShapeSingle, result.Shape)
		require.Len(t, result.Components, 1)
		c := result.Components[0]
		require.Equal(t, domain.ComponentRoleMobile, c.Role)
		require.NotNil(t, c.Mobile)
		require.Equal(t, "com.acme.app", c.Mobile.Identity.BundleID)
		require.Equal(t, "com.acme.app", c.Mobile.Identity.PackageName)
	})

	t.Run("Electron app: desktop", func(t *testing.T) {
		files := map[string]string{
			"package.json": `{"name":"shell","dependencies":{"electron":"30.0.0"}}`,
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Len(t, result.Components, 1)
		require.Equal(t, domain.ComponentRoleDesktop, result.Components[0].Role)
	})

	t.Run("Go worker: no listener, pubsub consumer", func(t *testing.T) {
		files := map[string]string{
			"go.mod":  "module example.com/worker\n\ngo 1.26\n",
			"main.go": "package main\n\nimport \"cloud.google.com/go/pubsub\"\n\nfunc main() {\n\t_ = pubsub.NewClient\n}\n",
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Len(t, result.Components, 1)
		require.Equal(t, domain.ComponentRoleWorker, result.Components[0].Role)
	})

	t.Run("Terraform-only repo: infra", func(t *testing.T) {
		files := map[string]string{
			"main.tf":      "resource \"google_project\" \"x\" {}\n",
			"variables.tf": "variable \"x\" {}\n",
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Equal(t, domain.RepoShapeSingle, result.Shape)
		require.Len(t, result.Components, 1)
		c := result.Components[0]
		require.Equal(t, ".", c.Path)
		require.Equal(t, domain.ComponentRoleInfra, c.Role)
		require.Equal(t, domain.ConfidenceLow, c.RoleConfidence)
	})

	t.Run("package.json with only workspaces and turbo: root is not a component", func(t *testing.T) {
		files := map[string]string{
			"package.json": `{
				"name": "monorepo", "private": true,
				"workspaces": ["packages/*"],
				"devDependencies": {"turbo": "2.0.0"},
				"scripts": {"build": "turbo run build", "dev": "turbo run dev"}
			}`,
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Len(t, result.Components, 1)
		c := result.Components[0]
		require.Equal(t, ".", c.Path)
		require.NotEqual(t, domain.ComponentRoleLibrary, c.Role, "an orchestration-only root must not be mistaken for a library")
	})

	t.Run("nested roots like this repo: three independent components", func(t *testing.T) {
		files := map[string]string{
			"server/go.mod":  "module github.com/acme/server\n\ngo 1.26\n",
			"server/main.go": "package main\n\nimport \"net/http\"\n\nfunc main() {\n\thttp.ListenAndServe(\":8080\", nil)\n}\n",
			"desktop/package.json": `{
				"name": "desktop", "main": "dist/main/index.cjs",
				"scripts": {"dev": "electron .", "build": "tsc"},
				"dependencies": {"electron": "30.0.0"}
			}`,
			"desktop/ui/package.json": `{
				"name": "ui",
				"scripts": {"dev": "vite", "build": "vite build"},
				"dependencies": {"react": "19.0.0", "react-dom": "19.0.0"},
				"devDependencies": {"vite": "5.0.0"}
			}`,
		}
		result := discovery.ScanTree(context.Background(), treeFromFiles(files), nil)
		require.Equal(t, domain.RepoShapeMonorepo, result.Shape)
		require.Len(t, result.Components, 3)

		byPath := componentsByPath(result)
		require.Equal(t, domain.ComponentRoleBackend, byPath["server"].Role)
		require.Equal(t, domain.ComponentRoleDesktop, byPath["desktop"].Role)
		require.Equal(t, domain.ComponentRoleFrontend, byPath["desktop/ui"].Role)
	})
}

// repoRoot resolves the monorepo root from this test file's own location,
// so the real-repo assertion below survives the file moving as long as it
// stays four levels under the repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "../../../.."))
	require.NoError(t, err)
	if _, statErr := os.Stat(filepath.Join(root, "server", "go.mod")); statErr != nil {
		t.Skipf("could not resolve repo root from %s: %v", file, statErr)
	}
	return root
}

func TestScanRealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	root := repoRoot(t)

	result, err := discovery.New().Scan(context.Background(), root, nil)
	require.NoError(t, err)
	require.Equal(t, domain.RepoShapeMonorepo, result.Shape)

	byPath := componentsByPath(result)
	server, ok := byPath["server"]
	require.True(t, ok, "expected a server component: %+v", result.Components)
	require.Equal(t, domain.ComponentRoleBackend, server.Role)

	desktopUI, ok := byPath["desktop/ui"]
	require.True(t, ok, "expected a desktop/ui component: %+v", result.Components)
	require.Equal(t, domain.ComponentRoleFrontend, desktopUI.Role)

	desktop, ok := byPath["desktop"]
	require.True(t, ok, "expected a desktop component: %+v", result.Components)
	require.Equal(t, domain.ComponentRoleDesktop, desktop.Role)
}
