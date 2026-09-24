package components

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestClassifyRole(t *testing.T) {
	cases := []struct {
		name   string
		layout fileset
		dir    string
		info   *ManifestInfo
		role   domain.ComponentRole
		conf   domain.Confidence
	}{
		{
			name: "flutter is mobile",
			dir:  ".",
			info: &ManifestInfo{Ecosystem: "dart", Path: "pubspec.yaml", Dependencies: map[string]string{"flutter": ""}},
			role: domain.ComponentRoleMobile, conf: domain.ConfidenceHigh,
		},
		{
			name: "electron is desktop",
			dir:  ".",
			info: &ManifestInfo{Ecosystem: "node", Path: "package.json", Dependencies: map[string]string{"electron": "^30.0.0"}},
			role: domain.ComponentRoleDesktop, conf: domain.ConfidenceHigh,
		},
		{
			name: "next is high-confidence frontend",
			dir:  ".",
			info: &ManifestInfo{Ecosystem: "node", Path: "package.json", Dependencies: map[string]string{"next": "^14.0.0", "react": "^18.0.0"}},
			role: domain.ComponentRoleFrontend, conf: domain.ConfidenceHigh,
		},
		{
			name: "react-dom alone is medium-confidence frontend",
			dir:  ".",
			info: &ManifestInfo{Ecosystem: "node", Path: "package.json", Dependencies: map[string]string{"react-dom": "^18.0.0"}},
			role: domain.ComponentRoleFrontend, conf: domain.ConfidenceMedium,
		},
		{
			name: "chi is high-confidence Go backend",
			dir:  ".",
			info: &ManifestInfo{Ecosystem: "go", Path: "go.mod", Dependencies: map[string]string{"github.com/go-chi/chi/v5": "5.0.0"}},
			role: domain.ComponentRoleBackend, conf: domain.ConfidenceHigh,
		},
		{
			name: "a bare ListenAndServe is medium-confidence Go backend",
			dir:  ".",
			layout: fileset{
				"main.go": "package main\n\nfunc main() {\n\thttp.ListenAndServe(\":8080\", nil)\n}\n",
			},
			info: &ManifestInfo{Ecosystem: "go", Path: "go.mod", Dependencies: map[string]string{}},
			role: domain.ComponentRoleBackend, conf: domain.ConfidenceMedium,
		},
		{
			name: "a Go main with a pubsub consumer and no listener is a worker",
			dir:  "worker",
			layout: fileset{
				"worker/main.go": "package main\n\nimport \"cloud.google.com/go/pubsub\"\n\nfunc main() {\n\t_ = pubsub.NewClient\n}\n",
			},
			info: &ManifestInfo{Ecosystem: "go", Path: "worker/go.mod", Dependencies: map[string]string{}},
			role: domain.ComponentRoleWorker, conf: domain.ConfidenceMedium,
		},
		{
			name: "a node package with a bin entry is a cli",
			dir:  ".",
			info: &ManifestInfo{Ecosystem: "node", Path: "package.json", Bin: true, Dependencies: map[string]string{}},
			role: domain.ComponentRoleCLI, conf: domain.ConfidenceMedium,
		},
		{
			name: "a node package under packages/ with no runnable script is a high-confidence library",
			dir:  "packages/ui",
			info: &ManifestInfo{Ecosystem: "node", Path: "packages/ui/package.json", NoOwnSource: true, Dependencies: map[string]string{}},
			role: domain.ComponentRoleLibrary, conf: domain.ConfidenceHigh,
		},
		{
			name: "a Go module with no package main is a library",
			dir:  ".",
			layout: fileset{
				"lib.go": "package acme\n\nfunc Do() {}\n",
			},
			info: &ManifestInfo{Ecosystem: "go", Path: "go.mod", Dependencies: map[string]string{}},
			role: domain.ComponentRoleLibrary, conf: domain.ConfidenceHigh,
		},
		{
			name:   "a Terraform-only directory with no manifest is infra",
			dir:    ".",
			layout: fileset{"main.tf": "resource \"x\" \"y\" {}\n"},
			info:   nil,
			role:   domain.ComponentRoleInfra, conf: domain.ConfidenceLow,
		},
		{
			name:   "nothing recognisable falls back to other",
			dir:    ".",
			layout: fileset{"README.md": "hi\n"},
			info:   nil,
			role:   domain.ComponentRoleOther, conf: domain.ConfidenceLow,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := treeFrom(t, tc.layout)
			role, conf, _ := classifyRole(tree, tc.dir, tc.info)
			require.Equal(t, tc.role, role)
			require.Equal(t, tc.conf, conf)
		})
	}
}

