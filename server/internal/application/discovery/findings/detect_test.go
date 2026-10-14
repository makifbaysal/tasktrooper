package findings

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fileset map[string]string

func detect(t *testing.T, files fileset, comps ...domain.DetectedComponent) []domain.DetectedFinding {
	t.Helper()
	mapFS := fstest.MapFS{}
	for p, content := range files {
		mapFS[p] = &fstest.MapFile{Data: []byte(content)}
	}
	return Detect(inventory.FromFS(mapFS), comps).Findings
}

func comp(path string, role domain.ComponentRole) domain.DetectedComponent {
	return domain.DetectedComponent{Path: path, Role: role}
}

func find(found []domain.DetectedFinding, componentPath, rule string) *domain.DetectedFinding {
	for i := range found {
		if found[i].ComponentPath == componentPath && found[i].Rule == rule {
			return &found[i]
		}
	}
	return nil
}

func lines(found *domain.DetectedFinding) []int {
	var out []int
	for _, e := range found.Evidence {
		out = append(out, e.Line)
	}
	return out
}

// many repeats a file so component-level rules see a real codebase, not a
// two-file fixture.
func many(files fileset, prefix, ext, body string, n int) fileset {
	for i := 0; i < n; i++ {
		files[prefix+string(rune('a'+i))+ext] = body
	}
	return files
}

func TestEveryRuleHasAKnownCategoryAndSeverity(t *testing.T) {
	categories := map[domain.FindingCategory]bool{}
	for _, c := range domain.AllFindingCategories() {
		categories[c] = true
	}
	require.NotEmpty(t, Rules())
	for _, r := range Rules() {
		require.True(t, categories[r.Category], r.ID)
		require.Less(t, r.Severity.Rank(), 3, r.ID)
	}
}

func TestFrontendConsoleLogsGuardedAndStripped(t *testing.T) {
	files := fileset{
		"package.json": `{"dependencies":{"react":"18.0.0","react-dom":"18.0.0"}}`,
		"src/App.tsx": strings.Join([]string{
			`export function App() {`,
			`  console.log("rendering", props);`,
			`  if (import.meta.env.DEV) {`,
			`    console.debug("dev only");`,
			`  }`,
			`  console.error("kept: errors are not debug noise");`,
			`  // console.log("commented out")`,
			`  return null;`,
			`}`,
		}, "\n"),
	}
	found := detect(t, files, comp(".", domain.ComponentRoleFrontend))
	f := find(found, ".", RuleConsoleLogsInProduction)
	require.NotNil(t, f)
	require.Equal(t, []int{2}, lines(f))
	require.Equal(t, domain.FindingLogging, f.Category)

	files["vite.config.ts"] = `export default { esbuild: { drop: ["console", "debugger"] } }`
	found = detect(t, files, comp(".", domain.ComponentRoleFrontend))
	require.Nil(t, find(found, ".", RuleConsoleLogsInProduction), "a build that drops console calls ships none")
}

func TestReactComponentGaps(t *testing.T) {
	files := many(fileset{"package.json": `{"dependencies":{"react":"18.0.0"}}`}, "src/View", ".tsx", "export const V = () => null;\n", 6)
	found := detect(t, files, comp(".", domain.ComponentRoleFrontend))
	require.NotNil(t, find(found, ".", RuleNoErrorBoundary))
	crash := find(found, ".", RuleNoCrashReporting)
	require.NotNil(t, crash)
	require.Equal(t, domain.FindingSeverityLow, crash.Severity)
	require.Equal(t, "package.json", crash.Evidence[0].Path)

	files["src/ErrorBoundary.tsx"] = "class ErrorBoundary extends React.Component { componentDidCatch() {} }\n"
	files["package.json"] = `{"dependencies":{"react":"18.0.0","@sentry/react":"8.0.0"}}`
	found = detect(t, files, comp(".", domain.ComponentRoleFrontend))
	require.Nil(t, find(found, ".", RuleNoErrorBoundary))
	require.Nil(t, find(found, ".", RuleNoCrashReporting))
}

