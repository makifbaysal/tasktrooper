// Package findings reads a working copy for code-health problems — debug
// logging that reaches production, errors swallowed without a trace, secrets
// in the tree, missing crash reporting — and reports each as one
// domain.DetectedFinding per (component, rule). Like the rest of discovery it
// is a pure reader: no network, no LLM, so a rescan finds the same thing
// again and a person's dismissal can be keyed on it.
package findings

import (
	"sort"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Rule is one kind of problem. Its wording (title, why, how to fix) is not
// here: the UI carries it per locale and catalog/system/partials/findings
// carries what an agent reads in the task.
type Rule struct {
	ID       string                 `json:"id"`
	Category domain.FindingCategory `json:"category"`
	Severity domain.FindingSeverity `json:"severity"`
}

const (
	RuleHardcodedSecret          = "hardcoded_secret"
	RuleHardcodedCredential      = "hardcoded_credential"
	RuleCommittedEnvFile         = "committed_env_file"
	RuleTLSVerificationDisabled  = "tls_verification_disabled"
	RuleCleartextTrafficAllowed  = "cleartext_traffic_allowed"
	RuleSQLInjectionRisk         = "sql_injection_risk"
	RuleDynamicCodeExecution     = "dynamic_code_execution"
	RuleUnsafeHTMLInjection      = "unsafe_html_injection"
	RulePermissiveCORS           = "permissive_cors"
	RuleInsecureTokenStorage     = "insecure_token_storage"
	RuleHTTPBodyLogging          = "http_body_logging"
	RuleErrorDetailsExposed      = "error_details_exposed"
	RulePublicSourceMaps         = "public_source_maps"
	RuleDebugModeInProduction    = "debug_mode_in_production"
	RuleDebugLogLevelProduction  = "debug_log_level_in_production"
	RuleDebuggableReleaseBuild   = "debuggable_release_build"
	RuleReleaseMinifyDisabled    = "release_minification_disabled"
	RuleUnconditionalDebugLog    = "unconditional_debug_logging"
	RuleConsoleLogsInProduction  = "console_logs_in_production"
	RuleMobileDebugPrints        = "mobile_debug_prints"
	RulePrintInsteadOfLogger     = "print_instead_of_logger"
	RulePrintStackTrace          = "print_stack_trace"
	RuleNoStructuredLogging      = "no_structured_logging"
	RuleNoRequestLogging         = "no_request_logging"
	RuleCatchWithoutLogging      = "catch_without_logging"
	RuleEmptyCatchBlock          = "empty_catch_block"
	RuleBareExcept               = "bare_except"
	RuleNoErrorBoundary          = "no_error_boundary"
	RuleNoCrashReporting         = "no_crash_reporting"
	RuleNoHealthEndpoint         = "no_health_endpoint"
	RuleHTTPClientWithoutTimeout = "http_client_without_timeout"
	RuleForceUnwrap              = "force_unwrap"
	RuleMutableDefaultArgument   = "mutable_default_argument"
	RuleDebugLeftovers           = "debug_leftovers"
	RuleOversizedSourceFile      = "oversized_source_file"
	RuleTypeScriptAnyOveruse     = "typescript_any_overuse"
	RuleTechDebtMarkers          = "tech_debt_markers"
	RuleNoAutomatedTests         = "no_automated_tests"
	RuleNoLinterConfigured       = "no_linter_configured"
	RuleWildcardImport           = "wildcard_import"
)

var catalog = map[string]Rule{}

func define(id string, cat domain.FindingCategory, sev domain.FindingSeverity) {
	catalog[id] = Rule{ID: id, Category: cat, Severity: sev}
}

func init() {
	sec, cfg, lg, eh := domain.FindingSecurity, domain.FindingConfiguration, domain.FindingLogging, domain.FindingErrorHandling
	obs, rel, q := domain.FindingObservability, domain.FindingReliability, domain.FindingQuality
	high, med, low := domain.FindingSeverityHigh, domain.FindingSeverityMedium, domain.FindingSeverityLow

	define(RuleHardcodedSecret, sec, high)
	define(RuleHardcodedCredential, sec, med)
	define(RuleCommittedEnvFile, sec, high)
	define(RuleTLSVerificationDisabled, sec, high)
	define(RuleCleartextTrafficAllowed, sec, med)
	define(RuleSQLInjectionRisk, sec, med)
	define(RuleDynamicCodeExecution, sec, med)
	define(RuleUnsafeHTMLInjection, sec, med)
	define(RulePermissiveCORS, sec, med)
	define(RuleInsecureTokenStorage, sec, med)
	define(RuleHTTPBodyLogging, sec, med)
	define(RuleErrorDetailsExposed, sec, med)
	define(RulePublicSourceMaps, sec, low)

	define(RuleDebugModeInProduction, cfg, high)
	define(RuleDebugLogLevelProduction, cfg, high)
	define(RuleDebuggableReleaseBuild, cfg, high)
	define(RuleReleaseMinifyDisabled, cfg, low)

	define(RuleUnconditionalDebugLog, lg, med)
	define(RuleConsoleLogsInProduction, lg, med)
	define(RuleMobileDebugPrints, lg, med)
	define(RulePrintInsteadOfLogger, lg, low)
	define(RulePrintStackTrace, lg, low)
	define(RuleNoStructuredLogging, lg, med)
	define(RuleNoRequestLogging, lg, low)
	define(RuleCatchWithoutLogging, lg, low)

	define(RuleEmptyCatchBlock, eh, med)
	define(RuleBareExcept, eh, med)
	define(RuleNoErrorBoundary, eh, med)

	define(RuleNoCrashReporting, obs, med)
	define(RuleNoHealthEndpoint, obs, low)

	define(RuleHTTPClientWithoutTimeout, rel, low)
	define(RuleForceUnwrap, rel, low)
	define(RuleMutableDefaultArgument, rel, low)

	define(RuleDebugLeftovers, q, med)
	define(RuleOversizedSourceFile, q, low)
	define(RuleTypeScriptAnyOveruse, q, low)
	define(RuleTechDebtMarkers, q, low)
	define(RuleNoAutomatedTests, q, med)
	define(RuleNoLinterConfigured, q, low)
	define(RuleWildcardImport, q, low)
}

// Rules lists every rule, sorted by id.
func Rules() []Rule {
	out := make([]Rule, 0, len(catalog))
	for _, r := range catalog {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func Lookup(id string) (Rule, bool) {
	r, ok := catalog[id]
	return r, ok
}
