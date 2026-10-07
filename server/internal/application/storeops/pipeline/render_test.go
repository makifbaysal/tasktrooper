package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func iosSpec() Spec {
	return Spec{
		Platform:   domain.MobileStorePlatformIOS,
		Identifier: "com.example.myapp",
		AppName:    "MyApp",
		StoreAppID: "6470000001",
		Scheme:     "MyApp",
	}
}

func androidSpec() Spec {
	return Spec{
		Platform:   domain.MobileStorePlatformAndroid,
		Identifier: "com.example.myapp",
		AppName:    "MyApp",
		Module:     "app",
	}
}

func render(t *testing.T, spec Spec) (script, workflow Artifact) {
	t.Helper()
	arts, err := Render(spec)
	require.NoError(t, err)
	require.Len(t, arts, 2)
	return arts[0], arts[1]
}

func codeOnly(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func funcBody(t *testing.T, script, name string) string {
	t.Helper()
	start := strings.Index(script, name+"() {")
	require.GreaterOrEqual(t, start, 0, "%s is not defined in the generated script", name)
	rest := script[start:]
	end := strings.Index(rest, "\n}\n")
	require.GreaterOrEqual(t, end, 0, "%s has no closing brace", name)
	return rest[:end]
}

func TestRenderReturnsTheScriptBeforeTheWorkflow(t *testing.T) {
	for _, spec := range []Spec{iosSpec(), androidSpec()} {
		script, workflow := render(t, spec)

		assert.Equal(t, "scripts/mobile-release.sh", script.Path)
		assert.Equal(t, ".github/workflows/mobile-release.yml", workflow.Path)

		assert.Equal(t, uint32(0o755), script.Mode)
		assert.Equal(t, uint32(0o644), workflow.Mode)
		assert.Contains(t, workflow.Body, script.Path,
			"the wrapper has to name the script it calls")
	}
}

func TestRenderScopesASubProject(t *testing.T) {
	spec := androidSpec()
	spec.SubProjectPath = "apps/mobile"
	script, workflow := render(t, spec)

	assert.Equal(t, "apps/mobile/scripts/mobile-release.sh", script.Path)
	assert.Equal(t, ".github/workflows/mobile-release-apps-mobile.yml", workflow.Path)
	assert.Contains(t, workflow.Body, "bash apps/mobile/scripts/mobile-release.sh")
	assert.Contains(t, workflow.Body, "path: apps/mobile/build/mobile-release/**")
	assert.Contains(t, workflow.Body, `git checkout "$GITHUB_SHA" -- apps/mobile/scripts/mobile-release.sh`,
		"the restored procedure is the sub-project's own script")
}

func TestGeneratedScriptParses(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			script, _ := render(t, spec)
			path := filepath.Join(t.TempDir(), "mobile-release.sh")
			require.NoError(t, os.WriteFile(path, []byte(script.Body), 0o755))

			out, err := exec.Command(bash, "-n", path).CombinedOutput()
			require.NoError(t, err, "bash -n: %s", out)
		})
	}
}

func TestGeneratedScriptRefusesZsh(t *testing.T) {
	for _, spec := range []Spec{iosSpec(), androidSpec()} {
		script, _ := render(t, spec)
		assert.True(t, strings.HasPrefix(script.Body, "#!/usr/bin/env bash\n"))
		assert.Contains(t, script.Body, "set -euo pipefail")
		assert.Contains(t, script.Body, `if [ -z "${BASH_VERSION:-}" ]; then`)
	}
}

func TestEveryBase64SecretIsDecodedInTheScript(t *testing.T) {
	cases := map[string]struct {
		spec   Spec
		base64 []string
		plain  []string
	}{
		"ios": {
			spec:   iosSpec(),
			base64: []string{"IOS_DIST_CERT_P12", "IOS_PROFILE_B64", "ASC_KEY_P8"},
			plain:  []string{"IOS_CERT_PASSWORD", "ASC_KEY_ID", "ASC_ISSUER_ID"},
		},
		"android": {
			spec:   androidSpec(),
			base64: []string{"ANDROID_UPLOAD_KEYSTORE_B64", "PLAY_SA_JSON"},
			plain: []string{
				"ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			script, workflow := render(t, tc.spec)
			for _, secret := range tc.base64 {
				assert.Contains(t, script.Body, "decode_secret "+secret,
					"%s arrives base64-encoded and every consumer wants raw bytes", secret)
			}
			for _, secret := range append(tc.base64, tc.plain...) {
				assert.Contains(t, workflow.Body, secret+": ${{ secrets."+secret+" }}",
					"the wrapper's only job with a secret is to put it in the environment")
			}
			assert.NotContains(t, codeOnly(workflow.Body), "base64",
				"decoding is a step, and every step lives in the script")
		})
	}
}

