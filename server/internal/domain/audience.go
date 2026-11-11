package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Audience research: synthetic personas grounded in a project, studies that
// ask them the same questions, and the reports those produce. The people are
// invented by the user's own LLM; everything here says so.

var (
	ErrAudienceNotFound = errors.New("audience not found")
	ErrStudyNotFound    = errors.New("study not found")
	ErrPersonaNotFound  = errors.New("persona not found")
	// ErrAudienceInvalid wraps a refusal the person can act on (a missing
	// brief, an audience that is not ready, a study that already ran).
	ErrAudienceInvalid = errors.New("invalid audience request")
)

type AudienceStatus string

const (
	AudienceDraft    AudienceStatus = "draft"
	AudienceBuilding AudienceStatus = "building"
	AudienceReady    AudienceStatus = "ready"
	AudienceFailed   AudienceStatus = "failed"
)

type AudienceSegment struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Count is how many of the audience's people belong to the segment; the
	// counts of all segments are scaled to the audience's size.
	Count int `json:"count,omitempty"`
}

// AudienceBrief is what the personas are written from. The product's state is
// the honest part: people judge what exists, not what is planned.
type AudienceBrief struct {
	Product          string            `json:"product"`
	Segments         []AudienceSegment `json:"segments,omitempty"`
	Locations        string            `json:"locations,omitempty"`
	CurrentSolutions string            `json:"current_solutions,omitempty"`
	// Pricing is the current or planned price, as the owner states it.
	Pricing string `json:"pricing,omitempty"`
	// Topic is what the research is for ("would they pay for this").
	Topic string `json:"topic,omitempty"`
	// Language is the language the personas speak in; empty is English.
	Language string `json:"language,omitempty"`
}

