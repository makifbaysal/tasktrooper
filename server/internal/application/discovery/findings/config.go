package findings

import (
	"path"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// configRule matches one line of a configuration file (env, yaml, properties,
// xml, Dockerfile…). file decides which files it reads at all.
type configRule struct {
	rule string
	sev  domain.FindingSeverity
	file func(f fileInfo) bool
	re   *regexp.Regexp
}

var (
	springBase  = re(`(^|/)application\.(properties|ya?ml)$`)
	springAny   = re(`(^|/)application(-[\w]+)?\.(properties|ya?ml)$`)
	dotnetBase  = re(`(^|/)appsettings\.json$`)
	logbackXML  = re(`(^|/)(logback(-spring)?|log4j2?(-spring)?)\.xml$`)
	log4jProps  = re(`(^|/)log4j2?\.properties$`)
	phpIni      = re(`(^|/)php(-production)?\.ini$`)
	androidMain = re(`(^|/)AndroidManifest\.xml$`)
	netSecurity = re(`(^|/)res/xml/[\w-]+\.xml$`)
	infoPlist   = re(`(^|/)Info\.plist$`)
	gradleBuild = re(`(^|/)build\.gradle(\.kts)?$`)
	helmValues  = re(`(^|/)values\.ya?ml$`)

	debugLevelValue = `["']?\s*[:=]\s*[:"']?\s*(debug|trace|verbose|silly|all)\b`
)

func configFile(f fileInfo) bool { return f.kind == kindConfig || f.kind == kindEnv }

func envFile(f fileInfo) bool { return f.kind == kindEnv && !exampleEnvFile(f.base) }

func prodConfig(f fileInfo) bool {
	return configFile(f) && !isDockerfile(f.base) && prodOnly(f.path) && !exampleEnvFile(f.base)
}

func shippedConfig(f fileInfo) bool { return configFile(f) && !devOnly(f.path) && !f.test }

func dockerfile(f fileInfo) bool { return isDockerfile(f.base) && !devOnly(f.path) }

func matchPath(r *regexp.Regexp, extra func(fileInfo) bool) func(fileInfo) bool {
	return func(f fileInfo) bool { return r.MatchString(f.path) && (extra == nil || extra(f)) }
}

func notDebugSourceSet(f fileInfo) bool {
	lower := strings.ToLower(f.path)
	return !strings.Contains(lower, "/debug/") && !strings.Contains(lower, "/src/test") && !f.test
}

var configRules = []configRule{
	{rule: RuleDebugLogLevelProduction, file: prodConfig,
		re: re(`(?i)\b(log[_.-]?level|loglevel|logging[_.]level[\w.\[\]"'-]*|level|min(imum)?[_-]?level|default|root)` + debugLevelValue)},
	{rule: RuleDebugLogLevelProduction, sev: domain.FindingSeverityMedium, file: matchPath(springBase, shippedConfig),
		re: re(`(?i)^\s*logging\.level\.[\w.-]+\s*[=:]\s*(debug|trace|all)\b|^\s+root:\s*(debug|trace|all)\b`)},
	{rule: RuleDebugLogLevelProduction, sev: domain.FindingSeverityMedium, file: matchPath(dotnetBase, shippedConfig),
		re: re(`(?i)"Default"\s*:\s*"(Debug|Trace)"`)},
	{rule: RuleDebugLogLevelProduction, sev: domain.FindingSeverityMedium, file: matchPath(logbackXML, shippedConfig),
		re: re(`(?i)<root\s+level\s*=\s*"(debug|trace|all)"`)},
	{rule: RuleDebugLogLevelProduction, sev: domain.FindingSeverityMedium, file: matchPath(log4jProps, shippedConfig),
		re: re(`(?i)^\s*(log4j\.rootLogger|rootLogger\.level)\s*=\s*(debug|trace|all)\b`)},
	{rule: RuleDebugLogLevelProduction, sev: domain.FindingSeverityMedium, file: dockerfile,
		re: re(`(?i)^\s*ENV\s+.*\b(LOG_LEVEL|LOGLEVEL|LOGGING_LEVEL)[= ]\s*["']?(debug|trace)\b`)},
	{rule: RuleDebugLogLevelProduction, sev: domain.FindingSeverityMedium, file: matchPath(helmValues, shippedConfig),
		re: re(`(?i)^\s*(log[_-]?level|loglevel)\s*:\s*["']?(debug|trace)\b`)},

	{rule: RuleDebugModeInProduction, file: func(f fileInfo) bool { return envFile(f) && prodOnly(f.path) },
		re: re(`(?i)^\s*(export\s+)?((APP_|FLASK_|DJANGO_)?DEBUG\s*=\s*["']?(true|1|on|yes)\b|(NODE_ENV|APP_ENV|FLASK_ENV|RAILS_ENV|ASPNETCORE_ENVIRONMENT)\s*=\s*["']?(dev|development|local|debug)\b)`)},
	{rule: RuleDebugModeInProduction, file: func(f fileInfo) bool { return prodConfig(f) && f.kind == kindConfig },
		re: re(`(?i)^\s*["']?debug["']?\s*[:=]\s*["']?true\b`)},
	{rule: RuleDebugModeInProduction, sev: domain.FindingSeverityMedium, file: dockerfile,
		re: re(`(?i)^\s*ENV\s+.*\b((NODE_ENV|FLASK_ENV|RAILS_ENV|APP_ENV|ASPNETCORE_ENVIRONMENT)[= ]\s*["']?(development|dev)\b|(APP_|FLASK_|DJANGO_)?DEBUG[= ]\s*["']?(true|1)\b)`)},
	{rule: RuleDebugModeInProduction, sev: domain.FindingSeverityMedium, file: matchPath(springBase, shippedConfig),
		re: re(`(?i)^\s*debug\s*[=:]\s*true\b`)},

	{rule: RuleErrorDetailsExposed, file: matchPath(springAny, shippedConfig),
		re: re(`(?i)include-stacktrace\s*[:=]\s*["']?always|include-exception\s*[:=]\s*true`)},
	{rule: RuleErrorDetailsExposed, file: matchPath(phpIni, shippedConfig),
		re: re(`(?i)^\s*display_errors\s*=\s*(on|1|true)\b`)},

	{rule: RuleDebuggableReleaseBuild, file: matchPath(androidMain, notDebugSourceSet),
		re: re(`android:debuggable\s*=\s*"true"`)},
	{rule: RuleCleartextTrafficAllowed, file: matchPath(androidMain, notDebugSourceSet),
		re: re(`android:usesCleartextTraffic\s*=\s*"true"`)},
	{rule: RuleCleartextTrafficAllowed, file: matchPath(netSecurity, notDebugSourceSet),
		re: re(`cleartextTrafficPermitted\s*=\s*"true"`)},

	{rule: RulePublicSourceMaps, file: func(f fileInfo) bool { return envFile(f) && !devOnly(f.path) },
		re: re(`(?i)^\s*GENERATE_SOURCEMAP\s*=\s*true\b`)},
}

// plistArbitraryLoads finds NSAllowsArbitraryLoads set to true; the value is
// on the line after the key.
func plistArbitraryLoads(lines []string) []int {
	var out []int
	for i, line := range lines {
		if !strings.Contains(line, "<key>NSAllowsArbitraryLoads") {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+2; j++ {
			if strings.Contains(lines[j], "<true/>") {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

var (
	gradleRelease       = re(`(^|[^\w])(release|getByName\(\s*"release"\s*\))\s*\{`)
	gradleDebuggable    = re(`\b(debuggable\s*(=\s*)?true|isDebuggable\s*=\s*true)\b`)
	gradleMinifyOff     = re(`\b(minifyEnabled\s*(=\s*)?false|isMinifyEnabled\s*=\s*false)\b`)
	gradleRNProguardOff = re(`enableProguardInReleaseBuilds\s*=\s*false`)
)

type gradleHit struct {
	rule string
	line int
}

// gradleReleaseHits reads the release build type's block: debuggable or
// unminified there is what ships to the store.
func gradleReleaseHits(lines []string) []gradleHit {
	var out []gradleHit
	for i, line := range lines {
		if gradleRNProguardOff.MatchString(line) {
			out = append(out, gradleHit{RuleReleaseMinifyDisabled, i})
		}
		if !gradleRelease.MatchString(line) {
			continue
		}
		depth := 0
		for j := i; j < len(lines) && j < i+60; j++ {
			if gradleDebuggable.MatchString(lines[j]) {
				out = append(out, gradleHit{RuleDebuggableReleaseBuild, j})
			}
			if gradleMinifyOff.MatchString(lines[j]) {
				out = append(out, gradleHit{RuleReleaseMinifyDisabled, j})
			}
			o, c := braces(lines[j])
			depth += o - c
			if depth <= 0 {
				break
			}
		}
	}
	return out
}

func isGradle(f fileInfo) bool { return gradleBuild.MatchString(f.path) }

func isPlist(f fileInfo) bool { return infoPlist.MatchString(f.path) && !f.test }

var buildConfigBase = re(`^((vite|webpack[\w.-]*|next|babel|metro|craco|rollup|nuxt|vue|svelte|astro|esbuild[\w.-]*)\.config\.[cm]?[jt]s|\.babelrc(\.json)?|babel\.config\.json|angular\.json|\.swcrc|package\.json)$`)

var consoleStrip = re(`drop_console|transform-remove-console|removeConsole|drop:\s*\[[^\]]*["']console["']|pure_funcs[^\]]*console\.`)

func isBuildConfig(base string) bool { return buildConfigBase.MatchString(base) }

func isProguard(f fileInfo) bool {
	return strings.HasSuffix(f.base, ".pro") || f.base == "proguard.cfg"
}

var proguardStripsLog = re(`-assumenosideeffects\s+class\s+android\.util\.Log`)

func baseIn(p string, names ...string) bool {
	b := path.Base(p)
	for _, n := range names {
		if b == n {
			return true
		}
	}
	return false
}
