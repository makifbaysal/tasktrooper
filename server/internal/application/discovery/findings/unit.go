package findings

import (
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	oversizedLines   = 1500
	minSourcesForGap = 5
	minSourcesTests  = 10
)

var manifestBases = map[string]bool{
	"package.json": true, "go.mod": true, "requirements.txt": true, "pyproject.toml": true, "Pipfile": true,
	"setup.py": true, "setup.cfg": true, "build.gradle": true, "build.gradle.kts": true, "pom.xml": true,
	"Podfile": true, "Package.swift": true, "Cartfile": true, "pubspec.yaml": true, "Gemfile": true,
	"composer.json": true, "Cargo.toml": true, "project.pbxproj": true, "libs.versions.toml": true,
	"deno.json": true,
}

// markers are facts a component rule needs from the code itself, read once
// per file while the line rules run. Each is a set of lower-case substrings:
// a case-insensitive regexp alternation over every file is what made the
// first version of this scan slow.
var markers = map[string][]string{
	"slog":           {`"log/slog"`},
	"pyLogging":      {"import logging", "from logging import", "structlog", "loguru"},
	"jvmLogging":     {"loggerfactory", "logger.getlogger", "kotlinlogging", "java.util.logging", "logmanager.getlogger", "@slf4j", "@log4j", "timber.log"},
	"dotnetLogging":  {"ilogger", "serilog", "nlog", "log4net"},
	"phpLogging":     {"log::", "loggerinterface", "monolog"},
	"rubyLogging":    {"logger.new", "rails.logger"},
	"errorBoundary":  {"componentdidcatch", "getderivedstatefromerror", "errorboundary", "errorelement", "react-error-boundary"},
	"health":         {"/health", `"health`, "'health", "healthz", "healthcheck", "health-check", "health_check", "readyz", "livez", "readiness", "liveness", "/ping", `"/status`, "actuator", "maphealthchecks", "addhealthchecks"},
	"requestLogging": {"morgan", "pino-http", "express-winston", "express-pino-logger", "koa-logger", "hapi-pino", "@hapi/good", "logginginterceptor", "requestlogg", "request_logg", "accesslog", "access_log", "access-log", "gin.logger(", "gin.default(", "middleware.logger", "middleware.requestlogger", "hlog.", "middleware/logger", "logger.new(", "otelhttp", "otelgin", "otelecho", "otelfiber", "otelchi", "loggingmiddleware", "logging_middleware", "logmiddleware", "withlogging", "requestloggingfilter", "calllogging", "usehttplogging", "useserilogrequestlogging", "reqlogger", "httplogger"},
	"crash":          {"sentry", "crashlytics", "bugsnag", "datadog", "rollbar", "newrelic", "honeybadger", "instabug", "raygun", "logrocket", "trackjs", "appcenter", "embrace", "airbrake", "highlight.run", "@highlight-run", "apm-rum", "bugfender", "faro", "captureexception"},
	"goHTTP":         {`net/http"`, "github.com/gin-gonic", "github.com/labstack", "github.com/gofiber", "github.com/go-chi", "github.com/gorilla"},
}

func (s *unitScan) mark(lower string) {
	for name, subs := range markers {
		if s.markers[name] {
			continue
		}
		for _, sub := range subs {
			if strings.Contains(lower, sub) {
				s.markers[name] = true
				break
			}
		}
	}
}

