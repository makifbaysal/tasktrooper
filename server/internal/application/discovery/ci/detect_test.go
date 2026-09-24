package ci

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func treeFromFiles(files map[string]string) *inventory.Tree {
	fsys := fstest.MapFS{}
	for p, body := range files {
		fsys[p] = &fstest.MapFile{Data: []byte(body)}
	}
	return inventory.FromFS(fsys)
}

func checkFor(t *testing.T, checks []domain.DetectedCheck, jobKey string) domain.DetectedCheck {
	t.Helper()
	for _, c := range checks {
		if c.JobKey == jobKey {
			return c
		}
	}
	t.Fatalf("no check for job %q in %+v", jobKey, checks)
	return domain.DetectedCheck{}
}

func checksFor(checks []domain.DetectedCheck, jobKey string) []domain.DetectedCheck {
	var out []domain.DetectedCheck
	for _, c := range checks {
		if c.JobKey == jobKey {
			out = append(out, c)
		}
	}
	return out
}

func cmd(dir string, argv ...string) domain.LocalCommand {
	return domain.LocalCommand{Dir: dir, Argv: argv}
}

// This repository's own .github/workflows/ci.yml, copied verbatim.
const realCI = `name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  server:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: server/go.mod
          cache-dependency-path: server/go.sum
      - run: sudo apt-get install -y ripgrep
      - run: cd server && go build ./... && go vet ./...
      - run: cd server && go test ./...

  ui:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: desktop/ui/package-lock.json
      - run: cd desktop/ui && npm ci
      - run: npm --prefix desktop/ui run typecheck
      - run: cd desktop/ui && npm run build

  desktop:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: desktop/package-lock.json
      - run: cd desktop && npm ci
      - run: cd desktop && npm run typecheck
      - run: cd desktop && npm run lint
      - run: cd desktop && npm test
`

func TestDetect_RealCI(t *testing.T) {
	tree := treeFromFiles(map[string]string{".github/workflows/ci.yml": realCI})
	components := []domain.DetectedComponent{
		{Path: "server"}, {Path: "desktop"}, {Path: "desktop/ui"},
	}
	res := Detect(tree, components)
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}

	server := checkFor(t, res.Checks, "server")
	if server.ComponentPath != "server" {
		t.Errorf("server ComponentPath = %q, want server", server.ComponentPath)
	}
	if server.Purpose != domain.CheckTest {
		t.Errorf("server Purpose = %q, want test", server.Purpose)
	}
	wantServer := []domain.LocalCommand{
		cmd("server", "go", "build", "./..."),
		cmd("server", "go", "vet", "./..."),
		cmd("server", "go", "test", "./..."),
	}
	if !reflect.DeepEqual(server.LocalCommands, wantServer) {
		t.Errorf("server LocalCommands = %+v, want %+v", server.LocalCommands, wantServer)
	}

	ui := checkFor(t, res.Checks, "ui")
	if ui.ComponentPath != "desktop/ui" {
		t.Errorf("ui ComponentPath = %q, want desktop/ui", ui.ComponentPath)
	}
	if ui.Purpose != domain.CheckTypecheck {
		t.Errorf("ui Purpose = %q, want typecheck", ui.Purpose)
	}
	wantUI := []domain.LocalCommand{
		cmd("desktop/ui", "npm", "run", "typecheck"),
		cmd("desktop/ui", "npm", "run", "build"),
	}
	if !reflect.DeepEqual(ui.LocalCommands, wantUI) {
		t.Errorf("ui LocalCommands = %+v, want %+v", ui.LocalCommands, wantUI)
	}

	desktop := checkFor(t, res.Checks, "desktop")
	if desktop.ComponentPath != "desktop" {
		t.Errorf("desktop ComponentPath = %q, want desktop", desktop.ComponentPath)
	}
	if desktop.Purpose != domain.CheckTest {
		t.Errorf("desktop Purpose = %q, want test", desktop.Purpose)
	}
	wantDesktop := []domain.LocalCommand{
		cmd("desktop", "npm", "run", "typecheck"),
		cmd("desktop", "npm", "run", "lint"),
		cmd("desktop", "npm", "test"),
	}
	if !reflect.DeepEqual(desktop.LocalCommands, wantDesktop) {
		t.Errorf("desktop LocalCommands = %+v, want %+v", desktop.LocalCommands, wantDesktop)
	}
}