func TestBuildStack(t *testing.T) {
	tree := treeFrom(t, fileset{
		"pnpm-lock.yaml": "lockfileVersion: '6.0'\n",
		"Dockerfile":     "FROM node:20 AS build\nRUN npm run build\nFROM gcr.io/distroless/nodejs20-debian12\nCOPY --from=build /app /app\n",
		"src/index.tsx":  "export {}\n",
		"src/app.ts":     "export {}\n",
	})
	info := &ManifestInfo{
		Ecosystem: "node", Path: "package.json",
		Dependencies:    map[string]string{"next": "^14.2.0", "tailwindcss": "^3.4.0"},
		DevDependencies: map[string]string{},
	}
	stack := buildStack(tree, ".", info)

	require.Equal(t, "pnpm", stack.PackageManager)
	require.Equal(t, "Docker · gcr.io/distroless/nodejs20-debian12", stack.Container)

	var frameworkNamesFound []string
	for _, f := range stack.Frameworks {
		frameworkNamesFound = append(frameworkNamesFound, f.Name)
	}
	require.Contains(t, frameworkNamesFound, "Next.js")

	var libNamesFound []string
	for _, l := range stack.Libraries {
		libNamesFound = append(libNamesFound, l.Name)
	}
	require.Contains(t, libNamesFound, "Tailwind CSS")

	require.NotEmpty(t, stack.Languages)
	require.Equal(t, "TypeScript", stack.Languages[0].Name)
}

func TestBuildCommandsMakefileBeatsEcosystemDefault(t *testing.T) {
	tree := treeFrom(t, fileset{
		"Makefile": "build:\n\tgo build ./...\n\ntest:\n\tgo test ./... -race\n",
	})
	info := &ManifestInfo{Ecosystem: "go", Path: "go.mod", Dependencies: map[string]string{}}
	cmds := buildCommands(tree, ".", info)

	byPurpose := map[domain.CommandPurpose]domain.DetectedCommand{}
	for _, c := range cmds {
		byPurpose[c.Purpose] = c
	}
	require.Equal(t, "make test", byPurpose[domain.CommandTest].Command)
	require.Equal(t, "go vet ./...", byPurpose[domain.CommandLint].Command, "lint has no Makefile target, so the Go default fills it")
}

func TestNodeScriptCommandsRenderPerPackageManager(t *testing.T) {
	tree := treeFrom(t, fileset{"pnpm-lock.yaml": "lockfileVersion: '6.0'\n"})
	info := &ManifestInfo{
		Ecosystem: "node", Path: "package.json",
		Scripts:      map[string]string{"build": "tsc -b", "test": "vitest run", "dev": "vite"},
		Dependencies: map[string]string{},
	}
	cmds := buildCommands(tree, ".", info)
	byPurpose := map[domain.CommandPurpose]domain.DetectedCommand{}
	for _, c := range cmds {
		byPurpose[c.Purpose] = c
	}
	require.Equal(t, "pnpm build", byPurpose[domain.CommandBuild].Command)
	require.Equal(t, "pnpm test", byPurpose[domain.CommandTest].Command)
	require.Equal(t, "pnpm dev", byPurpose[domain.CommandDev].Command)
	require.Equal(t, "pnpm install --frozen-lockfile", byPurpose[domain.CommandInstall].Command)
}

func TestDetectDevPort(t *testing.T) {
	t.Run("vite.config.ts wins", func(t *testing.T) {
		tree := treeFrom(t, fileset{"vite.config.ts": "export default { server: { port: 4173 } }\n"})
		info := &ManifestInfo{Ecosystem: "node", Path: "package.json", Dependencies: map[string]string{"vite": "^5.0.0"}}
		require.Equal(t, 4173, detectDevPort(tree, ".", info, nil))
	})
	t.Run("next.js defaults to 3000 when nothing else states a port", func(t *testing.T) {
		tree := treeFrom(t, fileset{})
		info := &ManifestInfo{Ecosystem: "node", Path: "package.json", Dependencies: map[string]string{"next": "^14.0.0"}}
		require.Equal(t, 3000, detectDevPort(tree, ".", info, nil))
	})
	t.Run("a Dockerfile EXPOSE is used when nothing else states a port", func(t *testing.T) {
		tree := treeFrom(t, fileset{"Dockerfile": "FROM golang:1.26\nEXPOSE 9090\n"})
		info := &ManifestInfo{Ecosystem: "go", Path: "go.mod", Dependencies: map[string]string{}}
		require.Equal(t, 9090, detectDevPort(tree, ".", info, nil))
	})
}