var (
	crashDeps      = re(`(?i)sentry|crashlytics|bugsnag|datadog|dd-sdk|rollbar|newrelic|new-relic|honeybadger|instabug|raygun|logrocket|trackjs|appcenter|embrace|airbrake|highlight\.run|@highlight-run|apm-rum|bugfender|@grafana/faro|posthog|glitchtip|appsignal|opentelemetry`)
	goLogDeps      = re(`zerolog|go\.uber\.org/zap|sirupsen/logrus|log15|apex/log|charmbracelet/log|go-hclog|go-kit/log|golang/glog|k8s\.io/klog`)
	jsLogDeps      = re(`"(pino|winston|bunyan|log4js|@nestjs/common|consola|loglevel|signale|tslog|roarr|@aws-lambda-powertools/logger|fastify|@hapi/hapi|@logtail/node|electron-log)"`)
	jvmLogDeps     = re(`slf4j|logback|log4j|spring-boot|kotlin-logging|microutils|tinylog|ktor-server|timber|quarkus|micronaut`)
	rustLogDeps    = re(`(?m)^\s*(log|tracing|slog|env_logger|fern|flexi_logger|log4rs)\s*=`)
	rubyLogDeps    = re(`rails|semantic_logger|lograge`)
	phpLogDeps     = re(`laravel/framework|symfony|monolog`)
	jsHTTPDeps     = re(`"(express|koa|@nestjs/core|@hapi/hapi|restify|polka|hono)"`)
	jvmHTTPDeps    = re(`spring-boot-starter-web|ktor-server|javalin|spark-core|dropwizard|quarkus-resteasy|micronaut-http`)
	healthDeps     = re(`spring-boot-starter-actuator|terminus|lightship|express-healthcheck|@godaddy/terminus|health-checks|fastify-healthcheck|django-health-check|flask-healthz|healthcheck`)
	pyHTTPDeps     = re(`(?i)\b(fastapi|flask|django|starlette|aiohttp|sanic|falcon|tornado)\b`)
	jsLinterDeps   = re(`"(eslint|@biomejs/biome|oxlint|xo|standard|tslint|@antfu/eslint-config)"|"eslintConfig"`)
	pyLinterConfig = re(`(?m)^\[(tool\.ruff|tool\.pylint|tool\.flake8|flake8|pylint|tool\.pyright|tool\.mypy)|\b(ruff|flake8|pylint)\b`)
	jvmLinterDeps  = re(`checkstyle|spotbugs|\bpmd\b|detekt|ktlint|spotless|errorprone`)
	phpLinterDeps  = re(`phpstan|php_codesniffer|php-cs-fixer|psalm|phpmd`)
	reactDep       = re(`"(react|react-native|next|expo|@remix-run/react|gatsby)"\s*:`)
	jsxFile        = re(`\.(jsx|tsx)$`)
	nextErrorFile  = re(`(^|/)(app/.*/)?(global-)?error\.(tsx|jsx|js|ts)$|(^|/)pages/_error\.(tsx|jsx|js|ts)$|(^|/)app/(.*/)?error\.(tsx|jsx|js|ts)$`)
)

type unitScan struct {
	tree *inventory.Tree
	unit *unit
	col  *collector
	b    *budget

	deps             string
	manifest         string
	stripsConsole    bool
	stripsAndroidLog bool

	markers   map[string]bool
	sources   map[lang]int
	jsxFiles  int
	tests     int
	files     map[string]bool
	rootFiles []string
}

func newUnitScan(tree *inventory.Tree, u *unit, col *collector, b *budget) *unitScan {
	return &unitScan{tree: tree, unit: u, col: col, b: b, markers: map[string]bool{}, sources: map[lang]int{}, files: map[string]bool{}}
}

func (s *unitScan) run() {
	s.loadBuildFacts()
	for _, f := range s.unit.files {
		s.files[f.base] = true
		switch f.kind {
		case kindSource:
			if f.test {
				s.tests++
				continue
			}
			s.scanSource(f)
		case kindConfig:
			if !f.test {
				s.scanConfig(f)
			}
		case kindEnv:
			s.scanEnv(f)
		}
	}
	if s.unit.comp != nil {
		s.componentRules()
	}
	for rule, min := range thresholds {
		if n := s.col.count(s.unit.path, rule); n > 0 && n < min {
			s.col.drop(s.unit.path, rule)
		}
	}
}

func (s *unitScan) read(f fileInfo) (string, bool) {
	if s.b.exhausted {
		return "", false
	}
	data, err := s.tree.Read(f.path)
	if err != nil || len(data) == 0 || len(data) > maxFileBytes || !s.b.take(int64(len(data))) {
		return "", false
	}
	return string(data), true
}

func (s *unitScan) add(rule string, sev domain.FindingSeverity, p string, line int, note string) {
	s.col.add(s.unit.path, rule, sev, domain.SourceEvidence{Path: p, Line: line, Note: note})
}