func TestDecodedKeysAreMaskedInTheScriptAndOnlyUnderActions(t *testing.T) {
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			script, workflow := render(t, spec)
			assert.Contains(t, script.Body, `echo "::add-mask::$line"`)
			assert.Contains(t, script.Body, `if [ -z "${GITHUB_ACTIONS:-}" ]; then`)
			assert.NotContains(t, codeOnly(workflow.Body), "add-mask")
		})
	}
	iosScript, _ := render(t, iosSpec())
	assert.Contains(t, funcBody(t, iosScript.Body, "asc_key"), `mask_lines < "$ASC_KEY_PATH"`,
		"the ASC key is the one the templates called out by name")
}

func TestProdDoesNotRebuild(t *testing.T) {
	t.Run("ios", func(t *testing.T) {
		script, _ := render(t, iosSpec())
		prod := codeOnly(funcBody(t, script.Body, "release_prod"))
		assert.NotContains(t, prod, "archive")
		assert.NotContains(t, prod, "altool")
		assert.NotContains(t, prod, "resolve_build_number")
		assert.Contains(t, prod, "--skip_binary_upload true")
		assert.Contains(t, prod, "--submit_for_review")

		stage := funcBody(t, script.Body, "release_stage")
		assert.Contains(t, stage, "xcodebuild -scheme")
		assert.Contains(t, stage, "archive")
		assert.Contains(t, stage, "-exportArchive")
	})

	t.Run("android", func(t *testing.T) {
		script, _ := render(t, androidSpec())
		prod := codeOnly(funcBody(t, script.Body, "release_prod"))
		assert.NotContains(t, prod, "bundleRelease")
		assert.NotContains(t, prod, "gradlew")
		assert.NotContains(t, prod, "resolve_build_number")
		assert.Contains(t, prod, "track_promote_to:production")

		stage := funcBody(t, script.Body, "release_stage")
		assert.Contains(t, stage, `":$MODULE:bundleRelease"`)
	})
}

func TestScriptCarriesTheWholeProcedure(t *testing.T) {
	t.Run("ios", func(t *testing.T) {
		script, _ := render(t, iosSpec())
		stage := funcBody(t, script.Body, "release_stage")
		for _, want := range []string{

			`sec "create-keychain`,
			`sec "import`,

			`sec "set-key-partition-list`,
			"Provisioning Profiles",
			"CODE_SIGN_STYLE=Manual",
			`PRODUCT_BUNDLE_IDENTIFIER="$IDENTIFIER"`,
			`CURRENT_PROJECT_VERSION="$BUILD_NUMBER"`,
			"app-store-connect",
			"xcrun altool --upload-app",
		} {
			assert.Contains(t, stage, want)
		}
	})

	t.Run("android", func(t *testing.T) {
		script, _ := render(t, androidSpec())
		stage := funcBody(t, script.Body, "release_stage")
		for _, want := range []string{
			"decode_secret ANDROID_UPLOAD_KEYSTORE_B64",

			`signing_properties "$keystore"`,
			`-g "$GRADLE_RUN_HOME"`,
			`-PversionCode="$BUILD_NUMBER"`,
			`-Pandroid.injected.version.code="$BUILD_NUMBER"`,
			"track:internal",
		} {
			assert.Contains(t, stage, want)
		}
	})
}

func TestSigningValuesNeverReachArgv(t *testing.T) {
	t.Run("android", func(t *testing.T) {
		script, _ := render(t, androidSpec())
		for _, forbidden := range []string{
			"-Pandroid.injected.signing.store.password=",
			"-Pandroid.injected.signing.key.password=",
			"-Pandroid.injected.signing.key.alias=",
		} {
			assert.NotContains(t, script.Body, forbidden)
		}
	})

	t.Run("ios", func(t *testing.T) {
		script, _ := render(t, iosSpec())
		for _, forbidden := range []string{
			`security import "$WORKDIR/dist.p12"`,
			`security create-keychain -p`,
			`security set-key-partition-list`,
		} {
			assert.NotContains(t, script.Body, forbidden)
		}
	})
}