func TestGoBackend(t *testing.T) {
	files := fileset{
		"go.mod": "module example.com/api\n\nrequire github.com/rs/zerolog v1.33.0\n",
		"internal/app/log.go": strings.Join([]string{
			`package app`,
			`func init() { zerolog.SetGlobalLevel(zerolog.DebugLevel) }`,
		}, "\n"),
		"internal/app/handler.go": strings.Join([]string{
			`package app`,
			`func handle() {`,
			`	fmt.Println("got request")`,
			`	if err := save(); err != nil {`,
			`	}`,
			`	resp, _ := http.Get(url)`,
			`	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}`,
			`	q := fmt.Sprintf("SELECT * FROM users WHERE name = '%s'", name)`,
			`	q2 := "SELECT " + cols + " FROM users WHERE id = $1"`,
			`}`,
		}, "\n"),
		"cmd/api/main.go": "package main\n\nfunc main() { fmt.Println(\"listening\") }\n",
	}
	found := detect(t, files, comp(".", domain.ComponentRoleBackend))

	require.Equal(t, []int{2}, lines(find(found, ".", RuleUnconditionalDebugLog)))
	require.Equal(t, []int{3}, lines(find(found, ".", RulePrintInsteadOfLogger)), "package main may print")
	require.Equal(t, []int{4}, lines(find(found, ".", RuleEmptyCatchBlock)))
	require.Equal(t, []int{6}, lines(find(found, ".", RuleHTTPClientWithoutTimeout)))
	tls := find(found, ".", RuleTLSVerificationDisabled)
	require.Equal(t, domain.FindingSeverityHigh, tls.Severity)
	require.Equal(t, []int{8}, lines(find(found, ".", RuleSQLInjectionRisk)), "a column list concatenated in is not a value")
	require.Nil(t, find(found, ".", RuleNoStructuredLogging))

	files["internal/app/log.go"] = "package app\nfunc init() { lvl, _ := zerolog.ParseLevel(os.Getenv(\"LOG_LEVEL\")); zerolog.SetGlobalLevel(zerolog.DebugLevel) }\n"
	found = detect(t, files, comp(".", domain.ComponentRoleBackend))
	require.Nil(t, find(found, ".", RuleUnconditionalDebugLog), "a level read from the environment is a choice")
}

func TestBackendWithoutLoggingTestsOrHealth(t *testing.T) {
	files := many(fileset{
		"package.json": `{"dependencies":{"express":"4.0.0"}}`,
	}, "src/route", ".js", "module.exports = (req, res) => res.send('ok');\n", 10)
	found := detect(t, files, comp(".", domain.ComponentRoleBackend))
	require.NotNil(t, find(found, ".", RuleNoStructuredLogging))
	require.NotNil(t, find(found, ".", RuleNoRequestLogging))
	require.NotNil(t, find(found, ".", RuleNoHealthEndpoint))
	require.NotNil(t, find(found, ".", RuleNoAutomatedTests))
	require.NotNil(t, find(found, ".", RuleNoLinterConfigured))

	files["package.json"] = `{"dependencies":{"express":"4.0.0","pino":"9.0.0","pino-http":"10.0.0"},"devDependencies":{"eslint":"9.0.0"}}`
	files["src/health.js"] = "app.get('/healthz', (req, res) => res.send('ok'));\napp.use(require('pino-http')());\n"
	files["src/route.test.js"] = "test('ok', () => {});\n"
	found = detect(t, files, comp(".", domain.ComponentRoleBackend))
	for _, rule := range []string{RuleNoStructuredLogging, RuleNoRequestLogging, RuleNoHealthEndpoint, RuleNoAutomatedTests, RuleNoLinterConfigured} {
		require.Nil(t, find(found, ".", rule), rule)
	}
}