// loadBuildFacts reads the unit's manifests and build configs before any
// rule runs: whether the build strips console calls decides a whole rule.
func (s *unitScan) loadBuildFacts() {
	var deps strings.Builder
	read := 0
	for _, f := range s.unit.files {
		if f.test || read >= 40 {
			continue
		}
		switch {
		case manifestBases[f.base] || strings.HasSuffix(f.base, ".csproj") || strings.HasPrefix(f.base, "requirements"):
			if content, ok := s.read(f); ok {
				read++
				deps.WriteString(content)
				deps.WriteByte('\n')
				if s.manifest == "" && f.base != "project.pbxproj" {
					s.manifest = f.path
				}
				if isBuildConfig(f.base) && consoleStrip.MatchString(content) {
					s.stripsConsole = true
				}
			}
		case isBuildConfig(f.base):
			if content, ok := s.read(f); ok && consoleStrip.MatchString(content) {
				s.stripsConsole = true
			}
		case isProguard(f):
			if content, ok := s.read(f); ok && proguardStripsLog.MatchString(content) {
				s.stripsAndroidLog = true
			}
		}
	}
	s.deps = deps.String()
	if strings.Contains(s.deps, "transform-remove-console") {
		s.stripsConsole = true
	}
	for _, f := range s.tree.Files {
		if !strings.Contains(f, "/") {
			s.rootFiles = append(s.rootFiles, f)
		}
	}
}

func (s *unitScan) scanSource(f fileInfo) {
	s.sources[f.lang]++
	if jsxFile.MatchString(f.path) {
		s.jsxFiles++
	}
	if nextErrorFile.MatchString(f.path) {
		s.markers["errorBoundary"] = true
	}
	content, ok := s.read(f)
	if !ok || generatedContent(content) {
		return
	}
	s.mark(strings.ToLower(content))
	lines := splitLines(content)
	if len(lines) > oversizedLines {
		s.add(RuleOversizedSourceFile, "", f.path, 0, strconv.Itoa(len(lines)))
	}

	var fileRules []*lineRule
	for _, r := range lineRules {
		if r.applies(s, f) {
			fileRules = append(fileRules, r)
		}
	}
	// skipFile reads the whole file, so it runs only once a rule has
	// matched a line, and at most once per rule.
	fileSkipped := map[*lineRule]bool{}
	skipsFile := func(r *lineRule) bool {
		if r.skipFile == nil {
			return false
		}
		skipped, seen := fileSkipped[r]
		if !seen {
			skipped = r.skipFile.MatchString(content)
			fileSkipped[r] = skipped
		}
		return skipped
	}

	st := newLineState(f.lang)
	skip := make([]bool, len(lines))
	for i, line := range lines {
		if len(line) > maxLineLength {
			skip[i] = true
			continue
		}
		comment, guarded := st.step(line)
		skip[i] = comment
		if label, ok := secretOnLine(line, nextLine(lines, i)); ok {
			s.add(RuleHardcodedSecret, "", f.path, i+1, label)
		} else if !comment && !f.aux {
			if key, ok := credentialOnLine(line); ok {
				s.add(RuleHardcodedCredential, "", f.path, i+1, key)
			}
		}
		for _, r := range fileRules {
			if r.comments != comment || (r.guarded && guarded) {
				continue
			}
			if r.matches(line) && !skipsFile(r) {
				s.add(r.rule, r.sev, f.path, i+1, snippet(line))
			}
		}
	}
	for _, hit := range scanBlocks(f.lang, lines, skip) {
		s.add(hit.rule, "", f.path, hit.line+1, snippet(hit.text))
	}
	if f.lang == langPython {
		for _, hit := range pythonRequestsWithoutTimeout(lines, skip) {
			s.add(hit.rule, "", f.path, hit.line+1, snippet(hit.text))
		}
	}
}

func (s *unitScan) scanConfig(f fileInfo) {
	var rules []configRule
	for _, r := range configRules {
		if r.file(f) {
			rules = append(rules, r)
		}
	}
	plist, gradle := isPlist(f), isGradle(f)
	content, ok := s.read(f)
	if !ok {
		return
	}
	lower := strings.ToLower(content)
	for _, name := range []string{"health", "requestLogging", "crash"} {
		for _, sub := range markers[name] {
			if strings.Contains(lower, sub) {
				s.markers[name] = true
				break
			}
		}
	}
	lines := splitLines(content)
	example := devOnly(f.path)
	for i, line := range lines {
		if len(line) > maxLineLength {
			continue
		}
		if label, ok := secretOnLine(line, nextLine(lines, i)); ok {
			s.add(RuleHardcodedSecret, "", f.path, i+1, label)
		} else if !example {
			if key, ok := credentialOnLine(line); ok {
				s.add(RuleHardcodedCredential, "", f.path, i+1, key)
			}
		}
		for _, r := range rules {
			if r.re.MatchString(line) {
				s.add(r.rule, r.sev, f.path, i+1, snippet(line))
			}
		}
	}
	if plist {
		for _, i := range plistArbitraryLoads(lines) {
			s.add(RuleCleartextTrafficAllowed, "", f.path, i+1, snippet(lines[i]))
		}
	}
	if gradle {
		for _, hit := range gradleReleaseHits(lines) {
			s.add(hit.rule, "", f.path, hit.line+1, snippet(lines[hit.line]))
		}
	}
}

