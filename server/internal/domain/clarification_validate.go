package domain

import "fmt"

// ClarificationRuleCode names which clarification-question rule failed.
// Application renders the actual guidance text from this code (and the
// error's other fields) via catalog/system/guards/clarification_*.md — domain
// carries no authored prose so it stays importable without the prompt
// library's dependency.
type ClarificationRuleCode string

const (
	ClarificationRuleTooFewOptions        ClarificationRuleCode = "too_few_options"
	ClarificationRuleTextModeExtraOption  ClarificationRuleCode = "text_mode_extra_option"
	ClarificationRuleChoiceMissingOther   ClarificationRuleCode = "choice_missing_other"
	ClarificationRuleChoiceReservedOption ClarificationRuleCode = "choice_reserved_option"
	ClarificationRuleChoiceNeedsConcrete  ClarificationRuleCode = "choice_needs_concrete_option"
)

// ClarificationRuleError is a structured clarification-question validation
// failure. OptionID is set only for ClarificationRuleChoiceReservedOption.
type ClarificationRuleError struct {
	QuestionID string
	OptionID   string
	Code       ClarificationRuleCode
}

// Error is a non-prose fallback for callers that just log or compare
// err != nil; the model-facing wording is rendered by the consumer that
// catches this type from its Code and fields.
func (e *ClarificationRuleError) Error() string {
	if e.OptionID != "" {
		return fmt.Sprintf("clarification question %s: rule %s (option %s)", e.QuestionID, e.Code, e.OptionID)
	}
	return fmt.Sprintf("clarification question %s: rule %s", e.QuestionID, e.Code)
}

func NormalizeClarificationQuestions(questions []ClarificationQuestion) []ClarificationQuestion {
	out := make([]ClarificationQuestion, len(questions))
	for i, q := range questions {
		out[i] = NormalizeClarificationQuestion(q)
	}
	return out
}

func NormalizeClarificationQuestion(q ClarificationQuestion) ClarificationQuestion {
	if q.MaxSelections < 0 || !q.AllowMultiple || IsTextModeQuestion(q) {
		q.MaxSelections = 0
	}
	if IsTextModeQuestion(q) {
		return q
	}
	for _, opt := range q.Options {
		if opt.ID == "other" {
			return q
		}
	}
	q.Options = append(q.Options, ClarificationOption{ID: "other", Label: "Other"})
	return q
}

func ValidateClarificationQuestions(questions []ClarificationQuestion) error {
	for _, q := range questions {
		if err := ValidateClarificationQuestion(q); err != nil {
			return err
		}
	}
	return nil
}

func ValidateClarificationQuestion(q ClarificationQuestion) error {
	if len(q.Options) < 2 {
		return &ClarificationRuleError{QuestionID: q.ID, Code: ClarificationRuleTooFewOptions}
	}
	if IsTextModeQuestion(q) {
		for _, opt := range q.Options {
			if opt.ID != "free_text" && opt.ID != "skip" {
				return &ClarificationRuleError{QuestionID: q.ID, Code: ClarificationRuleTextModeExtraOption}
			}
		}
		return nil
	}
	last := q.Options[len(q.Options)-1]
	if last.ID != "other" {
		return &ClarificationRuleError{QuestionID: q.ID, Code: ClarificationRuleChoiceMissingOther}
	}
	concrete := 0
	for i, opt := range q.Options {
		if i == len(q.Options)-1 {
			continue
		}
		if opt.ID == "free_text" || opt.ID == "skip" || opt.ID == "other" {
			return &ClarificationRuleError{QuestionID: q.ID, OptionID: opt.ID, Code: ClarificationRuleChoiceReservedOption}
		}
		concrete++
	}
	if concrete < 1 {
		return &ClarificationRuleError{QuestionID: q.ID, Code: ClarificationRuleChoiceNeedsConcrete}
	}
	return nil
}

func IsTextModeQuestion(q ClarificationQuestion) bool {
	for _, opt := range q.Options {
		if opt.ID == "free_text" {
			return true
		}
	}
	return false
}