// This repository's own .github/workflows/release.yml, with comment lines stripped
// (Go raw strings cannot contain a backtick, and the original comments and one
// echo message use one) — every functional line is unchanged.
const realRelease = `name: release

on:
  push:
    tags: ["v*"]
  workflow_dispatch:

permissions:
  contents: write

concurrency:
  group: release-${{ github.ref }}
  cancel-in-progress: false

jobs:
  release:
    if: github.ref_type == 'tag'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Create the release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          set -euo pipefail
          gh release view "$GITHUB_REF_NAME" > /dev/null 2>&1 \
            || gh release create "$GITHUB_REF_NAME" --title "TaskTrooper ${GITHUB_REF_NAME#v}" --generate-notes

  mac:
    needs: release
    if: ${{ !cancelled() && (needs.release.result == 'success' || needs.release.result == 'skipped') }}
    runs-on: macos-14
    timeout-minutes: 90
    environment: ${{ github.ref_type == 'tag' && 'Release' || '' }}
    defaults:
      run:
        shell: bash
        working-directory: desktop
    env:
      CGO_ENABLED: "1"
      PUBLISH: ${{ github.ref_type == 'tag' && 'always' || 'never' }}

    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: server/go.mod
          cache-dependency-path: server/go.sum
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: |
            desktop/package-lock.json
            desktop/ui/package-lock.json

      - name: Set the version from the tag
        if: github.ref_type == 'tag'
        run: npm version --no-git-tag-version --allow-same-version "${GITHUB_REF_NAME#v}"

      - name: Install and build
        run: |
          npm ci
          npm --prefix ui ci
          npm run build
          npm run build:ui
          npm run build:server:universal
          npm run build:embedder

      - name: Work out the signing material
        id: sign
        working-directory: .
        env:
          APPLE_API_KEY_B64: ${{ secrets.APPLE_API_KEY }}
          CSC_LINK: ${{ secrets.CSC_LINK }}
        run: |
          set -euo pipefail
          if [ -n "${APPLE_API_KEY_B64:-}" ]; then
            install -d -m 700 "$RUNNER_TEMP/asc"
            printf '%s' "$APPLE_API_KEY_B64" | base64 --decode > "$RUNNER_TEMP/asc/AuthKey.p8"
            chmod 600 "$RUNNER_TEMP/asc/AuthKey.p8"
            grep -q "BEGIN PRIVATE KEY" "$RUNNER_TEMP/asc/AuthKey.p8" \
              || { echo "::error title=APPLE_API_KEY is not a base64 .p8::Re-run: base64 -i AuthKey_XXXX.p8 | pbcopy"; exit 1; }
            echo "apikey=$RUNNER_TEMP/asc/AuthKey.p8" >> "$GITHUB_OUTPUT"
          fi
          if [ -n "${CSC_LINK:-}" ]; then
            printf "args=\nsigned=true\n" >> "$GITHUB_OUTPUT"
            echo "Signed with the Developer ID certificate in CSC_LINK." >> "$GITHUB_STEP_SUMMARY"
          else
            printf "args=-c.mac.identity=-\nsigned=false\n" >> "$GITHUB_OUTPUT"
            echo "**Ad-hoc signed** (no CSC_LINK secret), so not notarized: the first launch needs right-click → Open, or 'xattr -dr com.apple.quarantine'." >> "$GITHUB_STEP_SUMMARY"
          fi

      - name: Package
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          CSC_LINK: ${{ secrets.CSC_LINK }}
          CSC_KEY_PASSWORD: ${{ secrets.CSC_KEY_PASSWORD }}
          APPLE_API_KEY: ${{ steps.sign.outputs.apikey }}
          APPLE_API_KEY_ID: ${{ secrets.APPLE_API_KEY_ID }}
          APPLE_API_ISSUER: ${{ secrets.APPLE_API_ISSUER }}
          IDENTITY: ${{ steps.sign.outputs.args }}
        run: |
          for name in CSC_LINK CSC_KEY_PASSWORD APPLE_API_KEY APPLE_API_KEY_ID APPLE_API_ISSUER; do
            [ -n "${!name:-}" ] || unset "$name"
          done
          npx electron-builder --mac --publish "$PUBLISH" \
            -c.publish.provider=github -c.publish.owner=makifbaysal -c.publish.repo=tasktrooper \
            -c.publish.releaseType=release ${IDENTITY:+"$IDENTITY"}

      - name: Verify the artifact
        run: |
          set -euo pipefail
          app="release/mac-universal/TaskTrooper.app"
          [ -d "$app" ] || { echo "::error::$app was not produced"; exit 1; }
          codesign --verify --deep --strict --verbose=2 "$app"
          for f in Contents/Resources/bin/agent-server Contents/Resources/web/index.html Contents/Resources/embedder/index.cjs; do
            [ -e "$app/$f" ] || { echo "::error title=Missing from the bundle::$f"; exit 1; }
          done
          lipo -archs "$app/Contents/Resources/bin/agent-server"
          if [ "${{ steps.sign.outputs.signed }}" = "true" ] && [ -n "${{ steps.sign.outputs.apikey }}" ]; then
            xcrun stapler validate "$app"
            spctl --assess -t exec -vv "$app"
          fi
          echo "macOS: $(find release -maxdepth 1 \( -name '*.dmg' -o -name '*.zip' \) -print | sed 's#.*/##' | tr '\n' ' ')" >> "$GITHUB_STEP_SUMMARY"

      - uses: actions/upload-artifact@v4
        if: github.ref_type != 'tag'
        with:
          name: macos
          path: |
            desktop/release/*.dmg
            desktop/release/*.zip

  windows:
    needs: release
    if: ${{ !cancelled() && (needs.release.result == 'success' || needs.release.result == 'skipped') }}
    runs-on: windows-2022
    timeout-minutes: 90
    defaults:
      run:
        shell: bash
        working-directory: desktop
    env:
      CGO_ENABLED: "1"
      PUBLISH: ${{ github.ref_type == 'tag' && 'always' || 'never' }}

    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: server/go.mod
          cache-dependency-path: server/go.sum
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: |
            desktop/package-lock.json
            desktop/ui/package-lock.json

      - uses: msys2/setup-msys2@v2
        id: msys2
        with:
          msystem: UCRT64
          update: false
          install: mingw-w64-ucrt-x86_64-gcc
      - name: Put gcc on PATH
        shell: pwsh
        working-directory: .
        run: '"${{ steps.msys2.outputs.msys2-location }}\ucrt64\bin" | Out-File -FilePath $env:GITHUB_PATH -Encoding utf8 -Append'

      - name: Set the version from the tag
        if: github.ref_type == 'tag'
        run: npm version --no-git-tag-version --allow-same-version "${GITHUB_REF_NAME#v}"

      - name: Install and build
        run: |
          npm ci
          npm --prefix ui ci
          npm run build
          npm run build:ui
          npm run build:server
          npm run build:embedder

      - name: Package
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          npx electron-builder --win --publish "$PUBLISH" \
            -c.publish.provider=github -c.publish.owner=makifbaysal -c.publish.repo=tasktrooper \
            -c.publish.releaseType=release

      - name: Verify the artifact
        run: |
          set -euo pipefail
          app="release/win-unpacked/resources"
          for f in bin/agent-server.exe web/index.html embedder/index.cjs; do
            [ -e "$app/$f" ] || { echo "::error title=Missing from the bundle::$f"; exit 1; }
          done
          ls release/*-setup.exe
          echo "Windows (unsigned): $(find release -maxdepth 1 -name '*-setup.exe' -print | sed 's#.*/##')" >> "$GITHUB_STEP_SUMMARY"

      - uses: actions/upload-artifact@v4
        if: github.ref_type != 'tag'
        with:
          name: windows
          path: desktop/release/*-setup.exe

  linux:
    needs: release
    if: ${{ !cancelled() && (needs.release.result == 'success' || needs.release.result == 'skipped') }}
    runs-on: ubuntu-22.04
    timeout-minutes: 90
    defaults:
      run:
        shell: bash
        working-directory: desktop
    env:
      CGO_ENABLED: "1"
      PUBLISH: ${{ github.ref_type == 'tag' && 'always' || 'never' }}

    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: server/go.mod
          cache-dependency-path: server/go.sum
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
          cache-dependency-path: |
            desktop/package-lock.json
            desktop/ui/package-lock.json

      - name: Set the version from the tag
        if: github.ref_type == 'tag'
        run: npm version --no-git-tag-version --allow-same-version "${GITHUB_REF_NAME#v}"

      - name: Install and build
        run: |
          npm ci
          npm --prefix ui ci
          npm run build
          npm run build:ui
          npm run build:server
          npm run build:embedder

      - name: Package
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          npx electron-builder --linux --publish "$PUBLISH" \
            -c.publish.provider=github -c.publish.owner=makifbaysal -c.publish.repo=tasktrooper \
            -c.publish.releaseType=release

      - name: Verify the artifact
        run: |
          set -euo pipefail
          app="release/linux-unpacked/resources"
          for f in bin/agent-server web/index.html embedder/index.cjs; do
            [ -e "$app/$f" ] || { echo "::error title=Missing from the bundle::$f"; exit 1; }
          done
          file "$app/bin/agent-server" | grep -q "ELF 64-bit"
          ls release/*.AppImage release/*.deb
          echo "Linux: $(find release -maxdepth 1 \( -name '*.AppImage' -o -name '*.deb' \) -print | sed 's#.*/##' | tr '\n' ' ')" >> "$GITHUB_STEP_SUMMARY"

      - uses: actions/upload-artifact@v4
        if: github.ref_type != 'tag'
        with:
          name: linux
          path: |
            desktop/release/*.AppImage
            desktop/release/*.deb
`

