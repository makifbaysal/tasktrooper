package repofacts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func seed(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCommandsComeFromDeclarations(t *testing.T) {
	root := seed(t, map[string]string{
		"package.json":   `{"name":"web","packageManager":"pnpm@9.0.0","scripts":{"build":"vite build","test":"vitest run","dev":"vite","postinstall":"patch-package"},"dependencies":{"react":"^19.0.0","vite":"^6.0.0"}}`,
		"pnpm-lock.yaml": "lockfileVersion: '9.0'\n",
		"src/main.tsx":   "export {}\n",
	})
	f := Collect(context.Background(), root)

	var build, test, run string
	for _, c := range f.Commands {
		switch c.Purpose {
		case "build":
			build = c.Cmd
		case "test":
			test = c.Cmd
		case "run":
			run = c.Cmd
		}
		if c.Source == "" {
			t.Errorf("command %q has no source file", c.Cmd)
		}
	}
	if build != "pnpm build" {
		t.Errorf("build command = %q, want the lockfile's package manager", build)
	}
	if test != "pnpm test" {
		t.Errorf("test command = %q", test)
	}
	if run == "" {
		t.Error("dev script must be reported as a run command")
	}
	for _, c := range f.Commands {
		if strings.Contains(c.Cmd, "postinstall") {
			t.Error("only meaningful scripts belong in the profile")
		}
	}
	if len(f.Manifests) != 1 || f.Manifests[0].Manager != "pnpm" {
		t.Fatalf("manifest = %+v, want one npm manifest resolved to pnpm", f.Manifests)
	}
}

func TestDeployFromWorkflow(t *testing.T) {
	root := seed(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.26\n",
		".github/workflows/deploy.yml": `name: Deploy
on:
  push:
    branches: [main]
  workflow_dispatch:
jobs:
  deploy-prod:
    steps:
      - run: kubectl apply -f deploy/k8s
`,
		".github/workflows/ci.yml": `name: CI
on: [pull_request]
jobs:
  test:
    steps:
      - run: go test ./...
`,
	})
	f := Collect(context.Background(), root)

	if len(f.Workflows) != 2 {
		t.Fatalf("workflows = %d, want 2", len(f.Workflows))
	}
	var deployWF, ciWF Workflow
	for _, w := range f.Workflows {
		if strings.HasSuffix(w.File, "deploy.yml") {
			deployWF = w
		} else {
			ciWF = w
		}
	}
	if !deployWF.Deploys {
		t.Error("a kubectl apply step must mark the workflow as deploying")
	}
	if ciWF.Deploys {
		t.Error("a test-only workflow must not be marked as deploying")
	}
	if !contains(deployWF.Triggers, "push:main") {
		t.Errorf("triggers = %v, want the branch filter attached", deployWF.Triggers)
	}

	var automatic bool
	for _, d := range f.Deploys {
		if d.Automatic && strings.HasPrefix(d.Trigger, "push:main") {
			automatic = true
			if d.Evidence != ".github/workflows/deploy.yml" {
				t.Errorf("deploy evidence = %q", d.Evidence)
			}
		}
	}
	if !automatic {
		t.Fatalf("landing on main must be reported as an automatic deploy; got %+v", f.Deploys)
	}
}

func TestHostingIntegrationWithoutWorkflowIsTheDeploy(t *testing.T) {
	root := seed(t, map[string]string{
		"vercel.json":  `{"framework":"nextjs"}`,
		"package.json": `{"name":"site","scripts":{"build":"next build"},"dependencies":{"next":"15.0.0"}}`,
	})
	f := Collect(context.Background(), root)

	var vercel bool
	for _, in := range f.Integrations {
		if in.Name == "Vercel" && in.Category == "hosting" {
			vercel = true
		}
	}
	if !vercel {
		t.Fatalf("vercel.json must be detected as hosting; integrations = %+v", f.Integrations)
	}
	var prod, preview bool
	for _, d := range f.Deploys {
		if d.Provider != "Vercel" || !d.Automatic {
			continue
		}
		switch d.Environment {
		case "production":
			prod = true
			if !strings.Contains(d.Trigger, "no deploy workflow") {
				t.Errorf("production trigger should say why it is the provider's own hook: %q", d.Trigger)
			}
		case "preview":
			preview = true
		}
	}
	if !prod || !preview {
		t.Fatalf("a linked host with no workflow must yield prod+preview deploys; got %+v", f.Deploys)
	}
}

func TestWorkflowBeatsProviderIntegration(t *testing.T) {
	root := seed(t, map[string]string{
		"vercel.json": `{}`,
		".github/workflows/deploy.yml": `name: Deploy
on:
  push:
    branches: [main]
jobs:
  ship:
    steps:
      - uses: amondnet/vercel-action@v25
`,
	})
	f := Collect(context.Background(), root)
	for _, d := range f.Deploys {
		if d.Provider == "Vercel" {
			t.Fatalf("provider integration must not be reported alongside a deploy workflow: %+v", f.Deploys)
		}
	}
	if len(f.Deploys) == 0 {
		t.Fatal("the deploy workflow must still be reported")
	}
}

func TestMonorepoKindInference(t *testing.T) {
	files := map[string]string{
		"apps/backend/go.mod":   "module demo\n\ngo 1.26\n",
		"apps/web/package.json": `{"name":"web","dependencies":{"react":"^19.0.0"}}`,
	}
	for i := 0; i < 8; i++ {
		files["apps/backend/internal/svc"+string(rune('a'+i))+".go"] = "package svc\n"
		files["apps/web/src/comp"+string(rune('a'+i))+".tsx"] = "export {}\n"
	}
	f := Collect(context.Background(), seed(t, files))

	if f.Kind != domain.RepoKindMonorepo {
		t.Fatalf("kind = %q, want monorepo (evidence: %v)", f.Kind, f.KindEvidence)
	}
	if !contains(f.SubKinds, domain.RepoKindBackend) || !contains(f.SubKinds, domain.RepoKindFrontend) {
		t.Fatalf("sub kinds = %v, want backend and frontend", f.SubKinds)
	}
	if len(f.KindEvidence) == 0 {
		t.Error("a classification with no evidence is an assertion")
	}
}

func TestSingleKindInference(t *testing.T) {
	files := map[string]string{"tsconfig.json": "{}", "Podfile": "platform :ios\n"}
	for i := 0; i < 25; i++ {
		files["App/View"+string(rune('a'+i))+".swift"] = "import SwiftUI\n"
	}
	f := Collect(context.Background(), seed(t, files))
	if f.Kind != domain.RepoKindMobile {
		t.Fatalf("kind = %q, want mobile", f.Kind)
	}
}

func TestMissingWorkingCopyDegrades(t *testing.T) {
	f := Collect(context.Background(), filepath.Join(t.TempDir(), "nope"))
	if f.HasAny() {
		t.Fatal("a missing working copy must produce no facts")
	}
	if len(f.Warnings) == 0 {
		t.Fatal("a missing working copy must be reported as a warning")
	}
}