func TestPython(t *testing.T) {
	files := fileset{
		"app/settings.py":     "DEBUG = True\nALLOWED_HOSTS = []\n",
		"app/settings/dev.py": "DEBUG = True\n",
		"app/views.py": strings.Join([]string{
			`import requests`,
			`def view(request, user_id):`,
			`    print("view called")`,
			`    try:`,
			`        r = requests.get(URL)`,
			`    except:`,
			`        pass`,
			`    try:`,
			`        r = requests.post(`,
			`            URL,`,
			`            timeout=5,`,
			`        )`,
			`    except ValueError as e:`,
			`        return None`,
			`    except ImportError:`,
			`        pass`,
			`    cursor.execute(f"SELECT * FROM users WHERE id = {user_id}")`,
			`    cursor.execute("SELECT * FROM users WHERE id = %s", (user_id,))`,
			`def add(item, bucket=[]):`,
			`    bucket.append(item)`,
		}, "\n"),
	}
	found := detect(t, files, comp(".", domain.ComponentRoleBackend))
	debug := find(found, ".", RuleDebugModeInProduction)
	require.Equal(t, "app/settings.py", debug.Evidence[0].Path)
	require.Equal(t, 1, debug.Occurrences, "a dev settings module may debug")
	require.Equal(t, []int{3}, lines(find(found, ".", RulePrintInsteadOfLogger)))
	require.Equal(t, []int{5}, lines(find(found, ".", RuleHTTPClientWithoutTimeout)))
	require.Equal(t, []int{6}, lines(find(found, ".", RuleBareExcept)))
	require.Equal(t, []int{6}, lines(find(found, ".", RuleEmptyCatchBlock)), "ImportError fallbacks are expected")
	require.Equal(t, []int{13}, lines(find(found, ".", RuleCatchWithoutLogging)))
	require.Equal(t, []int{17}, lines(find(found, ".", RuleSQLInjectionRisk)), "%s with a parameter tuple is a bind")
	require.Equal(t, []int{19}, lines(find(found, ".", RuleMutableDefaultArgument)))
}

func TestCatchBlocks(t *testing.T) {
	files := fileset{
		"src/a.ts": strings.Join([]string{
			`try { a(); } catch (e) {}`,
			`try { a(); } catch (e) {`,
			`  // the cache is optional`,
			`}`,
			`try { a(); } catch (e) {`,
			`  logger.warn("a failed", e);`,
			`}`,
			`try { a(); } catch {`,
			`  return null;`,
			`}`,
			`try { a(); } catch (_ignored) {`,
			`  return null;`,
			`}`,
			`try { a(); } catch (err) {`,
			`  return { message: err.message };`,
			`}`,
			`fetchAll().catch(() => undefined);`,
		}, "\n"),
		"Sources/Api.swift": strings.Join([]string{
			`do { try a() } catch APIError.http(404, _) {`,
			`  return nil`,
			`}`,
			`do { try a() } catch {`,
			`}`,
		}, "\n"),
	}
	found := detect(t, files, comp(".", domain.ComponentRoleLibrary))
	empty := find(found, ".", RuleEmptyCatchBlock)
	require.ElementsMatch(t, []int{1, 4}, lines(empty))
	require.ElementsMatch(t, []int{8, 17}, lines(find(found, ".", RuleCatchWithoutLogging)))
}