func TestDetect_RealRelease(t *testing.T) {
	tree := treeFromFiles(map[string]string{".github/workflows/release.yml": realRelease})
	components := []domain.DetectedComponent{{Path: "desktop"}, {Path: "server"}}
	res := Detect(tree, components)
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	if len(res.Checks) == 0 {
		t.Fatal("expected checks")
	}

	sawPushTags, sawDispatch := false, false
	for _, c := range res.Checks {
		if c.Purpose != domain.CheckRelease {
			t.Errorf("job %s/%s Purpose = %q, want release", c.JobKey, c.ComponentPath, c.Purpose)
		}
		if len(c.LocalCommands) != 0 {
			t.Errorf("job %s/%s LocalCommands = %+v, want none", c.JobKey, c.ComponentPath, c.LocalCommands)
		}
		if !c.Dispatchable {
			t.Errorf("job %s/%s Dispatchable = false, want true", c.JobKey, c.ComponentPath)
		}
		for _, tr := range c.Triggers {
			if tr == "push:tags" {
				sawPushTags = true
			}
			if tr == "workflow_dispatch" {
				sawDispatch = true
			}
		}
	}
	if !sawPushTags {
		t.Errorf("Triggers missing push:tags, got %v", res.Checks[0].Triggers)
	}
	if !sawDispatch {
		t.Errorf("Triggers missing workflow_dispatch, got %v", res.Checks[0].Triggers)
	}

	mac := checkFor(t, res.Checks, "mac")
	if mac.ComponentPath != "desktop" {
		t.Errorf("mac ComponentPath = %q, want desktop", mac.ComponentPath)
	}
}

