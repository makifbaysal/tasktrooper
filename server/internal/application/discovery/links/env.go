package links

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// envExtractor reads environment variable NAMES and, where available,
// VALUES from three sources — example env files, docker-compose environment
// blocks, and code references — classifies each by name, and either folds it
// into an existing resource link (a manifest dependency usually gets there
// first) or creates a new one.
type envExtractor struct{}

// Real .env / .env.local are never read: they hold live secrets, and reading
// them would mean an agent quoting a real API key back at the user.
var exampleEnvFileNames = []string{
	".env.example", ".env.sample", ".env.template", ".env.dist", ".env.local.example", "example.env",
}

type envObservation struct {
	value    string
	evidence []domain.SourceEvidence
}

func (envExtractor) extract(ctx *scanCtx) {
	observations := map[string]map[string]*envObservation{}
	add := func(componentPath, name, value string, ev domain.SourceEvidence) {
		if componentPath == "" {
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		byName, ok := observations[componentPath]
		if !ok {
			byName = map[string]*envObservation{}
			observations[componentPath] = byName
		}
		obs, ok := byName[name]
		if !ok {
			byName[name] = &envObservation{value: value, evidence: []domain.SourceEvidence{ev}}
			return
		}
		if obs.value == "" && value != "" {
			obs.value = value
		}
		obs.evidence = mergeEvidence(obs.evidence, []domain.SourceEvidence{ev})
	}

	for _, exampleName := range exampleEnvFileNames {
		for _, file := range ctx.tree.ByBase(exampleName) {
			owner, ok := ctx.ownerOf(file)
			if !ok {
				continue
			}
			for _, entry := range parseDotEnv(ctx.tree.ReadString(file)) {
				add(owner, entry.name, entry.value, domain.SourceEvidence{Path: file, Line: entry.line})
			}
		}
	}

	collectComposeEnv(ctx, add)
	collectCodeEnvRefs(ctx, add)

	componentPaths := make([]string, 0, len(observations))
	for p := range observations {
		componentPaths = append(componentPaths, p)
	}
	sort.Strings(componentPaths)

	for _, componentPath := range componentPaths {
		names := make([]string, 0, len(observations[componentPath]))
		for n := range observations[componentPath] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			obs := observations[componentPath][name]
			classification, ok := classifyEnvVar(ctx, componentPath, name, obs.value)
			if !ok {
				continue
			}
			applyEnvSignal(ctx, componentPath, name, classification, obs.evidence)
		}
	}
}

func applyEnvSignal(ctx *scanCtx, componentPath, name string, c envClassification, evidence []domain.SourceEvidence) {
	if c.mergeEligible {
		if existing, ok := ctx.collector.findByFamily(componentPath, c.family); ok {
			existing.mergeEnvVars([]string{name})
			existing.mergeEvidence(evidence)
			return
		}
	}
	ctx.collector.addLink(rawSignal{
		componentPath: componentPath,
		signalKey:     "env:" + name,
		protocol:      c.protocol,
		target:        c.target,
		envVars:       []string{name},
		evidence:      evidence,
		confidence:    c.createConfidence,
		family:        c.family,
	})
}

// --- classification ---

type envClassification struct {
	mergeEligible    bool
	family           string
	target           domain.LinkTarget
	protocol         domain.LinkProtocol
	createConfidence domain.Confidence
}

var envPublicPrefixes = []string{"NEXT_PUBLIC_", "VITE_", "EXPO_PUBLIC_", "REACT_APP_", "PUBLIC_"}

func stripKnownEnvPrefix(name string) string {
	for _, p := range envPublicPrefixes {
		if strings.HasPrefix(name, p) {
			return strings.TrimPrefix(name, p)
		}
	}
	return name
}

