package findings

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// lineRule matches one source line. hints are literal substrings, at least
// one of which must be on the line before the regexp runs: most lines match
// no rule, and a substring check is what keeps a 40k-file scan quick.
type lineRule struct {
	rule  string
	sev   domain.FindingSeverity
	langs map[lang]bool
	roles map[domain.ComponentRole]bool
	hints []string
	re    *regexp.Regexp
	// match replaces re when one regexp cannot say it.
	match    func(line string) bool
	skipLine *regexp.Regexp
	skipFile *regexp.Regexp
	pathRe   *regexp.Regexp
	skipPath *regexp.Regexp
	skipAux  bool
	// guarded rules ignore lines inside a debug-only region (#if DEBUG,
	// if (BuildConfig.DEBUG) { … }, if (__DEV__) …).
	guarded bool
	// comments rules read comment lines and only those.
	comments bool
	when     func(*unitScan) bool
}

func (r *lineRule) applies(u *unitScan, f fileInfo) bool {
	if r.langs != nil && !r.langs[f.lang] {
		return false
	}
	if r.roles != nil && !r.roles[u.unit.role()] {
		return false
	}
	if r.skipAux && f.aux {
		return false
	}
	if r.pathRe != nil && !r.pathRe.MatchString(f.path) {
		return false
	}
	if r.skipPath != nil && r.skipPath.MatchString(f.path) {
		return false
	}
	if r.when != nil && !r.when(u) {
		return false
	}
	return true
}