func TestDetect_PnpmTurboMonorepo(t *testing.T) {
	const wf = `name: ci
on: push
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - run: pnpm install
      - run: pnpm --filter web lint

  test:
    runs-on: ubuntu-latest
    steps:
      - run: pnpm install
      - run: pnpm test
`
	tree := treeFromFiles(map[string]string{".github/workflows/ci.yml": wf})
	components := []domain.DetectedComponent{
		{Path: "apps/web", PackageName: "web"},
		{Path: "apps/api", PackageName: "api"},
	}
	res := Detect(tree, components)
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}

	lint := checkFor(t, res.Checks, "lint")
	if lint.ComponentPath != "apps/web" {
		t.Errorf("lint ComponentPath = %q, want apps/web", lint.ComponentPath)
	}
	if lint.Confidence != domain.ConfidenceHigh {
		t.Errorf("lint Confidence = %q, want high", lint.Confidence)
	}
	if lint.Purpose != domain.CheckLint {
		t.Errorf("lint Purpose = %q, want lint", lint.Purpose)
	}

	testChecks := checksFor(res.Checks, "test")
	if len(testChecks) != 2 {
		t.Fatalf("test job produced %d checks, want 2 (all components): %+v", len(testChecks), testChecks)
	}
	for _, c := range testChecks {
		if c.Confidence != domain.ConfidenceMedium {
			t.Errorf("test/%s Confidence = %q, want medium", c.ComponentPath, c.Confidence)
		}
		want := []domain.LocalCommand{cmd(".", "pnpm", "test")}
		if !reflect.DeepEqual(c.LocalCommands, want) {
			t.Errorf("test/%s LocalCommands = %+v, want %+v", c.ComponentPath, c.LocalCommands, want)
		}
	}
}