func classifyEnvVar(ctx *scanCtx, componentPath, rawName, value string) (envClassification, bool) {
	stripped := stripKnownEnvPrefix(rawName)

	if isDatabaseEnvName(stripped) {
		vendor, name, recognized := inferDBEngine(stripped, value)
		protocol := domain.LinkSQL
		if vendor == "mongodb" {
			protocol = domain.LinkOther
		}
		conf := domain.ConfidenceMedium
		if recognized {
			conf = domain.ConfidenceHigh
		}
		return envClassification{
			mergeEligible: true,
			family:        familyOf(domain.ResourceDatabase, vendor),
			target: domain.LinkTarget{
				Kind: domain.LinkTargetResource, ResourceKind: domain.ResourceDatabase, Vendor: vendor, Name: name,
			},
			protocol: protocol, createConfidence: conf,
		}, true
	}

	if stripped == "REDIS_URL" || stripped == "REDIS_HOST" {
		return envClassification{
			mergeEligible: true,
			family:        familyOf(domain.ResourceCache, "redis"),
			target: domain.LinkTarget{
				Kind: domain.LinkTargetResource, ResourceKind: domain.ResourceCache, Vendor: "redis", Name: "Redis",
			},
			protocol: domain.LinkRedis, createConfidence: domain.ConfidenceMedium,
		}, true
	}

	if stripped == "AMQP_URL" || strings.HasPrefix(stripped, "RABBITMQ_") {
		return envClassification{
			mergeEligible: true,
			family:        familyOf(domain.ResourceQueue, "rabbitmq"),
			target: domain.LinkTarget{
				Kind: domain.LinkTargetResource, ResourceKind: domain.ResourceQueue, Vendor: "rabbitmq", Name: "RabbitMQ",
			},
			protocol: domain.LinkQueue, createConfidence: domain.ConfidenceMedium,
		}, true
	}

	if stripped == "KAFKA_BROKERS" {
		return envClassification{
			mergeEligible: true,
			family:        familyOf(domain.ResourceQueue, "kafka"),
			target: domain.LinkTarget{
				Kind: domain.LinkTargetResource, ResourceKind: domain.ResourceQueue, Vendor: "kafka", Name: "Kafka",
			},
			protocol: domain.LinkQueue, createConfidence: domain.ConfidenceMedium,
		}, true
	}

	if id, ok := matchVendorPrefix(stripped); ok {
		if entry, found := catalog.byIDOrZero(id); found {
			return envClassification{
				mergeEligible: true,
				family:        familyOf(entry.Resource.Kind, entry.Resource.Vendor),
				target: domain.LinkTarget{
					Kind: domain.LinkTargetResource, ResourceKind: entry.Resource.Kind,
					Vendor: entry.Resource.Vendor, Name: entry.Resource.Name,
				},
				protocol: entry.Protocol, createConfidence: domain.ConfidenceMedium,
			}, true
		}
	}

	if isServiceURLName(stripped) && !isExcludedSelfURL(stripped) {
		return classifyServiceURL(ctx, componentPath, rawName, value), true
	}

	return envClassification{}, false
}

var dbNameSuffixRe = regexp.MustCompile(`_DATABASE_URL$`)

func isDatabaseEnvName(name string) bool {
	switch {
	case name == "DATABASE_URL", name == "DB_URL", name == "DB_HOST":
		return true
	case dbNameSuffixRe.MatchString(name):
		return true
	case strings.HasPrefix(name, "POSTGRES_"), strings.HasPrefix(name, "PG"):
		return true
	case strings.HasPrefix(name, "MYSQL_"):
		return true
	case strings.HasPrefix(name, "MONGO") && (strings.HasSuffix(name, "_URI") || strings.HasSuffix(name, "_URL")):
		return true
	}
	return false
}

// inferDBEngine prefers the value's URL scheme over the name, since a
// DATABASE_URL example value is authoritative about which engine runs.
func inferDBEngine(name, value string) (vendor, displayName string, recognizedScheme bool) {
	scheme := ""
	if idx := strings.Index(value, "://"); idx > 0 {
		scheme = strings.ToLower(value[:idx])
	}
	switch {
	case strings.HasPrefix(scheme, "postgres"):
		return "postgres", "PostgreSQL", true
	case strings.HasPrefix(scheme, "mysql"):
		return "mysql", "MySQL", true
	case strings.HasPrefix(scheme, "mongodb"):
		return "mongodb", "MongoDB", true
	}
	switch {
	case strings.HasPrefix(name, "POSTGRES_"), strings.HasPrefix(name, "PG"):
		return "postgres", "PostgreSQL", false
	case strings.HasPrefix(name, "MYSQL_"):
		return "mysql", "MySQL", false
	case strings.HasPrefix(name, "MONGO"):
		return "mongodb", "MongoDB", false
	}
	return "database", "Database", false
}

type vendorPrefixRule struct {
	match     func(string) bool
	catalogID string
}

func prefixRule(prefix, id string) vendorPrefixRule {
	return vendorPrefixRule{match: func(n string) bool { return strings.HasPrefix(n, prefix) }, catalogID: id}
}

func exactRule(name, id string) vendorPrefixRule {
	return vendorPrefixRule{match: func(n string) bool { return n == name }, catalogID: id}
}