type Audience struct {
	ID             uuid.UUID      `json:"id"`
	Name           string         `json:"name"`
	RepositoryID   *uuid.UUID     `json:"repository_id,omitempty"`
	Status         AudienceStatus `json:"status"`
	Brief          AudienceBrief  `json:"brief"`
	PersonaTarget  int            `json:"persona_target"`
	ProductContext string         `json:"-"`
	Generation     int            `json:"generation"`
	Error          string         `json:"error,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	BuiltAt        *time.Time     `json:"built_at,omitempty"`

	// PersonaCount and StudyCount are filled by listings.
	PersonaCount int `json:"persona_count"`
	StudyCount   int `json:"study_count"`
}

// PersonaProfile is the invented person. Everything but the name is free text
// the LLM wrote, bounded on the way in.
type PersonaProfile struct {
	Age             int      `json:"age,omitempty"`
	Role            string   `json:"role,omitempty"`
	Location        string   `json:"location,omitempty"`
	Bio             string   `json:"bio,omitempty"`
	Goals           []string `json:"goals,omitempty"`
	Pains           []string `json:"pains,omitempty"`
	CurrentSolution string   `json:"current_solution,omitempty"`
	// Budget is how they feel about spending: tight, moderate, comfortable.
	Budget        string `json:"budget,omitempty"`
	TechSavviness string `json:"tech_savviness,omitempty"`
	Personality   string `json:"personality,omitempty"`
	Quote         string `json:"quote,omitempty"`
}

type Persona struct {
	ID         uuid.UUID `json:"id"`
	AudienceID uuid.UUID `json:"audience_id"`
	Ordinal    int       `json:"ordinal"`
	Generation int       `json:"generation"`
	Name       string    `json:"name"`
	Segment    string    `json:"segment,omitempty"`
	PersonaProfile
	CreatedAt time.Time `json:"created_at"`
}

type StudyStatus string

const (
	StudyDraft   StudyStatus = "draft"
	StudyRunning StudyStatus = "running"
	// StudyInterviewed: every interview is over and the report has not been
	// asked for yet (the person generates it, or retries the failed ones).
	StudyInterviewed  StudyStatus = "interviewed"
	StudySynthesizing StudyStatus = "synthesizing"
	StudyDone         StudyStatus = "done"
	StudyFailed       StudyStatus = "failed"
	StudyCancelled    StudyStatus = "cancelled"
)

func (s StudyStatus) Terminal() bool {
	return s == StudyDone || s == StudyFailed || s == StudyCancelled
}

// StudyKind is the type of a study: what its people are shown and asked.
type StudyKind string

const (
	// KindUserInterviews is the original study: would you buy the product as it
	// is, at what price. The default, so a study without a kind keeps its meaning.
	KindUserInterviews  StudyKind = "user_interviews"
	KindLandingPageTest StudyKind = "landing_page_test"
	KindMessageTesting  StudyKind = "message_testing"
	KindABTest          StudyKind = "ab_test"
)

var StudyKinds = []StudyKind{KindUserInterviews, KindLandingPageTest, KindMessageTesting, KindABTest}

// ParseStudyKind reads a kind; empty is user_interviews.
func ParseStudyKind(s string) (StudyKind, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return KindUserInterviews, true
	}
	for _, k := range StudyKinds {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// PageInput is a page a study shows its people: the text itself, which is read
// from the URL when only a URL was given.
type PageInput struct {
	URL  string `json:"url,omitempty"`
	Copy string `json:"copy,omitempty"`
}

// StudyInputs is what a kind needs besides the goal. landing_page_test: Intent
// and one page. message_testing: 2-5 Variants. ab_test: exactly two pages.
type StudyInputs struct {
	Intent   string      `json:"intent,omitempty"`
	Pages    []PageInput `json:"pages,omitempty"`
	Variants []string    `json:"variants,omitempty"`
}

// ScriptState says whether the interview script exists. none is a study that
// was planned without one (it asks only the kind's built-in questions plus its
// extra questions).
type ScriptState string

const (
	ScriptNone       ScriptState = "none"
	ScriptGenerating ScriptState = "generating"
	// ScriptAwaitingApproval: the script exists and the person has not said
	// OK to it; nothing runs. Every change of the script lands here again.
	ScriptAwaitingApproval ScriptState = "awaiting_approval"
	// ScriptApproved: the person approved the current version of the script.
	ScriptApproved ScriptState = "approved"
	ScriptFailed     ScriptState = "failed"
)

type ScriptSection struct {
	Title     string   `json:"title"`
	Questions []string `json:"questions"`
}

// StudyScript is the open questions of a study, grouped. The kind's built-in
// questions (the ones the report's numbers come from) are always asked and are
// not part of it.
type StudyScript struct {
	Sections []ScriptSection `json:"sections"`
}

// Flat is every question of the script, in order.
func (s *StudyScript) Flat() []string {
	out := []string{}
	if s == nil {
		return out
	}
	for _, sec := range s.Sections {
		out = append(out, sec.Questions...)
	}
	return out
}

type Study struct {
	ID         uuid.UUID   `json:"id"`
	AudienceID uuid.UUID   `json:"audience_id"`
	Title      string      `json:"title"`
	Goal       string      `json:"goal,omitempty"`
	Kind       StudyKind   `json:"kind"`
	Inputs     StudyInputs `json:"inputs"`
	// Script is the interview script; Questions is its flat list (what the
	// people are asked on top of the kind's built-in questions).
	Script      *StudyScript `json:"script,omitempty"`
	ScriptState ScriptState  `json:"script_state"`
	// ScriptCalls counts the model calls spent writing the script.
	ScriptCalls int `json:"script_calls"`
	// ScriptVersion counts the changes of the script; an approval is for one
	// version, and any change makes it stale.
	ScriptVersion         int        `json:"script_version"`
	ScriptApprovedVersion int        `json:"script_approved_version"`
	ScriptApprovedAt      *time.Time `json:"script_approved_at,omitempty"`
	ScriptApprovedBy      string     `json:"script_approved_by,omitempty"`
	// Questions are extra open questions asked on top of the built-in
	// purchase-intent core.
	Questions       []string    `json:"questions"`
	PricePoints     []float64   `json:"price_points"`
	Currency        string      `json:"currency"`
	ProductSnapshot string      `json:"-"`
	Status          StudyStatus `json:"status"`
	PersonaTotal    int         `json:"persona_total"`
	PersonaDone     int         `json:"persona_done"`
	PersonaFailed   int         `json:"persona_failed"`
	CallsEstimate   int         `json:"calls_estimate"`
	Error           string      `json:"error,omitempty"`
	Report          *Report     `json:"report,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	StartedAt       *time.Time  `json:"started_at,omitempty"`
	FinishedAt      *time.Time  `json:"finished_at,omitempty"`

	// AudienceName is filled by listings.
	AudienceName string `json:"audience_name,omitempty"`
}