func TestAndroid(t *testing.T) {
	files := fileset{
		"app/src/main/AndroidManifest.xml":  `<application android:debuggable="true" android:usesCleartextTraffic="true">`,
		"app/src/debug/AndroidManifest.xml": `<application android:debuggable="true">`,
		"app/build.gradle": strings.Join([]string{
			`android {`,
			`  buildTypes {`,
			`    release {`,
			`      minifyEnabled false`,
			`    }`,
			`    debug {`,
			`      debuggable true`,
			`    }`,
			`  }`,
			`}`,
		}, "\n"),
		"app/src/main/java/App.kt": strings.Join([]string{
			`class App {`,
			`  fun start() {`,
			`    Log.d(TAG, "token=" + token)`,
			`    if (BuildConfig.DEBUG) {`,
			`      Log.d(TAG, "debug")`,
			`    }`,
			`    Log.e(TAG, "kept")`,
			`    val name = user!!.name`,
			`    prefs.edit().putString("auth_token", token).apply()`,
			`  }`,
			`}`,
		}, "\n"),
	}
	found := detect(t, files, comp(".", domain.ComponentRoleMobile))
	require.Equal(t, []int{1}, lines(find(found, ".", RuleDebuggableReleaseBuild)), "the debug source set may be debuggable")
	require.Equal(t, []int{1}, lines(find(found, ".", RuleCleartextTrafficAllowed)))
	require.Equal(t, []int{4}, lines(find(found, ".", RuleReleaseMinifyDisabled)))
	require.Equal(t, []int{3}, lines(find(found, ".", RuleMobileDebugPrints)))
	require.Equal(t, []int{9}, lines(find(found, ".", RuleInsecureTokenStorage)))
	require.Nil(t, find(found, ".", RuleForceUnwrap), "one !! is under the bulk threshold")

	files["app/proguard-rules.pro"] = "-assumenosideeffects class android.util.Log { *; }\n"
	found = detect(t, files, comp(".", domain.ComponentRoleMobile))
	require.Nil(t, find(found, ".", RuleMobileDebugPrints), "R8 strips android.util.Log from release")
}

func TestIOS(t *testing.T) {
	files := fileset{
		"App/Info.plist": "<dict>\n\t<key>NSAppTransportSecurity</key>\n\t<dict>\n\t\t<key>NSAllowsArbitraryLoads</key>\n\t\t<true/>\n\t</dict>\n</dict>\n",
		"App/Store.swift": strings.Join([]string{
			`final class Store {`,
			`  func load() {`,
			`    print("loading")`,
			`    #if DEBUG`,
			`    print("debug only")`,
			`    #else`,
			`    print("release")`,
			`    #endif`,
			`  }`,
			`}`,
		}, "\n"),
		"AppTests/StoreTests.swift": "func testLoad() { print(\"test\") }\n",
	}
	found := detect(t, files, comp(".", domain.ComponentRoleMobile))
	require.Equal(t, []int{3, 7}, lines(find(found, ".", RuleMobileDebugPrints)))
	require.Equal(t, []int{4}, lines(find(found, ".", RuleCleartextTrafficAllowed)))
}

// fake joins a fixture secret at run time, so the source never holds a
// string a secret scanner (GitHub push protection included) would match.
func fake(parts ...string) string { return strings.Join(parts, "") }