var envVendorRules = []vendorPrefixRule{
	prefixRule("STRIPE_", "stripe"),
	prefixRule("OPENAI_", "openai"),
	prefixRule("ANTHROPIC_", "anthropic"),
	prefixRule("SENDGRID_", "sendgrid"),
	prefixRule("TWILIO_", "twilio"),
	exactRule("SENTRY_DSN", "sentry"),
	prefixRule("ALGOLIA_", "algolia"),
	prefixRule("RESEND_", "resend"),
	prefixRule("POSTHOG_", "posthog"),
	prefixRule("CLERK_", "clerk"),
	prefixRule("SUPABASE_", "supabase"),
	prefixRule("FIREBASE_", "firebase"),
	prefixRule("AWS_", "aws_generic"),
	exactRule("GOOGLE_APPLICATION_CREDENTIALS", "gcp_generic"),
	prefixRule("GCP_", "gcp_generic"),
}

func matchVendorPrefix(name string) (string, bool) {
	for _, rule := range envVendorRules {
		if rule.match(name) {
			return rule.catalogID, true
		}
	}
	return "", false
}

var serviceURLSuffixes = []string{"_BASE_URL", "_API_URL", "_URL", "_ENDPOINT", "_HOST", "_ADDR"}

func isServiceURLName(name string) bool {
	for _, suffix := range serviceURLSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

var selfURLExact = map[string]bool{
	"PUBLIC_URL": true, "APP_URL": true, "SITE_URL": true, "BASE_URL": true,
	"HOST": true, "PORT": true, "NEXTAUTH_URL": true,
}

func isExcludedSelfURL(name string) bool {
	if selfURLExact[name] {
		return true
	}
	if strings.HasSuffix(name, "_CALLBACK_URL") || strings.HasSuffix(name, "_REDIRECT_URL") || strings.HasSuffix(name, "_WEBHOOK_URL") {
		return true
	}
	if strings.HasPrefix(name, "CORS_") || strings.HasPrefix(name, "ALLOWED_") {
		return true
	}
	if strings.Contains(name, "DEV_SERVER") {
		return true
	}
	return false
}

var vendorAPIHosts = map[string]string{
	"api.stripe.com":    "stripe",
	"api.openai.com":    "openai",
	"api.anthropic.com": "anthropic",
	"api.github.com":    "github",
	"hooks.slack.com":   "slack",
	"api.sendgrid.com":  "sendgrid",
}

func classifyServiceURL(ctx *scanCtx, componentPath, rawName, value string) envClassification {
	host, port, hasHostPort := parseHostPort(value)

	if hasHostPort && isLoopbackHost(host) {
		for _, c := range ctx.components {
			if c.Path != componentPath && c.DevPort != 0 && c.DevPort == port {
				return envClassification{
					target:           domain.LinkTarget{Kind: domain.LinkTargetComponent, ComponentPath: c.Path},
					protocol:         domain.LinkHTTP,
					createConfidence: domain.ConfidenceHigh,
				}
			}
		}
	}

	if hasHostPort {
		if vendorID, ok := vendorAPIHosts[strings.ToLower(host)]; ok {
			if entry, found := catalog.byIDOrZero(vendorID); found {
				return envClassification{
					mergeEligible: true,
					family:        familyOf(entry.Resource.Kind, entry.Resource.Vendor),
					target: domain.LinkTarget{
						Kind: domain.LinkTargetResource, ResourceKind: entry.Resource.Kind,
						Vendor: entry.Resource.Vendor, Name: entry.Resource.Name,
					},
					protocol: domain.LinkHTTP, createConfidence: domain.ConfidenceHigh,
				}
			}
		}
	}

	return envClassification{
		target: domain.LinkTarget{
			Kind: domain.LinkTargetUnresolved, ServiceHint: deriveServiceHint(rawName), URLHost: host, Port: port,
		},
		protocol: domain.LinkHTTP, createConfidence: domain.ConfidenceMedium,
	}
}

// deriveServiceHint turns BILLING_SVC_URL into "billing-svc" and
// NEXT_PUBLIC_API_URL into "api": strip the public prefix, strip the
// longest matching URL-shaped suffix, then lowercase and dasherize.
func deriveServiceHint(rawName string) string {
	name := stripKnownEnvPrefix(rawName)
	best := ""
	for _, suffix := range serviceURLSuffixes {
		if strings.HasSuffix(name, suffix) && len(suffix) > len(best) {
			best = suffix
		}
	}
	name = strings.TrimSuffix(name, best)
	name = strings.ToLower(name)
	return strings.ReplaceAll(name, "_", "-")
}

func parseHostPort(value string) (host string, port int, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, false
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return "", 0, false
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	return u.Hostname(), port, true
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "0.0.0.0", "host.docker.internal":
		return true
	}
	return false
}

// --- example env files ---

type dotenvEntry struct {
	name  string
	value string
	line  int
}