func TestDetect_DeployJob(t *testing.T) {
	const wf = `name: deploy
on:
  push:
    branches: [main]
jobs:
  deploy-api:
    runs-on: ubuntu-latest
    environment: production
    defaults:
      run:
        working-directory: services/api
    steps:
      - uses: actions/checkout@v4
      - uses: google-github-actions/deploy-cloudrun@v2
        with:
          service: api
          region: us-central1
`
	tree := treeFromFiles(map[string]string{".github/workflows/deploy.yml": wf})
	components := []domain.DetectedComponent{{Path: "services/api"}}
	res := Detect(tree, components)
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	check := checkFor(t, res.Checks, "deploy-api")
	if check.Purpose != domain.CheckDeploy {
		t.Errorf("Purpose = %q, want deploy", check.Purpose)
	}
	if check.Environment != domain.EnvironmentProduction {
		t.Errorf("Environment = %q, want production", check.Environment)
	}
	if check.ComponentPath != "services/api" {
		t.Errorf("ComponentPath = %q, want services/api", check.ComponentPath)
	}
	if len(check.LocalCommands) != 0 {
		t.Errorf("LocalCommands = %+v, want none", check.LocalCommands)
	}
}

func TestDetect_PathFilterMapping(t *testing.T) {
	const wf = `name: worker-ci
on:
  push:
    paths: ['services/worker/**']
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
`
	tree := treeFromFiles(map[string]string{".github/workflows/ci.yml": wf})
	components := []domain.DetectedComponent{{Path: "services/worker"}}
	res := Detect(tree, components)
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	check := checkFor(t, res.Checks, "build")
	if check.ComponentPath != "services/worker" {
		t.Errorf("ComponentPath = %q, want services/worker", check.ComponentPath)
	}
	if check.Confidence != domain.ConfidenceHigh {
		t.Errorf("Confidence = %q, want high", check.Confidence)
	}
	if len(check.PathFilters) != 1 || check.PathFilters[0] != "services/worker/**" {
		t.Errorf("PathFilters = %v, want [services/worker/**]", check.PathFilters)
	}
}

func TestDetect_NonRunnableSegmentsSkipped(t *testing.T) {
	const wf = `name: ci
on: push
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: |
          go test ./... | tee out.log
          echo $GITHUB_SHA > sha.txt
          VAR=1 npm run build
          go build ./... > build.log
`
	tree := treeFromFiles(map[string]string{".github/workflows/ci.yml": wf})
	components := []domain.DetectedComponent{{Path: "."}}
	res := Detect(tree, components)
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	check := checkFor(t, res.Checks, "verify")
	if len(check.LocalCommands) != 0 {
		t.Errorf("LocalCommands = %+v, want none (all segments non-runnable)", check.LocalCommands)
	}
}

func TestDetect_UnparsableYAMLWarns(t *testing.T) {
	const broken = `name: "unterminated
on: push
`
	tree := treeFromFiles(map[string]string{".github/workflows/broken.yml": broken})
	res := Detect(tree, []domain.DetectedComponent{{Path: "."}})
	if len(res.Warnings) == 0 {
		t.Fatal("expected a warning for unparsable YAML")
	}
	if len(res.Checks) != 0 {
		t.Errorf("expected no checks from an unparsable file, got %+v", res.Checks)
	}
}

func TestDetect_ReleaseJobsPinnedToOneComponent(t *testing.T) {
	fsys := fstest.MapFS{
		".github/workflows/release.yml": &fstest.MapFile{Data: []byte(`name: release
on:
  push:
    tags: ["v*"]
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - run: gh release create "$GITHUB_REF_NAME"
  mac:
    needs: release
    runs-on: macos-14
    defaults:
      run:
        shell: bash
        working-directory: desktop
    steps:
      - run: cd ../server && go build ./cmd/agent-server
      - run: npm --prefix ui run build
      - run: npx electron-builder --mac --publish always
`)},
	}
	components := []domain.DetectedComponent{{Path: "desktop"}, {Path: "desktop/ui"}, {Path: "server"}}
	res := Detect(inventory.FromFS(fsys), components)

	if len(res.Checks) != 2 {
		t.Fatalf("want one check per release job, got %d: %+v", len(res.Checks), res.Checks)
	}
	for _, c := range res.Checks {
		if c.ComponentPath != "desktop" {
			t.Errorf("job %s mapped to %q, want desktop", c.JobKey, c.ComponentPath)
		}
	}
}