func (r *lineRule) matches(line string) bool {
	if len(r.hints) > 0 {
		hit := false
		for _, h := range r.hints {
			if strings.Contains(line, h) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if r.skipLine != nil && r.skipLine.MatchString(line) {
		return false
	}
	if regexDefinition.MatchString(line) {
		return false
	}
	if r.match != nil {
		return r.match(line)
	}
	loc := r.re.FindStringIndex(line)
	if loc == nil {
		return false
	}
	// A string literal that is exactly the matched text names the API, it
	// does not call it: a rule list, a lint config, a doc string.
	return !(loc[0] > 0 && loc[1] < len(line) && isQuote(line[loc[0]-1]) && line[loc[1]] == line[loc[0]-1])
}

// regexDefinition is a line that defines a pattern rather than running
// code: a scanner's own rule table would otherwise report itself.
var regexDefinition = re(`regexp\.MustCompile\(|\bre\(\x60|new RegExp\(|re\.compile\(|Pattern\.compile\(|hints:\s*\[\]string\{`)

func isQuote(c byte) bool { return c == '"' || c == '\'' || c == '`' }

func containsFold(line string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(line, sub) || strings.Contains(line, strings.ToUpper(sub)) {
			return true
		}
	}
	return false
}

func langs(ls ...lang) map[lang]bool {
	m := make(map[lang]bool, len(ls))
	for _, l := range ls {
		m[l] = true
	}
	return m
}

func roles(rs ...domain.ComponentRole) map[domain.ComponentRole]bool {
	m := make(map[domain.ComponentRole]bool, len(rs))
	for _, r := range rs {
		m[r] = true
	}
	return m
}

var (
	serverRoles = roles(domain.ComponentRoleBackend, domain.ComponentRoleWorker)
	clientRoles = roles(domain.ComponentRoleFrontend, domain.ComponentRoleMobile, domain.ComponentRoleDesktop)
	appRoles    = roles(domain.ComponentRoleMobile, domain.ComponentRoleDesktop)
	re          = regexp.MustCompile
)

var envSwitch = re(`(?i)(os\.Getenv|os\.LookupEnv|process\.env|import\.meta\.env|os\.environ|getenv|ParseLevel|LOG_LEVEL|LOGLEVEL|viper|flag\.|IsDevelopment|BuildConfig\.DEBUG|kDebugMode|settings\.DEBUG|cfg\.|config\.)`)

var (
	sqlVerb   = re(`(?i)\b(select\s.+\sfrom|insert\s+into|update\s+\w+\s+set|delete\s+from)\b`)
	sqlTagged = re(`(?i)(\bsql\x60|\.sql\x60|queryRaw\x60|executeRaw\x60|\bsql\.raw\(|knex\.raw\([^)]*\?)`)
	// A value spliced into the statement right where a bind parameter
	// belongs: after =, LIKE, IN ( or VALUES (. A table or column list
	// concatenated in is not a value and does not match.
	sqlSplice    = re(`(?i)(=|<>|!=|\blike|\bin\s*\(|\bvalues\s*\()\s*'?%?\s*(\$\{|\{[a-z_][\w.\[\]'"]*\}|\{\}|#\{|\$[a-z_]|["'\x60]\s*(\+|\.\.?)\s*[a-z_$(])`)
	sqlPercent   = re(`(?i)(=|<>|!=|\blike|\bin\s*\(|\bvalues\s*\()\s*'?%[sdv]`)
	sqlFormatted = re(`fmt\.Sprintf|String\.format|format!\(|["']\s*%\s*[\w(]`)
)

func sqlInjectionLine(line string) bool {
	if !strings.ContainsAny(line, "\"'`") || !containsFold(line, "from", "into", " set ") {
		return false
	}
	if !sqlVerb.MatchString(line) || sqlTagged.MatchString(line) {
		return false
	}
	if sqlSplice.MatchString(line) {
		return true
	}
	// %s is psycopg's own bind placeholder; it is only a splice when the
	// string is formatted in place.
	return sqlPercent.MatchString(line) && sqlFormatted.MatchString(line)
}

var lineRules = []*lineRule{
	{
		rule: RuleTLSVerificationDisabled, hints: []string{"InsecureSkipVerify", "rejectUnauthorized", "NODE_TLS_REJECT_UNAUTHORIZED", "verify", "CERT_NONE", "_create_unverified_context", "check_hostname", "HostnameVerifier", "hostnameVerifier", "TrustAll", "TrustSelfSigned", "badCertificateCallback", "DisabledTrustEvaluator", "DisabledEvaluator", "ServerCertificateCustomValidationCallback", "DangerousAcceptAnyServerCertificateValidator", "CURLOPT_SSL_VERIFYPEER", "VERIFY_NONE", "danger_accept_invalid_certs", "allowsInvalidSSLCertificate"},
		re: re(`InsecureSkipVerify:\s*true|rejectUnauthorized:\s*false|NODE_TLS_REJECT_UNAUTHORIZED["']?\]?\s*=\s*["']?0|\bverify\s*=\s*False\b|ssl\.CERT_NONE|_create_unverified_context|check_hostname\s*=\s*False|NoopHostnameVerifier|ALLOW_ALL_HOSTNAME_VERIFIER|TrustAllStrategy|TrustSelfSignedStrategy|TrustAllCerts|[hH]ostnameVerifier\s*(\{|\(\s*\([^)]*\)\s*->)\s*[^}]*\btrue\b|badCertificateCallback\s*=.*=>\s*true|DisabledTrustEvaluator|DisabledEvaluator\(|ServerCertificateCustomValidationCallback\s*=.*=>\s*true|DangerousAcceptAnyServerCertificateValidator|CURLOPT_SSL_VERIFYPEER\s*,\s*(false|0)|VERIFY_NONE|danger_accept_invalid_certs\(\s*true|allowsInvalidSSLCertificate\s*=\s*true`),
	},
	{
		rule: RuleSQLInjectionRisk, match: sqlInjectionLine,
		hints: []string{"SELECT", "select", "INSERT", "insert", "UPDATE", "update", "DELETE", "delete"},
	},
	{
		rule: RuleDynamicCodeExecution, langs: langs(langJS), hints: []string{"eval", "Function", "setTimeout", "setInterval"},
		re: re(`(^|[^\w.$])eval\s*\(|\bnew\s+Function\s*\(|\bset(Timeout|Interval)\(\s*["'\x60]`),
	},
	{
		rule: RuleDynamicCodeExecution, langs: langs(langPython), hints: []string{"eval", "exec", "pickle", "yaml.load", "marshal"},
		re: re(`(^|[^\w.])(eval|exec)\s*\(|\bpickle\.loads?\(|\bcPickle\.loads?\(|\byaml\.load\(|\bmarshal\.loads?\(`), skipLine: re(`SafeLoader|CSafeLoader|safe_load`),
	},
	{
		rule: RuleDynamicCodeExecution, langs: langs(langPHP, langRuby, langJava, langKotlin), hints: []string{"eval", "create_function", "unserialize", "ObjectInputStream"},
		re: re(`(^|[^\w>.:])eval\s*\(|\bcreate_function\s*\(|(^|[^\w>])unserialize\s*\(|new\s+ObjectInputStream\(`),
	},
	{
		rule: RuleUnsafeHTMLInjection, langs: langs(langJS), hints: []string{"dangerouslySetInnerHTML", "v-html", "innerHTML", "outerHTML", "bypassSecurityTrust", "document.write", "insertAdjacentHTML", "{@html"},
		re:       re(`dangerouslySetInnerHTML|\bv-html\s*=|\[innerHTML\]\s*=|bypassSecurityTrust(Html|Script|Url|ResourceUrl|Style)\s*\(|\.(inner|outer)HTML\s*\+?=|document\.write(ln)?\s*\(|insertAdjacentHTML\s*\(|\{@html\s`),
		skipLine: re(`(?i)sanitiz|dompurify|purify|\.(inner|outer)HTML\s*=\s*(""|''|\x60\x60)\s*;?\s*$`),
	},
	{
		rule: RuleUnsafeHTMLInjection, langs: langs(langPHP), hints: []string{"$_GET", "$_POST", "$_REQUEST"},
		re: re(`\b(echo|print)\b[^;]*\$_(GET|POST|REQUEST|COOKIE)\b`), skipLine: re(`htmlspecialchars|htmlentities|strip_tags|e\(`),
	},
	{
		rule:  RulePermissiveCORS,
		hints: []string{"cors", "CORS", "Origin", "origin", "AllowAllOrigins", "AllowedOrigins", "AllowOrigins", "allow_origins", "CrossOrigin", "AllowAnyOrigin", "origins"},
		re:    re(`(^|[^\w.])cors\(\s*\)|\borigin\s*:\s*(["']\*["']|true)\s*[,}]|Access-Control-Allow-Origin["']?\s*[,:]\s*["']\*|AllowAllOrigins:\s*true|AllowedOrigins:\s*\[\]string\{\s*"\*"|AllowOrigins:\s*(\[\]string\{\s*)?"\*"|allow_origins\s*=\s*\[\s*["']\*["']|CORS_ORIGIN_ALLOW_ALL\s*=\s*True|CORS_ALLOW_ALL_ORIGINS\s*=\s*True|(^|[^\w.])CORS\(\s*app\s*\)|@CrossOrigin\s*(\(\s*\)|\(\s*(origins\s*=\s*)?"\*"|$)|allowedOrigins\(\s*"\*"|addAllowedOrigin\(\s*"\*"|AllowAnyOrigin\(\)|\borigins\s+["']\*["']`),
	},
	{
		rule: RuleInsecureTokenStorage, langs: langs(langJS), roles: roles(domain.ComponentRoleFrontend, domain.ComponentRoleMobile, domain.ComponentRoleDesktop, ""),
		hints: []string{"localStorage", "AsyncStorage", "sessionStorage"},
		re:    re(`(?i)(localStorage|AsyncStorage)\s*(\.setItem\(\s*["'\x60][^"'\x60]*|\[\s*["'\x60][^"'\x60]*)(token|jwt|auth|secret|password|passwd|credential|refresh)`),
	},
	{
		rule: RuleInsecureTokenStorage, langs: langs(langSwift), hints: []string{"UserDefaults"},
		re: re(`(?i)UserDefaults.*\.set\(.*forKey:\s*"[^"]*(token|jwt|secret|password|passwd|credential)`),
	},
	{
		rule: RuleInsecureTokenStorage, langs: langs(langKotlin, langJava, langDart), hints: []string{"putString", "setString"},
		re:       re(`(?i)(putString|setString)\(\s*["'][^"']*(token|jwt|secret|password|passwd|credential)`),
		skipFile: re(`EncryptedSharedPreferences|flutter_secure_storage|FlutterSecureStorage|Keystore`),
	},
	{
		rule: RuleHTTPBodyLogging, langs: langs(langKotlin, langJava), hints: []string{"HttpLoggingInterceptor"},
		re: re(`HttpLoggingInterceptor\.Level\.(BODY|HEADERS)`), guarded: true, skipFile: re(`BuildConfig\.DEBUG`),
	},
	{
		rule: RuleHTTPBodyLogging, langs: langs(langDart), hints: []string{"LogInterceptor"},
		re: re(`LogInterceptor\([^)]*(requestBody|responseBody|requestHeader)\s*:\s*true`), guarded: true, skipFile: re(`kDebugMode|kReleaseMode`),
	},
	{
		rule: RuleHTTPBodyLogging, hints: []string{"DumpRequest", "DumpResponse", "debuglevel", "req.", "request."},
		re: re(`httputil\.Dump(Request|RequestOut|Response)\(|HTTPConnection\.debuglevel\s*=\s*[1-9]|(?i)(log|logger|console|print)\w*\.?\w*\([^)]*\b(req|request)\.(body|headers|cookies)\b`),
	},
	{
		rule: RuleErrorDetailsExposed, hints: []string{"UseDeveloperExceptionPage", "stack", "format_exc", "display_errors"},
		re:       re(`UseDeveloperExceptionPage\(\)|\.(send|json|write|end)\([^)]*\b(err|error|e|ex)\.stack\b|\bstack:\s*(err|error|e|ex)\.stack\b|return[^#\n]*traceback\.format_exc\(\)|ini_set\(\s*["']display_errors["']\s*,\s*["']?(1|on|true)`),
		skipFile: re(`IsDevelopment\(\)|NODE_ENV\s*!==?\s*["']production|NODE_ENV\s*===?\s*["']development`),
	},
	{
		rule: RulePublicSourceMaps, langs: langs(langJS), hints: []string{"sourcemap", "productionSourceMap", "productionBrowserSourceMaps", "devtool"},
		pathRe: re(`(^|/)(vite|vue|next|webpack[\w.-]*|rollup|nuxt|svelte|astro|craco)\.config\.[cm]?[jt]s$|webpack\.prod[\w.-]*\.[cm]?[jt]s$`),
		re:     re(`\bsourcemap:\s*(true|["']inline["'])|productionSourceMap:\s*true|productionBrowserSourceMaps:\s*true|devtool:\s*["'](source-map|inline-source-map|eval[\w-]*|cheap[\w-]*)["']`),
	},
	{
		rule: RuleDebugModeInProduction, langs: langs(langPython), hints: []string{"DEBUG"},
		pathRe: re(`(^|/)settings(\.py$|/)`), skipPath: devPathRe, re: re(`^\s*DEBUG\s*=\s*True\b`),
	},
	{
		rule: RuleDebugModeInProduction, sev: domain.FindingSeverityMedium, langs: langs(langPython), hints: []string{"debug"},
		re: re(`\.run\([^)]*\bdebug\s*=\s*True`),
	},
	{
		rule: RuleDebugModeInProduction, sev: domain.FindingSeverityMedium, langs: langs(langGo), hints: []string{"gin.DebugMode"},
		re: re(`gin\.SetMode\(\s*gin\.DebugMode\s*\)`), skipFile: envSwitch,
	},
	{
		rule: RuleDebugModeInProduction, sev: domain.FindingSeverityMedium, langs: langs(langKotlin, langJava, langSwift),
		hints: []string{"setWebContentsDebuggingEnabled", "isInspectable"},
		re:    re(`setWebContentsDebuggingEnabled\(\s*true\s*\)|\.isInspectable\s*=\s*true`), guarded: true,
		skipFile: re(`BuildConfig\.DEBUG`),
	},
	{
		rule: RuleUnconditionalDebugLog, langs: langs(langGo), hints: []string{"DebugLevel", "TraceLevel", "NewDevelopment", "LevelDebug"},
		re:       re(`zerolog\.SetGlobalLevel\(\s*zerolog\.(Debug|Trace)Level\s*\)|logrus\.SetLevel\(\s*logrus\.(Debug|Trace)Level\s*\)|\.SetLevel\(\s*(log|logrus)\.(Debug|Trace)Level\s*\)|zap\.NewDevelopment(Config)?\(|Level:\s*slog\.LevelDebug|zapcore\.(Debug)Level\b`),
		skipFile: envSwitch, skipAux: true,
	},
	{
		rule: RuleUnconditionalDebugLog, langs: langs(langJS), hints: []string{"level", "Level"},
		re:       re(`(?i)\b(level|logLevel)\s*:\s*["'](debug|trace|silly|verbose|all)["']|\.setLevel\(\s*["'](debug|trace)["']|setLogLevel\(\s*["']?(debug|trace|verbose)`),
		skipLine: re(`process\.env|import\.meta|\?|\|\||NODE_ENV|isDev|__DEV__`), skipFile: re(`process\.env\.(LOG_LEVEL|NODE_ENV|DEBUG)|import\.meta\.env`), skipAux: true,
	},
	{
		rule: RuleUnconditionalDebugLog, langs: langs(langPython), hints: []string{"DEBUG"},
		re:       re(`basicConfig\([^)]*level\s*=\s*(logging\.)?DEBUG|\.setLevel\(\s*(logging\.)?DEBUG\s*\)|\blevel\s*=\s*["']DEBUG["']`),
		skipFile: envSwitch, skipAux: true,
	},
	{
		rule: RuleUnconditionalDebugLog, langs: langs(langJava, langKotlin, langCSharp, langDart, langRust, langSwift),
		hints:   []string{"Level", "MinimumLevel", "LevelFilter", "logLevel"},
		re:      re(`\.setLevel\(\s*(Level\.)?(DEBUG|TRACE|ALL|FINEST|FINER|FINE)\s*\)|SetMinimumLevel\(\s*LogLevel\.(Debug|Trace)\s*\)|MinimumLevel\.(Debug|Verbose)\(\)|Logger\.root\.level\s*=\s*Level\.(ALL|FINE|FINER|FINEST)|with_max_level\(\s*(tracing::)?Level::(DEBUG|TRACE)\s*\)|filter_level\(\s*(log::)?LevelFilter::(Debug|Trace)\s*\)|\.logLevel\s*=\s*\.(debug|trace|verbose)`),
		guarded: true, skipFile: envSwitch, skipAux: true,
	},
	{
		rule: RuleConsoleLogsInProduction, langs: langs(langJS), roles: clientRoles, hints: []string{"console."},
		re: re(`(^|[^\w.$])console\.(log|debug|info|trace|dir|table|count|time|timeLog)\s*\(`), guarded: true, skipAux: true,
		when: func(u *unitScan) bool { return !u.stripsConsole },
	},
	{
		rule: RuleMobileDebugPrints, langs: langs(langSwift), roles: appRoles, hints: []string{"print", "dump", "NSLog"},
		re: re(`(^|[^\w.])(print|debugPrint|dump|NSLog)\s*\(`), guarded: true, skipAux: true,
	},
	{
		rule: RuleMobileDebugPrints, langs: langs(langKotlin, langJava), roles: appRoles, hints: []string{"Log."},
		re: re(`(^|[^\w.])Log\.(v|d|i)\s*\(`), guarded: true, skipAux: true,
		when: func(u *unitScan) bool { return !u.stripsAndroidLog },
	},
	{
		rule: RuleMobileDebugPrints, langs: langs(langKotlin, langJava), roles: appRoles, hints: []string{"println", "System.out"},
		re: re(`(^|[^\w.])println\s*\(|System\.out\.print`), guarded: true, skipAux: true,
	},
	{
		rule: RuleMobileDebugPrints, langs: langs(langDart), roles: appRoles, hints: []string{"print"},
		re: re(`(^|[^\w.])(print|debugPrint)\s*\(`), guarded: true, skipAux: true,
	},
	{
		rule: RulePrintInsteadOfLogger, langs: langs(langGo), roles: serverRoles, hints: []string{"fmt.Print", "print"},
		re: re(`(^|[^\w.])fmt\.Print(ln|f)?\s*\(|^\s*print(ln)?\s*\(`), skipAux: true, skipFile: re(`(?m)^package main\b`),
	},
	{
		rule: RulePrintInsteadOfLogger, langs: langs(langPython), roles: serverRoles, hints: []string{"print"},
		re: re(`^\s*print\s*\(`), skipAux: true, skipFile: re(`if\s+__name__\s*==\s*["']__main__["']`),
		skipPath: re(`(^|/)(manage|__main__|cli|setup|conftest)\.py$`),
	},
	{
		rule: RulePrintInsteadOfLogger, langs: langs(langJava, langKotlin, langCSharp), roles: serverRoles, hints: []string{"System.", "println", "Console.Write"},
		re: re(`System\.(out|err)\.print|^\s*println\s*\(|Console\.Write(Line)?\s*\(`), skipAux: true,
	},
	{
		rule: RulePrintInsteadOfLogger, langs: langs(langJS), roles: serverRoles, hints: []string{"console."},
		re: re(`(^|[^\w.$])console\.(log|debug|info|trace|dir)\s*\(`), skipAux: true,
	},
	{
		rule: RulePrintInsteadOfLogger, langs: langs(langRuby, langRust), roles: serverRoles, hints: []string{"puts", "pp ", "println!"},
		re: re(`^\s*(puts|pp)\s|^\s*e?println!\s*\(`), skipAux: true, skipFile: re(`(?m)^fn main\(`),
	},
	{
		rule: RulePrintStackTrace, langs: langs(langJava, langKotlin, langPython, langDart), hints: []string{"printStackTrace", "print_exc", "debugPrintStack"},
		re: re(`\.printStackTrace\(\s*\)|traceback\.print_exc\(|debugPrintStack\(`),
	},
	{
		rule: RuleDebugLeftovers, langs: langs(langJS), hints: []string{"debugger"},
		re: re(`^\s*debugger\s*;?\s*$`),
	},
	{
		rule: RuleDebugLeftovers, langs: langs(langPython), hints: []string{"set_trace", "breakpoint", "pdb"},
		re: re(`\b(i?pdb|pudb|ipdb)\.set_trace\(|^\s*breakpoint\(\s*\)|^\s*import\s+(i?pdb|pudb)\b`),
	},
	{
		rule: RuleDebugLeftovers, langs: langs(langRuby), hints: []string{"binding.", "byebug", "debugger"},
		re: re(`\bbinding\.(pry|irb)\b|^\s*byebug\b|^\s*debugger\b`),
	},
	{
		rule: RuleDebugLeftovers, langs: langs(langPHP), hints: []string{"var_dump", "dd(", "dump(", "print_r", "var_export"},
		re: re(`(^|[^\w>:$])(var_dump|dd|dump|print_r|var_export)\s*\(`), skipLine: re(`(print_r|var_export)\([^;]*,\s*true\s*\)`),
	},
	{
		rule: RuleDebugLeftovers, langs: langs(langRust), hints: []string{"dbg!"},
		re: re(`(^|[^\w])dbg!\s*\(`),
	},
	{
		rule: RuleHTTPClientWithoutTimeout, langs: langs(langGo), hints: []string{"http."},
		re: re(`(^|[^\w.])http\.(Get|Post|PostForm|Head)\s*\(|(&|=\s*)http\.Client\{\s*\}`),
	},
	{
		rule: RuleForceUnwrap, langs: langs(langSwift), hints: []string{"try!", "as!"},
		re: re(`\btry!|\bas!\s`),
	},
	{
		rule: RuleForceUnwrap, langs: langs(langKotlin), hints: []string{"!!"},
		re: re(`[\w)\]]!!(\.|\s|\)|,|$)`),
	},
	{
		rule: RuleMutableDefaultArgument, langs: langs(langPython), hints: []string{"def "},
		re: re(`^\s*(async\s+)?def\s+\w+\s*\([^)]*=\s*(\[\]|\{\}|set\(\)|list\(\)|dict\(\))`),
	},
	{
		rule: RuleTypeScriptAnyOveruse, langs: langs(langJS), hints: []string{"any"},
		pathRe: re(`\.(ts|tsx|mts|cts)$`), re: re(`:\s*any\b\s*([,;)=\]>|]|\[|$)|\bas\s+any\b|<any>`),
	},
	{
		rule: RuleTypeScriptAnyOveruse, langs: langs(langJS), hints: []string{"@ts-ignore", "@ts-nocheck", "no-explicit-any"},
		pathRe: re(`\.(ts|tsx|mts|cts)$`), comments: true, re: re(`@ts-ignore|@ts-nocheck|no-explicit-any`),
	},
	{
		rule: RuleTechDebtMarkers, hints: []string{"TODO", "FIXME", "HACK", "XXX"}, comments: true,
		re: re(`\b(TODO|FIXME|HACK|XXX)\b`),
	},
	{
		rule: RuleWildcardImport, langs: langs(langPython), hints: []string{"import *"},
		re: re(`^\s*from\s+[\w.]+\s+import\s+\*`),
	},
	{
		rule: RuleBareExcept, langs: langs(langPython), hints: []string{"except"},
		re: re(`^\s*except\s*:`),
	},
	{
		rule: RuleCatchWithoutLogging, langs: langs(langJS), hints: []string{".catch("},
		re: re(`\.catch\(\s*(\(\s*\w*\s*\)|\w+)\s*=>\s*(\{\s*\}|null|undefined|void 0|false|\[\]|0)\s*\)|\.catch\(\s*(noop|_\.noop|function\s*\(\s*\w*\s*\)\s*\{\s*\})\s*\)`),
	},
	{
		rule: RuleEmptyCatchBlock, langs: langs(langRuby), hints: []string{"rescue"},
		re: re(`\brescue\s+nil\b`),
	},
}

// thresholds are rules that only mean something in bulk: one `any` is a
// judgement call, two hundred is a codebase opting out of its type checker.
var thresholds = map[string]int{
	RuleTypeScriptAnyOveruse: 15,
	RuleForceUnwrap:          10,
	RuleTechDebtMarkers:      20,
}

var debugGuardRe = map[lang]*regexp.Regexp{
	langJS:     re(`__DEV__|import\.meta\.env\.DEV|import\.meta\.env\.MODE\s*[!=]==?\s*["'](development|production)|process\.env\.NODE_ENV\s*[!=]==?\s*["'](development|production|test)|\bif\s*\(\s*!?\s*[\w.]*(isDev|IS_DEV|isDebug|DEBUG|debugMode|isDevelopment)\w*\b`),
	langKotlin: re(`BuildConfig\.DEBUG|\bif\s*\(\s*!?\s*[\w.]*(isDebug|DEBUG|debugMode)\w*\b`),
	langJava:   re(`BuildConfig\.DEBUG|\bif\s*\(\s*!?\s*[\w.]*(isDebug|DEBUG|debugMode)\w*\b`),
	langDart:   re(`kDebugMode|kReleaseMode|kProfileMode|^\s*assert\s*\(|\bif\s*\(\s*!?\s*[\w.]*(isDebug|debugMode)\w*\b`),
	langSwift:  re(`_isDebugAssertConfiguration|\bif\s+[\w.]*(isDebug|debugMode)\w*\b`),
	langGo:     re(`(?i)\bif\s+!?[\w.]*debug\w*\s*(\{|&&|\|\|)`),
}

// lineState follows what a line-by-line reader needs to know about the lines
// before: block comments, Swift #if DEBUG regions, and the brace regions an
// `if (debug) {` opened.
type lineState struct {
	lang          lang
	inBlock       bool
	inDoc         string
	ifStack       []ifFrame
	depth         int
	guards        []int
	prevGuardOpen bool
	pyGuardIndent int
}

type ifFrame struct{ debug, mentions bool }

func newLineState(l lang) *lineState { return &lineState{lang: l, pyGuardIndent: -1} }

// step reads one line and reports whether it is a comment and whether it sits
// inside a debug-only region.
func (s *lineState) step(line string) (comment, guarded bool) {
	trimmed := strings.TrimSpace(line)
	comment = s.comment(trimmed)

	if s.lang == langSwift && strings.HasPrefix(trimmed, "#") {
		s.preprocessor(trimmed)
	}
	for _, f := range s.ifStack {
		if f.debug {
			guarded = true
		}
	}

	if s.lang == langPython {
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if trimmed != "" && s.pyGuardIndent >= 0 && indent <= s.pyGuardIndent {
			s.pyGuardIndent = -1
		}
		if s.pyGuardIndent >= 0 && trimmed != "" {
			guarded = true
		}
		if pyDebugGuard.MatchString(line) {
			guarded = true
			if strings.HasSuffix(trimmed, ":") {
				s.pyGuardIndent = indent
			}
		}
		return comment, guarded
	}

	if comment {
		return comment, guarded
	}

	for _, d := range s.guards {
		if s.depth > d {
			guarded = true
		}
	}
	if s.prevGuardOpen {
		guarded = true
		s.prevGuardOpen = false
	}
	before := s.depth
	opens, closes := braces(line)
	if g := debugGuardRe[s.lang]; g != nil && g.MatchString(line) {
		guarded = true
		switch {
		case opens > closes:
			s.guards = append(s.guards, before)
		case opens == 0 && !strings.HasSuffix(trimmed, ";"):
			s.prevGuardOpen = true
		}
	}
	s.depth += opens - closes
	if s.depth < 0 {
		s.depth = 0
	}
	for len(s.guards) > 0 && s.guards[len(s.guards)-1] >= s.depth {
		s.guards = s.guards[:len(s.guards)-1]
	}
	return comment, guarded
}

var pyDebugGuard = re(`^\s*(el)?if\s+.*\b(settings\.DEBUG|DEBUG|__debug__|app\.debug|is_debug|debug_mode)\b`)

func (s *lineState) comment(trimmed string) bool {
	if s.lang.hashComments() {
		if s.inDoc != "" {
			if strings.Contains(trimmed, s.inDoc) {
				s.inDoc = ""
			}
			return true
		}
		for _, q := range []string{`"""`, `'''`} {
			if strings.HasPrefix(trimmed, q) || strings.HasPrefix(trimmed, "r"+q) || strings.HasPrefix(trimmed, "f"+q) {
				if strings.Count(trimmed, q) == 1 {
					s.inDoc = q
				}
				return true
			}
		}
		if s.lang == langRuby && strings.HasPrefix(trimmed, "=begin") {
			s.inDoc = "=end"
			return true
		}
		return strings.HasPrefix(trimmed, "#")
	}
	if s.inBlock {
		if strings.Contains(trimmed, "*/") {
			s.inBlock = false
		}
		return true
	}
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "<!--") {
		return true
	}
	if s.lang == langPHP && strings.HasPrefix(trimmed, "#") {
		return true
	}
	if strings.HasPrefix(trimmed, "/*") {
		if !strings.Contains(trimmed[2:], "*/") {
			s.inBlock = true
		}
		return true
	}
	return false
}

