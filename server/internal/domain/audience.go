package domain

import (
	"errors"
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
	StudyDraft        StudyStatus = "draft"
	StudyRunning      StudyStatus = "running"
	StudySynthesizing StudyStatus = "synthesizing"
	StudyDone         StudyStatus = "done"
	StudyFailed       StudyStatus = "failed"
	StudyCancelled    StudyStatus = "cancelled"
)

func (s StudyStatus) Terminal() bool {
	return s == StudyDone || s == StudyFailed || s == StudyCancelled
}

type Study struct {
	ID         uuid.UUID `json:"id"`
	AudienceID uuid.UUID `json:"audience_id"`
	Title      string    `json:"title"`
	Goal       string    `json:"goal,omitempty"`
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

type Report struct {
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
