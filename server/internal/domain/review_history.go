package domain

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	ReviewItemFinding     = "finding"
	ReviewItemHumanReview = "human_review"
	// A reviewer's own account of what it could not verify, and of the aspects
	// it did cover (Scope/Reason, and Message respectively).
	ReviewItemNotReviewed = "not_reviewed"
	ReviewItemAspect      = "aspect"

	ReviewFindingOpen      = "open"
	ReviewFindingWithdrawn = "withdrawn"
	ReviewFindingPassed    = "passed"

	// The lifecycle of a finding as the history reads it across rounds.
	ReviewStateOpen      = "open"
	ReviewStateFixed     = "fixed"
	ReviewStateWithdrawn = "withdrawn"
	ReviewStateReopened  = "reopened"

	// ReviewCategoryAcceptanceCriterion marks a finding that is a QA verdict
	// on one acceptance criterion rather than something a reviewer found.
	ReviewCategoryAcceptanceCriterion = "acceptance_criterion"

	ReviewFlagSourceAgent     = "agent"
	ReviewFlagSourceAutomatic = "automatic"

	// Codes of the flags the server derives itself. The wording is the UI's.
	ReviewFlagAgentUncertain    = "agent_uncertain"
	ReviewFlagSensitivePath     = "sensitive_path"
	ReviewFlagLargeDiff         = "large_diff"
	ReviewFlagUnrecordedChanges = "unrecorded_changes"
	ReviewFlagLoopGuard         = "loop_guard"
	ReviewFlagTestsMissing      = "tests_missing"
	ReviewFlagQANoEvidence      = "qa_no_evidence"
	ReviewFlagCoverageGap       = "coverage_gap"

	MaxReviewFindingsPerReport = 40
	MaxReviewFlagsPerReport    = 5
	MaxReviewNotReviewed       = 20
	MaxReviewAspects           = 8
	MaxReviewMessageChars      = 800
	MaxReviewReasonChars       = 300
	MaxReviewQuestionChars     = 300
)

var (
	ErrReviewReportInvalid = errors.New("invalid review report")
	ErrReviewFlagNotFound  = errors.New("review flag not found")
)

var reviewSeverities = []string{"critical", "high", "medium", "low", "info"}
var reviewConfidences = []string{"high", "medium", "low"}

func ReviewSeverities() []string  { return append([]string(nil), reviewSeverities...) }
func ReviewConfidences() []string { return append([]string(nil), reviewConfidences...) }

