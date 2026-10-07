package agent

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

func TruncateToolOutputForTest(content string, maxChars int) string {
	return truncateToolOutput(content, maxChars)
}

func ToolOutputTruncateMarkerForTest() string { return toolOutputTruncateSuffix }

func RepeatNudgeMessageForTest(name string, repeats int) string {
	return repeatNudgeMessage(name, repeats)
}

func RepeatAbortThresholdForTest() int { return repeatAbortThreshold }

func ErrorStreakMessageForTest(streak int) string { return errorStreakMessage(streak) }

func EmptyTurnPromptForTest() string { return emptyTurnPrompt }

func ErrStreakNoteThresholdForTest() int  { return errStreakNoteThreshold }
func ErrStreakAbortThresholdForTest() int { return errStreakAbortThreshold }
func ToolErrorNoteThresholdForTest() int  { return toolErrorNoteThreshold }

func ToolErrorMessageForTest(name string, count int) string { return toolErrorMessage(name, count) }

func BuildLLMRequestPayloadForTest(model string, history []domain.Message, toolCount int) map[string]any {
	return buildLLMRequestPayload(model, len(history), history, toolCount)
}

func OutputTruncatedMessageForTest(limit int) string { return outputTruncatedMessage(limit) }

type TokenCalibrationForTest struct {
	c *tokenCalibration
}

func NewTokenCalibrationForTest() *TokenCalibrationForTest {
	return &TokenCalibrationForTest{c: newTokenCalibration()}
}

func (t *TokenCalibrationForTest) Observe(real, estimated int) { t.c.observe(real, estimated) }

func (t *TokenCalibrationForTest) Current() float64 { return t.c.current() }

func CalibrationInitialRatioForTest() float64 { return calibrationInitialRatio }
func MinCalibrationRatioForTest() float64     { return minCalibrationRatio }
func MaxCalibrationRatioForTest() float64     { return maxCalibrationRatio }