func TestPlayFirstUploadStopsCleanlyWithTheArtifact(t *testing.T) {
	script, workflow := render(t, androidSpec())
	stage := funcBody(t, script.Body, "release_stage")

	copyAt := strings.Index(stage, `cp "$built" "$AAB"`)
	uploadAt := strings.Index(stage, "fastlane run supply")
	require.GreaterOrEqual(t, copyAt, 0)
	require.GreaterOrEqual(t, uploadAt, 0)
	assert.Less(t, copyAt, uploadAt, "a refused upload must still leave a signed bundle")

	assert.Contains(t, script.Body, `UPLOAD="${MOBILE_RELEASE_UPLOAD:-true}"`,
		"the deliberate opt-out for the first run")
	assert.Contains(t, stage, `if [ "$UPLOAD" != "true" ]; then`)
	assert.Contains(t, stage, "cannot go up over the API",
		"and the same explanation when Play refuses it rather than being told")
	assert.Contains(t, workflow.Body, "MOBILE_RELEASE_UPLOAD: ${{ inputs.upload }}")
	assert.Contains(t, workflow.Body, "if-no-files-found: ignore")
}

func TestWorkflowIsOnlyAWrapper(t *testing.T) {
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			_, workflow := render(t, spec)
			steps := codeOnly(workflow.Body)
			for _, forbidden := range []string{
				"xcodebuild", "gradlew", "altool", "security import",
				"base64", "deliver", "supply", "import-codesign-certs",
				"upload-google-play", "upload-testflight-build",
			} {
				assert.NotContains(t, steps, forbidden,
					"%q is a release step and belongs in the script", forbidden)
			}
			assert.Contains(t, workflow.Body, "workflow_dispatch:")
			assert.Contains(t, workflow.Body, "actions/checkout@v4")
			assert.Contains(t, workflow.Body, `run: bash scripts/mobile-release.sh "$CHANNEL"`)
		})
	}
}

func TestWorkflowRunsOnTheRightMachineAndSerialisesPerApp(t *testing.T) {
	_, ios := render(t, iosSpec())
	assert.Contains(t, ios.Body, "runs-on: macos-14", "Apple's toolchain exists nowhere else")

	_, android := render(t, androidSpec())
	assert.Contains(t, android.Body, "runs-on: ubuntu-latest")
	assert.Contains(t, android.Body, "actions/setup-java@v4")

	for _, workflow := range []Artifact{ios, android} {

		assert.Contains(t, workflow.Body, "group: mobile-${{ inputs.channel }}-com.example.myapp")
		assert.Contains(t, workflow.Body, "cancel-in-progress: false")
	}
}

func TestRenderKeepsANestedGradleModule(t *testing.T) {
	artifacts, err := Render(Spec{
		Platform:   domain.MobileStorePlatformAndroid,
		Identifier: "com.example.app",
		Module:     "apps:android",
	})
	require.NoError(t, err)
	assert.Contains(t, artifacts[0].Body, "MODULE='apps:android'")
}

func TestRenderRefusesWhatItCannotSafelyInterpolate(t *testing.T) {
	cases := map[string]Spec{
		"no platform":      {Identifier: "com.example.app", Scheme: "App"},
		"unknown platform": {Platform: "web", Identifier: "com.example.app"},
		"no identifier":    {Platform: domain.MobileStorePlatformIOS, Scheme: "App"},
		"shell in the identifier": {
			Platform: domain.MobileStorePlatformIOS, Identifier: "com.example$(id)", Scheme: "App",
		},
		"ios without a scheme": {
			Platform: domain.MobileStorePlatformIOS, Identifier: "com.example.app",
		},
		"android without a module": {
			Platform: domain.MobileStorePlatformAndroid, Identifier: "com.example.app",
		},
		"escaping sub-project": {
			Platform: domain.MobileStorePlatformAndroid, Identifier: "com.example.app",
			Module: "app", SubProjectPath: "../../etc",
		},
		"shell in the module": {
			Platform: domain.MobileStorePlatformAndroid, Identifier: "com.example.app",
			Module: "app;rm -rf /",
		},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Render(spec)
			assert.Error(t, err)
		})
	}
}

func TestDisplayNameIsQuotedRatherThanRefused(t *testing.T) {
	spec := iosSpec()
	spec.AppName = "It's a Test\"App"
	script, _ := render(t, spec)
	assert.Contains(t, script.Body, `APP_NAME='It'\''s a Test"App'`)

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	path := filepath.Join(t.TempDir(), "mobile-release.sh")
	require.NoError(t, os.WriteFile(path, []byte(script.Body), 0o755))
	out, err := exec.Command(bash, "-n", path).CombinedOutput()
	require.NoError(t, err, "bash -n: %s", out)
}

func bashOrSkip(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	return bash
}