func inList(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// TaskReviewFinding is one stored row of what a reviewer reported.
type TaskReviewFinding struct {
	ID                uuid.UUID  `json:"id"`
	TaskID            uuid.UUID  `json:"task_id"`
	Kind              string     `json:"kind"`
	Gate              string     `json:"gate"`
	AgentID           *uuid.UUID `json:"agent_id,omitempty"`
	AgentName         string     `json:"agent_name"`
	HeadSHA           string     `json:"head_sha,omitempty"`
	Fingerprint       string     `json:"fingerprint"`
	File              string     `json:"file,omitempty"`
	Line              int        `json:"line,omitempty"`
	Severity          string     `json:"severity,omitempty"`
	Category          string     `json:"category,omitempty"`
	Message           string     `json:"message"`
	Confidence        string     `json:"confidence,omitempty"`
	HumanReviewReason string     `json:"human_review_reason,omitempty"`
	Question          string     `json:"question,omitempty"`
	// Scope and Reason are a not_reviewed item: what the reviewer could not
	// verify (a file, a directory, an aspect) and why.
	Scope       string     `json:"scope,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Status      string     `json:"status"`
	CriterionID *uuid.UUID `json:"criterion_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ReviewFlagAck is a person marking one flag as looked at.
type ReviewFlagAck struct {
	TaskID     uuid.UUID `json:"task_id"`
	FlagKey    string    `json:"flag_key"`
	ReviewedBy string    `json:"reviewed_by"`
	ReviewedAt time.Time `json:"reviewed_at"`
}

// ReviewFindingInput is one finding in a reviewer's report. Every field but
// Message is optional: Key lets the reviewer name the same problem the same
// way in a later round, Status "withdrawn" takes an earlier finding back.
type ReviewFindingInput struct {
	Key               string `json:"key,omitempty"`
	File              string `json:"file,omitempty"`
	Line              int    `json:"line,omitempty"`
	Severity          string `json:"severity,omitempty"`
	Category          string `json:"category,omitempty"`
	Message           string `json:"message"`
	Confidence        string `json:"confidence,omitempty"`
	HumanReviewReason string `json:"human_review_reason,omitempty"`
	Question          string `json:"question,omitempty"`
	Status            string `json:"status,omitempty"`
}

// HumanReviewInput is a place the reviewer wants a person to look that is not
// a defect it is reporting.
type HumanReviewInput struct {
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Reason   string `json:"reason"`
	Question string `json:"question,omitempty"`
}

// NotReviewedInput is something the reviewer could not verify.
type NotReviewedInput struct {
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

type ReviewReportInput struct {
	Findings         []ReviewFindingInput `json:"findings,omitempty"`
	NeedsHumanReview []HumanReviewInput   `json:"needs_human_review,omitempty"`
	NotReviewed      []NotReviewedInput   `json:"not_reviewed,omitempty"`
	ReviewedAspects  []string             `json:"reviewed_aspects,omitempty"`
}

func (r ReviewReportInput) flagCount() int {
	n := len(r.NeedsHumanReview)
	for _, f := range r.Findings {
		if strings.TrimSpace(f.HumanReviewReason) != "" {
			n++
		}
	}
	return n
}

func tooLong(s string, max int) bool { return utf8.RuneCountInString(s) > max }

// Validate normalises the report in place and rejects what the schema allows
// but the rules do not: no more than MaxReviewFlagsPerReport places for a
// person to look at, because a flag on everything flags nothing.
func (r *ReviewReportInput) Validate() error {
	if len(r.Findings) == 0 && len(r.NeedsHumanReview) == 0 && len(r.NotReviewed) == 0 && len(r.ReviewedAspects) == 0 {
		return fmt.Errorf("%w: pass findings, needs_human_review, not_reviewed or reviewed_aspects", ErrReviewReportInvalid)
	}
	if len(r.NotReviewed) > MaxReviewNotReviewed || len(r.ReviewedAspects) > MaxReviewAspects {
		return fmt.Errorf("%w: at most %d not_reviewed items and %d reviewed_aspects per report", ErrReviewReportInvalid, MaxReviewNotReviewed, MaxReviewAspects)
	}
	for i := range r.NotReviewed {
		n := &r.NotReviewed[i]
		n.Scope, n.Reason = normalizeReviewPath(n.Scope), strings.TrimSpace(n.Reason)
		if n.Scope == "" || n.Reason == "" || tooLong(n.Scope, 200) || tooLong(n.Reason, MaxReviewReasonChars) {
			return fmt.Errorf("%w: not_reviewed[%d] needs a scope and a one-sentence reason", ErrReviewReportInvalid, i)
		}
	}
	for i, a := range r.ReviewedAspects {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" || tooLong(a, 40) {
			return fmt.Errorf("%w: reviewed_aspects[%d] must be a short word or two", ErrReviewReportInvalid, i)
		}
		r.ReviewedAspects[i] = a
	}
	if len(r.Findings) > MaxReviewFindingsPerReport {
		return fmt.Errorf("%w: %d findings, the limit is %d per report", ErrReviewReportInvalid, len(r.Findings), MaxReviewFindingsPerReport)
	}
	if n := r.flagCount(); n > MaxReviewFlagsPerReport {
		return fmt.Errorf("%w: %d places flagged for a person, the limit is %d per report: keep the ones that are genuinely uncertain or high-stakes", ErrReviewReportInvalid, n, MaxReviewFlagsPerReport)
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		f.Key = strings.TrimSpace(f.Key)
		f.File = normalizeReviewPath(f.File)
		f.Message = strings.TrimSpace(f.Message)
		f.Category = strings.ToLower(strings.TrimSpace(f.Category))
		f.Severity = strings.ToLower(strings.TrimSpace(f.Severity))
		f.Confidence = strings.ToLower(strings.TrimSpace(f.Confidence))
		f.HumanReviewReason = strings.TrimSpace(f.HumanReviewReason)
		f.Question = strings.TrimSpace(f.Question)
		f.Status = strings.ToLower(strings.TrimSpace(f.Status))
		switch {
		case f.Message == "":
			return fmt.Errorf("%w: findings[%d].message is required", ErrReviewReportInvalid, i)
		case tooLong(f.Message, MaxReviewMessageChars):
			return fmt.Errorf("%w: findings[%d].message is over %d characters", ErrReviewReportInvalid, i, MaxReviewMessageChars)
		case f.Severity != "" && !inList(reviewSeverities, f.Severity):
			return fmt.Errorf("%w: findings[%d].severity %q is not one of %s", ErrReviewReportInvalid, i, f.Severity, strings.Join(reviewSeverities, ", "))
		case f.Confidence != "" && !inList(reviewConfidences, f.Confidence):
			return fmt.Errorf("%w: findings[%d].confidence %q is not one of %s", ErrReviewReportInvalid, i, f.Confidence, strings.Join(reviewConfidences, ", "))
		case f.Line < 0:
			return fmt.Errorf("%w: findings[%d].line is negative", ErrReviewReportInvalid, i)
		case f.Status != "" && f.Status != ReviewFindingOpen && f.Status != ReviewFindingWithdrawn:
			return fmt.Errorf("%w: findings[%d].status must be open or withdrawn", ErrReviewReportInvalid, i)
		case tooLong(f.HumanReviewReason, MaxReviewReasonChars) || tooLong(f.Question, MaxReviewQuestionChars):
			return fmt.Errorf("%w: findings[%d] human_review_reason/question is too long: one sentence", ErrReviewReportInvalid, i)
		}
		if f.Status == "" {
			f.Status = ReviewFindingOpen
		}
	}
	for i := range r.NeedsHumanReview {
		h := &r.NeedsHumanReview[i]
		h.File = normalizeReviewPath(h.File)
		h.Reason = strings.TrimSpace(h.Reason)
		h.Question = strings.TrimSpace(h.Question)
		switch {
		case h.Reason == "":
			return fmt.Errorf("%w: needs_human_review[%d].reason is required", ErrReviewReportInvalid, i)
		case tooLong(h.Reason, MaxReviewReasonChars) || tooLong(h.Question, MaxReviewQuestionChars):
			return fmt.Errorf("%w: needs_human_review[%d] reason/question is too long: one sentence", ErrReviewReportInvalid, i)
		case h.Line < 0:
			return fmt.Errorf("%w: needs_human_review[%d].line is negative", ErrReviewReportInvalid, i)
		}
	}
	return nil
}

func normalizeReviewPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	return strings.TrimPrefix(p, "./")
}

var (
	reviewDigits    = regexp.MustCompile(`\d+`)
	reviewSpaces    = regexp.MustCompile(`\s+`)
	reviewPunct     = regexp.MustCompile(`[^\p{L}\p{N} ]+`)
	reviewLineRefs  = regexp.MustCompile(`(?i)\b(line|lines|l)\s*\d+(\s*-\s*\d+)?\b|:\d+(-\d+)?\b`)
	reviewFlagPunct = regexp.MustCompile(`[^a-z0-9]+`)
)

// NormalizeReviewMessage is what two phrasings of the same finding share:
// lower case, no line numbers, no punctuation, single spaces.
func NormalizeReviewMessage(msg string) string {
	msg = strings.ToLower(reviewLineRefs.ReplaceAllString(msg, " "))
	msg = reviewDigits.ReplaceAllString(msg, "")
	msg = reviewPunct.ReplaceAllString(msg, " ")
	return strings.TrimSpace(reviewSpaces.ReplaceAllString(msg, " "))
}

// ReviewFingerprint is a finding's identity across rounds. A key the
// reviewer chose wins (it names the same problem on purpose); without one the
// file, the category and the normalised message stand in for it. The line
// is left out because the lines move when the code is fixed around them.
func ReviewFingerprint(gate, key, file, category, message string) string {
	var basis string
	if k := strings.TrimSpace(key); k != "" {
		basis = "key|" + gate + "|" + strings.ToLower(k)
	} else {
		basis = "msg|" + gate + "|" + strings.ToLower(normalizeReviewPath(file)) + "|" + strings.ToLower(strings.TrimSpace(category)) + "|" + NormalizeReviewMessage(message)
	}
	sum := sha1.Sum([]byte(basis))
	return hex.EncodeToString(sum[:])[:16]
}

// CriterionFingerprint identifies one acceptance criterion's QA verdict.
func CriterionFingerprint(criterionID uuid.UUID) string {
	sum := sha1.Sum([]byte("criterion|" + criterionID.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// FlagFingerprint identifies a place a reviewer flagged, so the same flag
// raised again in the next round is one flag.
func FlagFingerprint(file, reason string) string {
	sum := sha1.Sum([]byte("flag|" + strings.ToLower(normalizeReviewPath(file)) + "|" + NormalizeReviewMessage(reason)))
	return hex.EncodeToString(sum[:])[:16]
}

// ReviewFindingRows turns a validated report into the rows to store.
func (r ReviewReportInput) Rows(taskID uuid.UUID, gate string, agentID *uuid.UUID, agentName, headSHA string, now time.Time) []TaskReviewFinding {
	rows := make([]TaskReviewFinding, 0, len(r.Findings)+len(r.NeedsHumanReview)+len(r.NotReviewed)+len(r.ReviewedAspects))
	for _, f := range r.Findings {
		rows = append(rows, TaskReviewFinding{
			TaskID: taskID, Kind: ReviewItemFinding, Gate: gate, AgentID: agentID, AgentName: agentName,
			HeadSHA: headSHA, Fingerprint: ReviewFingerprint(gate, f.Key, f.File, f.Category, f.Message),
			File: f.File, Line: f.Line, Severity: f.Severity, Category: f.Category, Message: f.Message,
			Confidence: f.Confidence, HumanReviewReason: f.HumanReviewReason, Question: f.Question,
			Status: f.Status, CreatedAt: now,
		})
	}
	for _, h := range r.NeedsHumanReview {
		rows = append(rows, TaskReviewFinding{
			TaskID: taskID, Kind: ReviewItemHumanReview, Gate: gate, AgentID: agentID, AgentName: agentName,
			HeadSHA: headSHA, Fingerprint: FlagFingerprint(h.File, h.Reason),
			File: h.File, Line: h.Line, Message: h.Reason, HumanReviewReason: h.Reason, Question: h.Question,
			Status: ReviewFindingOpen, CreatedAt: now,
		})
	}
	for _, n := range r.NotReviewed {
		rows = append(rows, TaskReviewFinding{
			TaskID: taskID, Kind: ReviewItemNotReviewed, Gate: gate, AgentID: agentID, AgentName: agentName,
			HeadSHA: headSHA, Fingerprint: FlagFingerprint(n.Scope, n.Reason), Scope: n.Scope, Reason: n.Reason,
			Message: n.Scope, Status: ReviewFindingOpen, CreatedAt: now,
		})
	}
	for _, a := range r.ReviewedAspects {
		rows = append(rows, TaskReviewFinding{
			TaskID: taskID, Kind: ReviewItemAspect, Gate: gate, AgentID: agentID, AgentName: agentName,
			HeadSHA: headSHA, Fingerprint: FlagFingerprint("", a), Message: a, Status: ReviewFindingOpen, CreatedAt: now,
		})
	}
	return rows
}

// ---- what GET .../review-history returns ----

type ReviewHistorySummary struct {
	Rounds         int `json:"rounds"`
	FindingsRaised int `json:"findings_raised"`
	Fixed          int `json:"fixed"`
	Open           int `json:"open"`
	Withdrawn      int `json:"withdrawn"`
	Reopened       int `json:"reopened"`
	Bounces        int `json:"bounces"`
	LoopGuardStops int `json:"loop_guard_stops"`
	QAFailures     int `json:"qa_failures"`
	OpenFlags      int `json:"open_flags"`
}

type ReviewHistoryReviewer struct {
	AgentID   *uuid.UUID `json:"agent_id,omitempty"`
	AgentName string     `json:"agent_name,omitempty"`
	Human     bool       `json:"human,omitempty"`
	Verdict   string     `json:"verdict"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
	RunID     *uuid.UUID `json:"run_id,omitempty"`
}

type ReviewHistoryMove struct {
	At           time.Time  `json:"at"`
	From         string     `json:"from"`
	To           string     `json:"to"`
	Actor        string     `json:"actor"`
	AgentID      *uuid.UUID `json:"agent_id,omitempty"`
	AgentName    string     `json:"agent_name,omitempty"`
	SystemReason string     `json:"system_reason,omitempty"`
	Bounce       bool       `json:"bounce,omitempty"`
	LoopGuard    bool       `json:"loop_guard,omitempty"`
}

type ReviewHistoryRun struct {
	RunID        uuid.UUID  `json:"run_id"`
	AgentID      uuid.UUID  `json:"agent_id"`
	AgentName    string     `json:"agent_name,omitempty"`
	Status       string     `json:"status"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Summary      string     `json:"summary,omitempty"`
	BaseSHA      string     `json:"base_sha,omitempty"`
	HeadSHA      string     `json:"head_sha,omitempty"`
	FilesChanged int        `json:"files_changed,omitempty"`
	HasTrace     bool       `json:"has_trace"`
}

type ReviewHistoryTrailStep struct {
	Round int       `json:"round"`
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

type ReviewHistoryFinding struct {
	ID            string                   `json:"id"`
	Gate          string                   `json:"gate"`
	AgentID       *uuid.UUID               `json:"agent_id,omitempty"`
	AgentName     string                   `json:"agent_name,omitempty"`
	File          string                   `json:"file,omitempty"`
	Line          int                      `json:"line,omitempty"`
	FileURL       string                   `json:"file_url,omitempty"`
	Severity      string                   `json:"severity,omitempty"`
	Category      string                   `json:"category,omitempty"`
	Message       string                   `json:"message"`
	Confidence    string                   `json:"confidence,omitempty"`
	CriterionID   *uuid.UUID               `json:"criterion_id,omitempty"`
	CriterionText string                   `json:"criterion_text,omitempty"`
	Status        string                   `json:"status"`
	RaisedRound   int                      `json:"raised_round"`
	FixedRound    int                      `json:"fixed_round,omitempty"`
	Trail         []ReviewHistoryTrailStep `json:"trail"`
}

type ReviewHistoryRound struct {
	// Seq numbers rounds in the order they happened across gates; Number
	// counts within the gate ("code review round 2").
	Seq       int                     `json:"seq"`
	Number    int                     `json:"number"`
	Gate      string                  `json:"gate"`
	EnteredAt time.Time               `json:"entered_at"`
	LeftAt    *time.Time              `json:"left_at,omitempty"`
	Outcome   string                  `json:"outcome"`
	Reviewers []ReviewHistoryReviewer `json:"reviewers"`
	FromSHA   string                  `json:"from_sha,omitempty"`
	HeadSHA   string                  `json:"head_sha,omitempty"`
	Raised    []string                `json:"raised"`
	Fixed     []string                `json:"fixed"`
	StillOpen []string                `json:"still_open"`
	Reopened  []string                `json:"reopened"`
	Moves     []ReviewHistoryMove     `json:"moves"`
	FixRuns   []ReviewHistoryRun      `json:"fix_runs"`
	LoopGuard bool                    `json:"loop_guard,omitempty"`
	Coverage  *RoundCoverage          `json:"coverage,omitempty"`
}

// ReviewNotReviewed is one thing a reviewer did not verify: either the
// reviewer said so (Source "declared") or the records show it ("auto").
type ReviewNotReviewed struct {
	Scope   string   `json:"scope"`
	Reason  string   `json:"reason"`
	Kind    string   `json:"kind"`
	Source  string   `json:"source"`
	Count   int      `json:"count,omitempty"`
	Files   []string `json:"files,omitempty"`
	FileURL string   `json:"file_url,omitempty"`
}

// ReviewerCoverage is what one reviewing agent covered in one round.
type ReviewerCoverage struct {
	AgentID   *uuid.UUID `json:"agent_id,omitempty"`
	AgentName string     `json:"agent_name"`
	RunID     *uuid.UUID `json:"run_id,omitempty"`
	// Basis lists where the numbers come from: "trace" (the run's recorded
	// reads), "diff" (the diff the reviewer was handed) and "declared" (the
	// reviewer's own list).
	Basis   []string `json:"basis"`
	Aspects []string `json:"aspects"`
	// Files are counted against the round's changed files; the lines of the
	// files the reviewer covered against all changed lines.
	FilesTotal    int `json:"files_total"`
	FilesReviewed int `json:"files_reviewed"`
	FilesOpened   int `json:"files_opened"`
	LinesTotal    int `json:"lines_total"`
	LinesReviewed int `json:"lines_reviewed"`
	// DiffKnown is false when the diff the reviewer was handed could not be
	// reconstructed (an earlier round, or no checkout here): its coverage is
	// then the files it opened and nothing more.
	DiffKnown       bool                `json:"diff_known"`
	CriteriaTotal   int                 `json:"criteria_total,omitempty"`
	CriteriaChecked int                 `json:"criteria_checked,omitempty"`
	UnreadFiles     []string            `json:"unread_files"`
	UnreadCount     int                 `json:"unread_count"`
	NotReviewed     []ReviewNotReviewed `json:"not_reviewed"`
}

type UncoveredFile struct {
	Path    string `json:"path"`
	Lines   int    `json:"lines,omitempty"`
	FileURL string `json:"file_url,omitempty"`
}

// RoundCoverage is the coverage of every reviewer of one round and what none
// of them covered.
type RoundCoverage struct {
	ChangedFiles   int                `json:"changed_files"`
	ChangedLines   int                `json:"changed_lines"`
	DiffTruncated  bool               `json:"diff_truncated,omitempty"`
	Reviewers      []ReviewerCoverage `json:"reviewers"`
	NobodyReviewed []UncoveredFile    `json:"nobody_reviewed"`
}

type ReviewFlag struct {
	ID         string     `json:"id"`
	Source     string     `json:"source"`
	Code       string     `json:"code"`
	Reason     string     `json:"reason,omitempty"`
	Question   string     `json:"question,omitempty"`
	File       string     `json:"file,omitempty"`
	Line       int        `json:"line,omitempty"`
	FileURL    string     `json:"file_url,omitempty"`
	Files      []string   `json:"files,omitempty"`
	Count      int        `json:"count,omitempty"`
	Gate       string     `json:"gate,omitempty"`
	AgentID    *uuid.UUID `json:"agent_id,omitempty"`
	AgentName  string     `json:"agent_name,omitempty"`
	RaisedAt   time.Time  `json:"raised_at"`
	Round      int        `json:"round,omitempty"`
	ReviewedBy string     `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
}

func (f ReviewFlag) Reviewed() bool { return f.ReviewedAt != nil }

type ReviewHistory struct {
	TaskID   uuid.UUID              `json:"task_id"`
	Summary  ReviewHistorySummary   `json:"summary"`
	Rounds   []ReviewHistoryRound   `json:"rounds"`
	Findings []ReviewHistoryFinding `json:"findings"`
	Flags    []ReviewFlag           `json:"flags"`
}

// ReviewFileURL is the link to a file (and line) in a GitHub pull request's
// diff, or "" when the task has no such pull request. GitHub names a file's
// anchor in the files view after the sha256 of its path.
func ReviewFileURL(prURL, file string, line int) string {
	file = normalizeReviewPath(file)
	if file == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(prURL))
	if err != nil || u.Host != "github.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return ""
	}
	sum := sha256.Sum256([]byte(file))
	link := fmt.Sprintf("https://github.com/%s/%s/pull/%s/files#diff-%s", parts[0], parts[1], parts[3], hex.EncodeToString(sum[:]))
	if line > 0 {
		link += fmt.Sprintf("R%d", line)
	}
	return link
}

// ReviewFlagKey builds a stable, URL-safe key from its parts.
func ReviewFlagKey(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(reviewFlagPunct.ReplaceAllString(strings.ToLower(p), "_"), "_")
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "-")
}

// ShortHash is a short stable digest of a sorted list of strings.
func ShortHash(items []string) string {
	sorted := append([]string(nil), items...)
	sort.Strings(sorted)
	sum := sha1.Sum([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(sum[:])[:10]
}