func (s *unitScan) scanEnv(f fileInfo) {
	if exampleEnvFile(f.base) {
		return
	}
	content, ok := s.read(f)
	if !ok {
		return
	}
	var rules []configRule
	for _, r := range configRules {
		if r.file(f) {
			rules = append(rules, r)
		}
	}
	lines := splitLines(content)
	for i, line := range lines {
		if name, ok := envSecret(line); ok {
			s.add(RuleCommittedEnvFile, "", f.path, i+1, name)
		} else if label, ok := secretOnLine(line, nextLine(lines, i)); ok {
			s.add(RuleHardcodedSecret, "", f.path, i+1, label)
		}
		for _, r := range rules {
			if r.re.MatchString(line) {
				s.add(r.rule, r.sev, f.path, i+1, snippet(line))
			}
		}
	}
}

func nextLine(lines []string, i int) string {
	if i+1 < len(lines) {
		return lines[i+1]
	}
	return ""
}

func (s *unitScan) dominant() (lang, int) {
	var best lang
	n := 0
	for l, c := range s.sources {
		if c > n || c == n && l < best {
			best, n = l, c
		}
	}
	return best, n
}

func (s *unitScan) totalSources() int {
	n := 0
	for _, c := range s.sources {
		n += c
	}
	return n
}

func (s *unitScan) evidence() (string, int) {
	if s.manifest != "" {
		return s.manifest, 0
	}
	if s.unit.path == "." || s.unit.path == "" {
		return ".", 0
	}
	return s.unit.path, 0
}

func (s *unitScan) gap(rule string, sev domain.FindingSeverity) {
	p, line := s.evidence()
	s.add(rule, sev, p, line, "")
}

// hasFile looks in the component, at the repository root and in every
// directory between them: a monorepo's tooling config often sits one level
// above the packages it covers.
func (s *unitScan) hasFile(names ...string) bool {
	for _, n := range names {
		if s.files[n] {
			return true
		}
		for _, dir := range s.ancestors() {
			if s.tree.Has(inventory.Join(dir, n)) {
				return true
			}
		}
	}
	return false
}

func (s *unitScan) hasFilePrefix(prefixes ...string) bool {
	for name := range s.files {
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
	}
	for _, dir := range s.ancestors() {
		for _, f := range s.tree.Files {
			if inventory.Dir(f) != dir {
				continue
			}
			for _, p := range prefixes {
				if strings.HasPrefix(path.Base(f), p) {
					return true
				}
			}
		}
	}
	return false
}

func (s *unitScan) ancestors() []string {
	if s.unit.path == "" || s.unit.path == "." {
		return []string{"."}
	}
	var out []string
	for dir := path.Dir(s.unit.path); ; dir = path.Dir(dir) {
		out = append(out, dir)
		if dir == "." || dir == "/" {
			return out
		}
	}
}

func (s *unitScan) componentRules() {
	role := s.unit.role()
	dom, domCount := s.dominant()
	total := s.totalSources()

	if (role == domain.ComponentRoleBackend || role == domain.ComponentRoleWorker) && domCount >= 3 && !s.hasLogging(dom) {
		s.gap(RuleNoStructuredLogging, "")
	}

	if role == domain.ComponentRoleBackend && domCount >= 3 && s.servesHTTP(dom) {
		if !s.markers["requestLogging"] {
			s.gap(RuleNoRequestLogging, "")
		}
		if !s.markers["health"] && !healthDeps.MatchString(s.deps) {
			s.gap(RuleNoHealthEndpoint, "")
		}
	}

	if clientRoles[role] && total >= minSourcesForGap && !crashDeps.MatchString(s.deps) && !s.markers["crash"] {
		sev := domain.FindingSeverityLow
		if role == domain.ComponentRoleMobile {
			sev = domain.FindingSeverityMedium
		}
		s.gap(RuleNoCrashReporting, sev)
	}

	if clientRoles[role] && s.jsxFiles >= minSourcesForGap && reactDep.MatchString(s.deps) && !s.markers["errorBoundary"] && !strings.Contains(s.deps, "react-error-boundary") {
		s.gap(RuleNoErrorBoundary, "")
	}

	if testableRoles[role] && total >= minSourcesTests && s.tests == 0 {
		s.gap(RuleNoAutomatedTests, "")
	}

	if domCount >= minSourcesForGap && !s.hasLinter(dom) {
		s.gap(RuleNoLinterConfigured, "")
	}
}