// buildNumberHarness runs the generated resolve_build_number — and the
// platform's check_build_number it delegates to — on its own, so the grammar
// is asserted by bash itself rather than by reading the pattern.
func buildNumberHarness(t *testing.T, script string) string {
	t.Helper()
	return strings.Join([]string{
		"set -euo pipefail",
		`die() { echo "error: $*" >&2; exit 1; }`,
		"CHANNEL=stage",
		`BUILD_NUMBER="${BUILD_NUMBER:-}"`,
		funcBody(t, script, "check_build_number") + "\n}",
		funcBody(t, script, "resolve_build_number") + "\n}",
		"resolve_build_number",
		`printf '%s' "$BUILD_NUMBER"`,
		"",
	}, "\n")
}

func TestBuildNumberGrammarIsPerPlatform(t *testing.T) {
	bash := bashOrSkip(t)
	cases := map[string]struct {
		spec     Spec
		accepted []string
		refused  []string
	}{
		"ios": {
			spec:     iosSpec(),
			accepted: []string{"1", "0", "412", "412.54", "412.54.2", "2100000001"},
			refused:  []string{"1.2.3.4", "1..2", ".1", "1.", "abc", "-1", "1 2", "1.a"},
		},
		"android": {
			spec:     androidSpec(),
			accepted: []string{"1", "42", "2100000000"},
			refused: []string{
				"0", "01", "412.54", "412.54.2", "2100000001",
				"99999999999999999999999999", "abc", "-1", "1 2",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			script, _ := render(t, tc.spec)
			harness := filepath.Join(t.TempDir(), "harness.sh")
			require.NoError(t, os.WriteFile(harness, []byte(buildNumberHarness(t, script.Body)), 0o700))

			run := func(value string) (string, error) {
				cmd := exec.Command(bash, harness)
				cmd.Env = []string{"PATH=/usr/bin:/bin", "BUILD_NUMBER=" + value}
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			for _, value := range tc.accepted {
				out, err := run(value)
				assert.NoError(t, err, "%q must be accepted: %s", value, out)
				assert.Equal(t, value, out)
			}
			for _, value := range tc.refused {
				out, err := run(value)
				assert.Error(t, err, "%q must be refused", value)
				assert.Contains(t, out, "error: BUILD_NUMBER must be")
			}
		})
	}

	ios, _ := render(t, iosSpec())
	assert.Contains(t, funcBody(t, ios.Body, "check_build_number"), `'^[0-9]+(\.[0-9]+){0,2}$'`)
	android, _ := render(t, androidSpec())
	assert.Contains(t, funcBody(t, android.Body, "check_build_number"), `[ "$1" -gt 2100000000 ]`)
}

// TestScriptHonoursTTProjectRoot runs the generated preamble up to the line
// that fixes ROOT, from a checkout layout and from outside one.
func TestScriptHonoursTTProjectRoot(t *testing.T) {
	bash := bashOrSkip(t)
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			script, _ := render(t, spec)
			const rootLine = "ROOT=\"$PWD\"\n"
			cut := strings.Index(script.Body, rootLine)
			require.GreaterOrEqual(t, cut, 0)
			prefix := script.Body[:cut+len(rootLine)] + `printf '%s' "$ROOT"` + "\n"
			assert.Contains(t, prefix, `if [ -n "${TT_PROJECT_ROOT:-}" ]; then`)

			base := t.TempDir()
			project := filepath.Join(base, "project")
			elsewhere := filepath.Join(base, "elsewhere")
			require.NoError(t, os.MkdirAll(filepath.Join(project, "scripts"), 0o755))
			require.NoError(t, os.MkdirAll(elsewhere, 0o755))
			path := filepath.Join(project, "scripts", "mobile-release.sh")
			require.NoError(t, os.WriteFile(path, []byte(prefix), 0o755))

			run := func(env ...string) (string, error) {
				cmd := exec.Command(bash, path, "stage")
				cmd.Dir = base
				cmd.Env = append([]string{"PATH=/usr/bin:/bin"}, env...)
				out, err := cmd.CombinedOutput()
				return string(out), err
			}
			resolved := func(p string) string {
				real, err := filepath.EvalSymlinks(p)
				require.NoError(t, err)
				return real
			}

			out, err := run()
			require.NoError(t, err, out)
			assert.Equal(t, resolved(project), resolved(out), "without it the project is the script's parent's parent")

			out, err = run("TT_PROJECT_ROOT=" + elsewhere)
			require.NoError(t, err, out)
			assert.Equal(t, resolved(elsewhere), resolved(out), "the local engine runs the script from outside the project")

			out, err = run("TT_PROJECT_ROOT=" + filepath.Join(base, "missing"))
			assert.Error(t, err, "set and not a directory is refused, not fallen back from")
			assert.Contains(t, out, "TT_PROJECT_ROOT")
			assert.Contains(t, out, "not a directory")
		})
	}
}