func (s *lineState) preprocessor(trimmed string) {
	switch {
	case strings.HasPrefix(trimmed, "#if"):
		mentions := strings.Contains(trimmed, "DEBUG")
		s.ifStack = append(s.ifStack, ifFrame{debug: mentions && !strings.Contains(trimmed, "!DEBUG"), mentions: mentions})
	case strings.HasPrefix(trimmed, "#elseif"):
		if n := len(s.ifStack); n > 0 {
			mentions := strings.Contains(trimmed, "DEBUG")
			s.ifStack[n-1] = ifFrame{debug: mentions && !strings.Contains(trimmed, "!DEBUG"), mentions: mentions}
		}
	case strings.HasPrefix(trimmed, "#else"):
		if n := len(s.ifStack); n > 0 && s.ifStack[n-1].mentions {
			s.ifStack[n-1].debug = !s.ifStack[n-1].debug
		}
	case strings.HasPrefix(trimmed, "#endif"):
		if n := len(s.ifStack); n > 0 {
			s.ifStack = s.ifStack[:n-1]
		}
	}
}

// braces counts { and } outside string literals on one line. Strings that
// span lines (template literals, Swift multi-line strings) can throw it off
// by one region, which only costs a guard ending early or late.
func braces(line string) (opens, closes int) {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '/':
			if i+1 < len(line) && line[i+1] == '/' {
				return opens, closes
			}
		case '{':
			opens++
		case '}':
			closes++
		}
	}
	return opens, closes
}