func TestSecretsAndEnvFiles(t *testing.T) {
	files := fileset{
		".env":             "PORT=3000\nSTRIPE_SECRET_KEY=" + fake("sk_", "test_", "51Habc0123456789") + "\nDATABASE_URL=postgres://app:Xy8k2Qp@db.internal:5432/app\n",
		".env.example":     "STRIPE_SECRET_KEY=" + fake("sk_", "live_", "xxxxxxxxxxxxxxxxxxxxxxxx") + "\n",
		".env.production":  "LOG_LEVEL=debug\nNODE_ENV=development\n",
		"src/aws.js":       "const key = '" + fake("AKIA", "IOSFODNN7ABCDEFG") + "';\n",
		"src/docs.js":      "const example = '" + fake("AKIA", "IOSFODNN7EXAMPLE") + "';\n",
		"src/form.tsx":     "<textarea placeholder=\"-----BEGIN PRIVATE KEY-----\" />\n",
		"src/client.js":    "const client = new Client({ apiKey: 'a8f3k2l9q0w7e6r5t4y3' });\n",
		"src/labels.js":    "export const labels = { password: 'Password' };\n",
		"config/prod.yaml": "logging:\n  level: debug\n",
		"config/dev.yaml":  "logging:\n  level: debug\n",
	}
	found := detect(t, files, comp(".", domain.ComponentRoleBackend))

	env := find(found, ".", RuleCommittedEnvFile)
	require.NotNil(t, env)
	require.Equal(t, ".env", env.Evidence[0].Path)
	require.Equal(t, "STRIPE_SECRET_KEY", env.Evidence[0].Note)
	require.Equal(t, 2, env.Occurrences)

	secret := find(found, ".", RuleHardcodedSecret)
	require.Equal(t, 1, secret.Occurrences, "EXAMPLE keys and a bare PEM header are not secrets")
	require.Equal(t, "src/aws.js", secret.Evidence[0].Path)
	require.Equal(t, "AWS access key id", secret.Evidence[0].Note)
	require.NotContains(t, secret.Evidence[0].Note, "AKIA")

	cred := find(found, ".", RuleHardcodedCredential)
	require.Equal(t, 1, cred.Occurrences)
	require.Equal(t, "apiKey", cred.Evidence[0].Note)

	level := find(found, ".", RuleDebugLogLevelProduction)
	require.ElementsMatch(t, []string{".env.production", "config/prod.yaml"}, []string{level.Evidence[0].Path, level.Evidence[1].Path})
	require.Equal(t, domain.FindingSeverityHigh, level.Severity)
	require.NotNil(t, find(found, ".", RuleDebugModeInProduction))
}

func TestThresholdsAndOwnership(t *testing.T) {
	few := strings.Repeat("let a: any = 1;\n", 3)
	lots := strings.Repeat("let a: any = 1;\n", 20)
	files := fileset{
		"apps/web/package.json":  `{"dependencies":{}}`,
		"apps/web/src/a.ts":      few,
		"apps/api/package.json":  `{"dependencies":{}}`,
		"apps/api/src/a.ts":      lots,
		"apps/api/src/a.test.ts": strings.Repeat("let a: any = 1;\n", 50),
		".env":                   "GITHUB_TOKEN=" + fake("ghp", "_abcdefghijklmnopqrstuvwxyz0123456789AB") + "\n",
	}
	found := detect(t, files, comp("apps/web", domain.ComponentRoleFrontend), comp("apps/api", domain.ComponentRoleBackend))
	require.Nil(t, find(found, "apps/web", RuleTypeScriptAnyOveruse))
	any := find(found, "apps/api", RuleTypeScriptAnyOveruse)
	require.Equal(t, 20, any.Occurrences, "tests are not counted")
	require.Len(t, any.Evidence, domain.MaxFindingEvidence)
	require.NotNil(t, find(found, "", RuleCommittedEnvFile), "a root .env no component owns is repository-wide")
}

func TestGeneratedAndOversizedFiles(t *testing.T) {
	big := strings.Repeat("x := 1\n", oversizedLines+1)
	files := fileset{
		"big.go":        "package a\n" + big,
		"gen.go":        "// Code generated by protoc. DO NOT EDIT.\npackage a\n" + big + "fmt.Println(1)\n",
		"api.pb.go":     "package a\n" + big,
		"vendor.min.js": "console.log(1)",
	}
	found := detect(t, files, comp(".", domain.ComponentRoleBackend))
	f := find(found, ".", RuleOversizedSourceFile)
	require.Equal(t, 1, f.Occurrences)
	require.Equal(t, "big.go", f.Evidence[0].Path)
	require.Nil(t, find(found, ".", RulePrintInsteadOfLogger))
}

func TestRuleTablesDoNotReportThemselves(t *testing.T) {
	files := fileset{
		"rules.go": "package rules\nvar tls = regexp.MustCompile(`InsecureSkipVerify:\\s*true`)\nvar names = []string{\"InsecureSkipVerify\"}\n",
	}
	found := detect(t, files, comp(".", domain.ComponentRoleLibrary))
	require.Nil(t, find(found, ".", RuleTLSVerificationDisabled))
}