func parseDotEnv(content string) []dotenvEntry {
	var out []dotenvEntry
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "export ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.IndexByte(trimmed, '=')
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		if value != "" {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				if len(value) >= 2 {
					value = value[1 : len(value)-1]
				}
			}
		}
		if name == "" {
			continue
		}
		out = append(out, dotenvEntry{name: name, value: value, line: i + 1})
	}
	return out
}

// --- docker-compose environment ---

func collectComposeEnv(ctx *scanCtx, add func(componentPath, name, value string, ev domain.SourceEvidence)) {
	for _, file := range ctx.tree.Files {
		if !isComposeFile(baseName(file)) {
			continue
		}
		content := ctx.tree.ReadString(file)
		if content == "" {
			continue
		}
		var doc composeFile
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			continue
		}
		dir := dirName(file)
		for _, svc := range doc.Services {
			buildCtx, ok := composeBuildContext(svc.Build)
			if !ok {
				continue
			}
			resolved := joinRel(dir, buildCtx)
			comp, ok := ctx.component(resolved)
			if !ok {
				continue
			}
			for name, value := range composeEnvironment(svc.Environment) {
				add(comp.Path, name, value, domain.SourceEvidence{Path: file})
			}
		}
	}
}

func composeEnvironment(node yaml.Node) map[string]string {
	out := map[string]string{}
	switch node.Kind {
	case yaml.SequenceNode:
		for _, c := range node.Content {
			idx := strings.IndexByte(c.Value, '=')
			if idx < 0 {
				out[c.Value] = ""
				continue
			}
			out[c.Value[:idx]] = c.Value[idx+1:]
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			out[node.Content[i].Value] = node.Content[i+1].Value
		}
	}
	return out
}

func dirName(p string) string {
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		return p[:idx]
	}
	return ""
}

// --- code references ---

const (
	maxEnvScanFiles     = 3000
	maxEnvScanFileBytes = 256 * 1024
)

var envScanExtensions = map[string]bool{
	".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true, ".cjs": true, ".mts": true, ".cts": true,
	".go": true, ".py": true, ".rb": true, ".php": true, ".java": true, ".kt": true, ".kts": true,
	".rs": true, ".cs": true, ".swift": true, ".dart": true, ".vue": true, ".svelte": true,
}

var envRefPatterns = []*regexp.Regexp{
	regexp.MustCompile(`process\.env\.([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`process\.env\[["']([A-Za-z_][A-Za-z0-9_]*)["']\]`),
	regexp.MustCompile(`import\.meta\.env\.([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`os\.Getenv\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
	regexp.MustCompile(`os\.LookupEnv\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
	regexp.MustCompile(`os\.environ\[["']([A-Za-z_][A-Za-z0-9_]*)["']\]`),
	regexp.MustCompile(`os\.environ\.get\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
	regexp.MustCompile(`os\.getenv\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
	regexp.MustCompile(`ENV\[["']([A-Za-z_][A-Za-z0-9_]*)["']\]`),
	regexp.MustCompile(`ENV\.fetch\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
	regexp.MustCompile(`System\.getenv\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
	regexp.MustCompile(`env::var\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
	regexp.MustCompile(`Deno\.env\.get\(["']([A-Za-z_][A-Za-z0-9_]*)["']\)`),
}

func collectCodeEnvRefs(ctx *scanCtx, add func(componentPath, name, value string, ev domain.SourceEvidence)) {
	scanned := 0
	for _, file := range ctx.tree.Files {
		if scanned >= maxEnvScanFiles {
			break
		}
		if !envScanExtensions[extOf(file)] || isTestOrVendorPath(file) {
			continue
		}
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		if ctx.tree.Size(file) > maxEnvScanFileBytes {
			continue
		}
		content := ctx.tree.ReadString(file)
		if content == "" {
			continue
		}
		scanned++
		for _, re := range envRefPatterns {
			for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
				name := content[m[2]:m[3]]
				line := 1 + strings.Count(content[:m[0]], "\n")
				add(owner, name, "", domain.SourceEvidence{Path: file, Line: line})
			}
		}
	}
}

func isTestOrVendorPath(p string) bool {
	lower := strings.ToLower(p)
	if strings.Contains(lower, "/vendor/") || strings.HasPrefix(lower, "vendor/") {
		return true
	}
	if strings.Contains(lower, "/node_modules/") {
		return true
	}
	if strings.Contains(lower, "/__tests__/") || strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") {
		return true
	}
	base := baseName(lower)
	return strings.Contains(base, "_test.") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}

func extOf(p string) string {
	idx := strings.LastIndexByte(p, '.')
	if idx < 0 {
		return ""
	}
	return p[idx:]
}