var testableRoles = roles(
	domain.ComponentRoleFrontend, domain.ComponentRoleBackend, domain.ComponentRoleMobile, domain.ComponentRoleDesktop,
	domain.ComponentRoleWorker, domain.ComponentRoleLibrary, domain.ComponentRoleCLI, domain.ComponentRoleGame,
)

func (s *unitScan) hasLogging(l lang) bool {
	switch l {
	case langGo:
		return s.markers["slog"] || goLogDeps.MatchString(s.deps)
	case langJS:
		return jsLogDeps.MatchString(s.deps)
	case langPython:
		return s.markers["pyLogging"]
	case langJava, langKotlin:
		return s.markers["jvmLogging"] || jvmLogDeps.MatchString(s.deps)
	case langCSharp:
		return s.markers["dotnetLogging"] || strings.Contains(s.deps, "Microsoft.AspNetCore") || strings.Contains(s.deps, "Microsoft.NET.Sdk.Web")
	case langRust:
		return rustLogDeps.MatchString(s.deps)
	case langRuby:
		return s.markers["rubyLogging"] || rubyLogDeps.MatchString(s.deps)
	case langPHP:
		return s.markers["phpLogging"] || phpLogDeps.MatchString(s.deps)
	}
	return true
}

func (s *unitScan) servesHTTP(l lang) bool {
	switch l {
	case langGo:
		return s.markers["goHTTP"]
	case langJS:
		return jsHTTPDeps.MatchString(s.deps)
	case langJava, langKotlin:
		return jvmHTTPDeps.MatchString(s.deps)
	case langPython:
		return pyHTTPDeps.MatchString(s.deps)
	}
	return false
}

func (s *unitScan) hasLinter(l lang) bool {
	switch l {
	case langJS:
		if s.hasFilePrefix(".eslintrc", "eslint.config.") {
			return true
		}
		return jsLinterDeps.MatchString(s.deps) || s.hasFile("biome.json", "biome.jsonc", ".oxlintrc.json", "tslint.json", "deno.json", "deno.jsonc") || s.rootDepsMatch(jsLinterDeps)
	case langPython:
		return s.hasFile("ruff.toml", ".ruff.toml", ".flake8", ".pylintrc", "pylintrc", "mypy.ini", ".mypy.ini") || pyLinterConfig.MatchString(s.deps) || s.rootDepsMatch(pyLinterConfig) || s.rootFileMatches(".pre-commit-config.yaml", pyLinterConfig)
	case langGo:
		return s.hasFile(".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json") || s.workflowsMention(`golangci|staticcheck|go vet|revive`)
	case langSwift:
		return s.hasFile(".swiftlint.yml", ".swiftlint.yaml", ".swiftformat") || strings.Contains(s.deps, "SwiftLint")
	case langKotlin, langJava:
		return jvmLinterDeps.MatchString(s.deps) || s.hasFile("detekt.yml", "detekt-config.yml", "checkstyle.xml", ".editorconfig") || strings.Contains(s.deps, "com.android.")
	case langDart:
		return s.hasFile("analysis_options.yaml")
	case langRuby:
		return s.hasFile(".rubocop.yml", ".standard.yml") || strings.Contains(s.deps, "rubocop")
	case langPHP:
		return phpLinterDeps.MatchString(s.deps) || s.hasFile("phpstan.neon", "phpstan.neon.dist", "phpcs.xml", "phpcs.xml.dist", ".php-cs-fixer.php", ".php-cs-fixer.dist.php", "psalm.xml")
	}
	return true
}

func (s *unitScan) rootDepsMatch(r *regexp.Regexp) bool {
	for _, f := range s.rootFiles {
		if f == "package.json" || f == "pyproject.toml" || f == "setup.cfg" || f == "tox.ini" {
			if r.MatchString(s.tree.ReadString(f)) {
				return true
			}
		}
	}
	return false
}

func (s *unitScan) rootFileMatches(name string, r *regexp.Regexp) bool {
	return s.tree.Has(name) && r.MatchString(s.tree.ReadString(name))
}

func (s *unitScan) workflowsMention(pattern string) bool {
	r := re(pattern)
	for _, f := range s.tree.Under(".github/workflows") {
		if r.MatchString(s.tree.ReadString(f)) {
			return true
		}
	}
	return s.tree.Has("Makefile") && r.MatchString(s.tree.ReadString("Makefile"))
}