func workflowDoc(t *testing.T, body string) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(body), &doc))
	return doc
}

func mapAt(t *testing.T, m map[string]any, keys ...string) map[string]any {
	t.Helper()
	for _, key := range keys {
		next, ok := m[key].(map[string]any)
		require.True(t, ok, "%s is not a mapping", key)
		m = next
	}
	return m
}

func workflowSteps(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	raw, ok := mapAt(t, doc, "jobs", "release")["steps"].([]any)
	require.True(t, ok)
	steps := make([]map[string]any, 0, len(raw))
	for _, s := range raw {
		step, ok := s.(map[string]any)
		require.True(t, ok)
		steps = append(steps, step)
	}
	return steps
}

func TestWorkflowTakesATestBuild(t *testing.T) {
	for name, spec := range map[string]Spec{"ios": iosSpec(), "android": androidSpec()} {
		t.Run(name, func(t *testing.T) {
			_, workflow := render(t, spec)
			doc := workflowDoc(t, workflow.Body)

			assert.Equal(t,
				"${{ inputs.build_id != '' && format('tt-test-build {0}', inputs.build_id) || 'mobile-release' }}",
				doc["run-name"], "TaskTrooper finds the run it dispatched by this title")

			inputs := mapAt(t, doc, "on", "workflow_dispatch", "inputs")
			for _, input := range []string{"build_number", "ref", "build_id"} {
				in := mapAt(t, inputs, input)
				assert.Equal(t, "string", in["type"], input)
				assert.Equal(t, "", in["default"], "%s empty means a person's run behaves as before", input)
			}
			if spec.Platform == domain.MobileStorePlatformAndroid {
				assert.Equal(t, "boolean", mapAt(t, inputs, "upload")["type"], "the first-publish opt-out stays")
			}

			assert.Contains(t, mapAt(t, doc, "concurrency")["group"],
				"${{ inputs.build_id != '' && format('-{0}', inputs.build_id) || '' }}",
				"a queued test build would otherwise cancel the one pending before it")

			steps := workflowSteps(t, doc)
			checkout, restore, release, upload := -1, -1, -1, -1
			for i, step := range steps {
				switch {
				case step["uses"] == "actions/checkout@v4":
					checkout = i
					assert.Equal(t, "${{ inputs.ref || github.ref }}", mapAt(t, step, "with")["ref"])
				case step["if"] == "inputs.ref != ''":
					restore = i
				case step["name"] == "Release":
					release = i
					assert.Equal(t, "${{ inputs.build_number || github.run_number }}", mapAt(t, step, "env")["BUILD_NUMBER"])
				case step["uses"] == "actions/upload-artifact@v4":
					upload = i
					assert.Equal(t,
						"mobile-release-com.example.myapp${{ inputs.build_id != '' && format('-{0}', inputs.build_id) || '' }}",
						mapAt(t, step, "with")["name"])
				}
				if run, ok := step["run"].(string); ok {
					assert.NotContains(t, run, "${{", "values reach a run: line through the environment")
				}
			}
			require.True(t, checkout >= 0 && restore >= 0 && release >= 0 && upload >= 0, "steps: %v", steps)
			assert.Less(t, checkout, restore)
			assert.Less(t, restore, release, "the procedure is restored before it runs")
			assert.Equal(t,
				`git fetch --no-tags --depth=1 origin "$GITHUB_SHA" && git checkout "$GITHUB_SHA" -- scripts/mobile-release.sh`,
				steps[restore]["run"])
		})
	}
}

// Test builds share the release's marketing version, so iOS prod submits a
// named build or nothing — never "the newest upload".
func TestIOSProdSubmitsOnlyANamedBuild(t *testing.T) {
	artifacts, err := Render(iosSpec())
	if err != nil {
		t.Fatal(err)
	}
	script, workflow := artifacts[0].Body, artifacts[1].Body
	for _, want := range []string{`--build_number "$RELEASE_BUILD_NUMBER"`, `if [ -z "${RELEASE_BUILD_NUMBER:-}" ]; then`, `derived=(-derivedDataPath "$WORKDIR/DerivedData")`} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if !strings.Contains(workflow, "RELEASE_BUILD_NUMBER: ${{ inputs.build_number }}") {
		t.Error("workflow does not hand prod the build_number input on its own")
	}
}