// PriceReaction is one persona's reaction to one price point.
type PriceReaction struct {
	Price    float64 `json:"price"`
	WouldBuy bool    `json:"would_buy"`
	Comment  string  `json:"comment,omitempty"`
}

type QuestionAnswer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// StudyAnswers is what one persona said, in the structured shape the report
// is computed from.
type StudyAnswers struct {
	// IntentScore is 0 (never) to 10 (certainly) for the product as it is today.
	IntentScore    int              `json:"intent_score"`
	Verdict        string           `json:"verdict"` // yes | maybe | no
	WouldPayNow    bool             `json:"would_pay_now"`
	Reasoning      string           `json:"reasoning,omitempty"`
	MaxPrice       *float64         `json:"max_price,omitempty"`
	PriceReactions []PriceReaction  `json:"price_reactions,omitempty"`
	Missing        []string         `json:"missing,omitempty"`
	Objections     []string         `json:"objections,omitempty"`
	Alternatives   []string         `json:"alternatives,omitempty"`
	Quote          string           `json:"quote,omitempty"`
	Answers        []QuestionAnswer `json:"answers,omitempty"`

	// landing_page_test
	Landing *LandingAnswer `json:"landing,omitempty"`
	// message_testing: a rating per candidate line (Variant is 1-based, in
	// the order the study lists them), the line this person chose and the
	// stronger line they would write.
	Ratings     []VariantRating `json:"ratings,omitempty"`
	BestVariant int             `json:"best_variant,omitempty"`
	Stronger    string          `json:"stronger,omitempty"`
	// ab_test: this person scored each version on its own (A and B), then
	// said which they prefer ("a" or "b"); ShownFirst is the version they saw
	// first (the order is balanced across people).
	PageA      *PageAssessment `json:"page_a,omitempty"`
	PageB      *PageAssessment `json:"page_b,omitempty"`
	Preferred  string          `json:"preferred,omitempty"`
	ShownFirst string          `json:"shown_first,omitempty"`
}

// PageAssessment is one version's scorecard in an A/B test: the same
// questions a landing page test asks, answered for that version alone.
type PageAssessment struct {
	FirstImpression string   `json:"first_impression,omitempty"`
	ClarityScore    int      `json:"clarity_score"`
	ActScore        int      `json:"act_score"`
	Verdict         string   `json:"verdict"` // yes | maybe | no
	Objections      []string `json:"objections,omitempty"`
	Missing         []string `json:"missing,omitempty"`
	TrustSignals    []string `json:"trust_signals,omitempty"`
	Change          string   `json:"change,omitempty"`
}

// LandingAnswer is one visitor's reading of a landing page.
type LandingAnswer struct {
	FirstImpression string   `json:"first_impression,omitempty"`
	ClarityScore    int      `json:"clarity_score"`
	TrustSignals    []string `json:"trust_signals,omitempty"`
	Change          string   `json:"change,omitempty"`
}

type VariantRating struct {
	Variant     int    `json:"variant"`
	Clarity     int    `json:"clarity"`
	Relevance   int    `json:"relevance"`
	Credibility int    `json:"credibility"`
	Appeal      int    `json:"appeal"`
	Comment     string `json:"comment,omitempty"`
}

// PersonaSnapshot is who answered, as they were when they answered.
type PersonaSnapshot struct {
	// Ordinal is the person's place in the audience when they answered; it
	// keeps a study's answers in one order however the interviews finished.
	Ordinal int    `json:"ordinal,omitempty"`
	Name    string `json:"name"`
	Segment string `json:"segment,omitempty"`
	PersonaProfile
}

