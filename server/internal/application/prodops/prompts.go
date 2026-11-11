package prodops

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// Zero-input keys for a fixed remedy sentence: prompt.Text renders them
// against struct{}{}.
var (
	rollbackStepGeneric = prompt.Define[struct{}]("briefs.prodops.remedy.rollback_step_generic", struct{}{})
	rollbackStepConfirm = prompt.Define[struct{}]("briefs.prodops.remedy.rollback_step_confirm", struct{}{})
	rollbackStepDiff     = prompt.Define[struct{}]("briefs.prodops.remedy.rollback_step_diff", struct{}{})
	rollbackStepAutoOff  = prompt.Define[struct{}]("briefs.prodops.remedy.rollback_step_auto_off", struct{}{})

	repeatStepFollowup = prompt.Define[struct{}]("briefs.prodops.remedy.repeat_step_followup", struct{}{})

	signatureConfigSummary = prompt.Define[struct{}]("briefs.prodops.remedy.signature_config_summary", struct{}{})
	signatureConfigStep1   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_config_step1", struct{}{})
	signatureConfigStep2   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_config_step2", struct{}{})
	signatureConfigStep3   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_config_step3", struct{}{})

	signatureDependencySummary = prompt.Define[struct{}]("briefs.prodops.remedy.signature_dependency_summary", struct{}{})
	signatureDependencyStep1   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_dependency_step1", struct{}{})
	signatureDependencyStep2   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_dependency_step2", struct{}{})
	signatureDependencyStep3   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_dependency_step3", struct{}{})

	signatureCapacitySummary = prompt.Define[struct{}]("briefs.prodops.remedy.signature_capacity_summary", struct{}{})
	signatureCapacityStep1   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_capacity_step1", struct{}{})
	signatureCapacityStep2   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_capacity_step2", struct{}{})
	signatureCapacityStep3   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_capacity_step3", struct{}{})

	signatureCodeFixSummary = prompt.Define[struct{}]("briefs.prodops.remedy.signature_codefix_summary", struct{}{})
	signatureCodeFixStep1   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_codefix_step1", struct{}{})
	signatureCodeFixStep2   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_codefix_step2", struct{}{})
	signatureCodeFixStep3   = prompt.Define[struct{}]("briefs.prodops.remedy.signature_codefix_step3", struct{}{})

	fallbackSummary = prompt.Define[struct{}]("briefs.prodops.remedy.fallback_summary", struct{}{})
	fallbackStep1    = prompt.Define[struct{}]("briefs.prodops.remedy.fallback_step1", struct{}{})
	fallbackStep2    = prompt.Define[struct{}]("briefs.prodops.remedy.fallback_step2", struct{}{})
	fallbackStep3    = prompt.Define[struct{}]("briefs.prodops.remedy.fallback_step3", struct{}{})
)

type envGapInput struct{ Env, Gap string }

var (
	rollbackSummaryRecent = prompt.Define[envGapInput]("briefs.prodops.remedy.rollback_summary_recent", envGapInput{Env: "prod", Gap: "10 min"})
	rollbackSummaryFailed = prompt.Define[envGapInput]("briefs.prodops.remedy.rollback_summary_failed", envGapInput{Env: "prod", Gap: "10 min"})
)

type hintInput struct{ Hint string }

var rollbackStepHint = prompt.Define[hintInput]("briefs.prodops.remedy.rollback_step_hint", hintInput{Hint: "gcloud run services update-traffic api --to-revisions PREV=100"})

type statusGapInput struct{ Status, Gap string }

var rollbackEvidenceDeploy = prompt.Define[statusGapInput]("briefs.prodops.remedy.rollback_evidence_deploy", statusGapInput{Status: "success", Gap: "10 min"})

type taskKeyInput struct{ TaskKey string }

var rollbackEvidenceTask = prompt.Define[taskKeyInput]("briefs.prodops.remedy.rollback_evidence_task", taskKeyInput{TaskKey: "TT-42"})

type whenInput struct{ When string }

var repeatSummary = prompt.Define[whenInput]("briefs.prodops.remedy.repeat_summary", whenInput{When: "2026-07-25"})

type fixInput struct{ Fix string }

var repeatStepFix = prompt.Define[fixInput]("briefs.prodops.remedy.repeat_step_fix", fixInput{Fix: "Scaled the consumer to 4 replicas."})

type whenOccurrencesInput struct {
	When        string
	Occurrences int
}

var repeatEvidence = prompt.Define[whenOccurrencesInput]("briefs.prodops.remedy.repeat_evidence", whenOccurrencesInput{When: "2026-07-25", Occurrences: 3})

type keywordInput struct{ Keyword string }

var signatureEvidence = prompt.Define[keywordInput]("briefs.prodops.remedy.signature_evidence", keywordInput{Keyword: "connection refused"})

type urlInput struct{ URL string }

var fallbackStepProbe = prompt.Define[urlInput]("briefs.prodops.remedy.fallback_step_probe", urlInput{URL: "https://api.example.com/health"})

type occurrencesSeveritySourceInput struct {
	Occurrences int
	Severity    string
	Source      string
}

var fallbackEvidence = prompt.Define[occurrencesSeveritySourceInput]("briefs.prodops.remedy.fallback_evidence", occurrencesSeveritySourceInput{
	Occurrences: 2, Severity: "high", Source: "probe",
})

type remediationTaskInput struct {
	Env         string
	Severity    string
	Source      string
	Occurrences int
	Title       string
	Detail      string
	RemedyKind  string
	Confidence  int
	RemedyText  string
	IncidentID  string
	AutoFix     bool
}

var remediationTaskKey = prompt.Define[remediationTaskInput]("briefs.prodops.remediation_task", remediationTaskInput{
	Env: "prod", Severity: "critical", Source: "probe", Occurrences: 1,
	Title: "prod health check failing", RemedyKind: "unknown", Confidence: 25,
	RemedyText: "No known signature matched.", IncidentID: "00000000-0000-0000-0000-000000000000",
})

type telemetryIncidentTaskInput struct {
	Env, Severity, Title, Detail, Service, Endpoint, TelemetryID, IncidentID, RemedyText string
}

var telemetryIncidentTaskKey = prompt.Define("briefs.prodops.telemetry_incident_task", telemetryIncidentTaskInput{
	Env: "prod", Severity: "critical", Title: "High error rate", Service: "orders-api", Endpoint: "POST /orders",
	TelemetryID: "42", IncidentID: "00000000-0000-0000-0000-000000000000", RemedyText: "No known signature matched.",
})

var incidentRecoveredCommentKey = prompt.Define[struct{}]("briefs.prodops.incident_recovered_comment", struct{}{})

type releaseAttributionNoteInput struct {
	TaskKey, Title, MergeSHA, Env, Gap, Window string
	AutoRollback                               bool
}

var releaseAttributionNoteKey = prompt.Define("briefs.prodops.release_attribution_note", releaseAttributionNoteInput{
	TaskKey: "TT-42", Title: "Add the export", MergeSHA: "abc1234", Env: "prod", Gap: "5m", Window: "30m",
})

type releaseAttributionCommentInput struct{ Note, Title, Env, Severity, Detail string }

var releaseAttributionCommentKey = prompt.Define("briefs.prodops.release_attribution_comment", releaseAttributionCommentInput{
	Note: "x", Title: "y", Env: "prod", Severity: "high", Detail: "z",
})