type StudyAnswer struct {
	ID        uuid.UUID       `json:"id"`
	StudyID   uuid.UUID       `json:"study_id"`
	PersonaID *uuid.UUID      `json:"persona_id,omitempty"`
	Persona   PersonaSnapshot `json:"persona"`
	Status    string          `json:"status"` // ok | failed
	Answers   StudyAnswers    `json:"answers"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// ChatMessage is one turn of a conversation with a persona. StudyID and GroupID
// are set on a follow-up question to a study's people.
type ChatMessage struct {
	ID        uuid.UUID  `json:"id"`
	PersonaID uuid.UUID  `json:"persona_id"`
	StudyID   *uuid.UUID `json:"study_id,omitempty"`
	GroupID   *uuid.UUID `json:"group_id,omitempty"`
	Role      string     `json:"role"` // user | persona
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
}

// ---- the report

type ReportVerdict struct {
	// Label is strong, promising, weak or poor: computed from the numbers,
	// never chosen by the LLM.
	Label string `json:"label"`
	// Score is the purchase-intent index, 0-100 (mean intent score x 10).
	Score     float64 `json:"score"`
	Rationale string  `json:"rationale,omitempty"`
}

type Bucket struct {
	From  float64 `json:"from"`
	To    float64 `json:"to"`
	Count int     `json:"count"`
}

type PriceTest struct {
	Price  float64 `json:"price"`
	BuyPct float64 `json:"buy_pct"`
	Asked  int     `json:"asked"`
}

type PriceStats struct {
	Currency string `json:"currency"`
	Answered int    `json:"answered"`
	// NotWilling counts the people who would pay nothing.
	NotWilling int     `json:"not_willing"`
	Min        float64 `json:"min"`
	P25        float64 `json:"p25"`
	Median     float64 `json:"median"`
	Mean       float64 `json:"mean"`
	P75        float64 `json:"p75"`
	Max        float64 `json:"max"`
	// Histogram buckets the stated maximum prices.
	Histogram []Bucket    `json:"histogram"`
	Tests     []PriceTest `json:"tests,omitempty"`
}

type ReportMetrics struct {
	Respondents int `json:"respondents"`
	Failed      int `json:"failed"`
	// IntentDistribution[i] counts the personas who scored i (0..10).
	IntentDistribution []int      `json:"intent_distribution"`
	IntentMean         float64    `json:"intent_mean"`
	Yes                int        `json:"yes"`
	Maybe              int        `json:"maybe"`
	No                 int        `json:"no"`
	WouldPayNowPct     float64    `json:"would_pay_now_pct"`
	Price              PriceStats `json:"price"`
}

type SegmentReport struct {
	Name        string  `json:"name"`
	Respondents int     `json:"respondents"`
	IntentMean  float64 `json:"intent_mean"`
	YesPct      float64 `json:"yes_pct"`
	MedianPrice float64 `json:"median_price"`
	Insight     string  `json:"insight,omitempty"`
}

// Theme is a recurring thing people said; the count is computed from the
// personas the synthesis assigned to it, not stated by the LLM.
type Theme struct {
	Label    string   `json:"label"`
	Count    int      `json:"count"`
	Pct      float64  `json:"pct"`
	Personas []string `json:"personas,omitempty"`
}

type ReportQuote struct {
	// Version is the page a quote is about (A or B) in an ab_test.
	Version   string `json:"version,omitempty"`
	Persona   string `json:"persona"`
	Segment   string `json:"segment,omitempty"`
	Role      string `json:"role,omitempty"`
	Text      string `json:"text"`
	Topic     string `json:"topic,omitempty"`
	Sentiment string `json:"sentiment,omitempty"` // positive | negative | neutral
}

type Recommendation struct {
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Priority string `json:"priority,omitempty"` // high | medium | low
}

// NamedScore is a number for one segment.
type NamedScore struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type LandingReport struct {
	// Intent is what the visitors were asked to come with.
	Intent      string       `json:"intent,omitempty"`
	Source      string       `json:"source,omitempty"`
	ClarityMean float64      `json:"clarity_mean"`
	ClarityBy   []NamedScore `json:"clarity_by_segment,omitempty"`
}

type VariantReport struct {
	Variant     int          `json:"variant"`
	Text        string       `json:"text"`
	Rank        int          `json:"rank"`
	Clarity     float64      `json:"clarity"`
	Relevance   float64      `json:"relevance"`
	Credibility float64      `json:"credibility"`
	Appeal      float64      `json:"appeal"`
	Overall     float64      `json:"overall"`
	BestPct     float64      `json:"best_pct"`
	BySegment   []NamedScore `json:"by_segment,omitempty"`
}

type MessageReport struct {
	Variants []VariantReport `json:"variants"`
	// Winner is the 1-based number of the top-ranked line.
	Winner int `json:"winner"`
	// Rewrites are the stronger lines people wrote, in their words.
	Rewrites []ReportQuote `json:"rewrites,omitempty"`
}

// ABSide is one version of an A/B test. Wins and WinPct count the people who
// preferred it; MeanScore is its mean act-now score over everyone; Issues are
// the objections people raised against it; Reasons why people preferred it.
type ABSide struct {
	// ActPct is the share of people who would take the next step on this version.
	ActPct    float64       `json:"act_pct"`
	Quotes    []ReportQuote `json:"quotes"`
	Label     string        `json:"label"` // A | B
	Source    string        `json:"source,omitempty"`
	Wins      int           `json:"wins"`
	WinPct    float64       `json:"win_pct"`
	MeanScore float64       `json:"mean_score"`
	BySegment []NamedScore  `json:"win_pct_by_segment,omitempty"`
	Issues    []Theme       `json:"issues,omitempty"`
	Reasons   []Theme       `json:"reasons,omitempty"`
}

// ArmMetric is a score's mean and median over everyone.
type ArmMetric struct {
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
}

// ABMetric compares one scorecard question across the two versions, over all
// the people who answered, who scored both. Diff is B's mean minus A's. The
// paired fields count people by which version they scored higher, and
// EffectSize is Cohen's d on each person's own B-minus-A difference: it is a
// direction and a size, never a p-value.
type ABMetric struct {
	A           ArmMetric `json:"a"`
	B           ArmMetric `json:"b"`
	Diff        float64   `json:"diff"`
	AHigher     int       `json:"a_higher"`
	BHigher     int       `json:"b_higher"`
	Same        int       `json:"same"`
	EffectSize  float64   `json:"effect_size"`
	EffectLabel string    `json:"effect_label"` // negligible | small | medium | large
}

type ABReport struct {
	Sides [2]ABSide `json:"sides"`
	// Act and Clarity compare the two scorecards over everyone.
	Act     ABMetric `json:"act"`
	Clarity ABMetric `json:"clarity"`
	// Winner is "A", "B" or "tie".
	Winner string `json:"winner"`
	// Confidence is clear, lean or toss-up: computed from the margin and how
	// many people answered, never chosen by the LLM.
	Confidence string `json:"confidence"`
	// FirstShownWinPct is how often the page shown first won; far from 50 means
	// position, not content, decided.
	FirstShownWinPct float64 `json:"first_shown_win_pct"`
}

type Report struct {
	Kind            StudyKind        `json:"kind,omitempty"`
	GeneratedAt     time.Time        `json:"generated_at"`
	Summary         string           `json:"summary"`
	Verdict         ReportVerdict    `json:"verdict"`
	Metrics         ReportMetrics    `json:"metrics"`
	Segments        []SegmentReport  `json:"segments"`
	Objections      []Theme          `json:"objections"`
	Needs           []Theme          `json:"needs"`
	Alternatives    []Theme          `json:"alternatives"`
	Quotes          []ReportQuote    `json:"quotes"`
	Recommendations []Recommendation `json:"recommendations"`
	// Strengths is what worked (landing_page_test, message_testing).
	Strengths []Theme        `json:"strengths,omitempty"`
	Landing   *LandingReport `json:"landing,omitempty"`
	Messages  *MessageReport `json:"messages,omitempty"`
	AB        *ABReport      `json:"ab,omitempty"`
	// OpenQuestions are the study's extra questions with a short digest of
	// what people answered.
	OpenQuestions []OpenQuestionDigest `json:"open_questions,omitempty"`
	Methodology   string               `json:"methodology"`
	Disclaimer    string               `json:"disclaimer"`
	// SynthesisFailed: the narrative could not be written; the numbers stand.
	SynthesisFailed bool `json:"synthesis_failed,omitempty"`
}

type OpenQuestionDigest struct {
	Question string `json:"question"`
	Digest   string `json:"digest"`
}
