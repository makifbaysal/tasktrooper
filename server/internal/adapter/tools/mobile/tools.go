package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// NewExecutors builds the mobile_* tool set. A session that no operator
// configured yields no tools at all rather than tools that always fail: an
// agent shown mobile_tap will try to use it, and "no device configured" spent
// as a tool call is a turn the run does not get back.
func NewExecutors(session device, targets AppResolver) []port.ToolExecutor {
	if session == nil || !session.Configured() {
		return nil
	}
	return NewExecutorsFor(session, targets)
}

// NewExecutorsFor builds the tool set without asking whether a device is
// attached yet.
//
// The runtime uses it because registration and attachment came apart: the
// session now exists from boot so a phone registered in the settings UI can be
// swapped in without a restart, and the caller — not this constructor — decides
// when the tools become visible to agents.
func NewExecutorsFor(session device, targets AppResolver) []port.ToolExecutor {
	if session == nil {
		return nil
	}
	return []port.ToolExecutor{
		newLaunchTool(session, targets),
		newScreenshotTool(session),
		newReadUITool(session),
		newTapTool(session),
		newTypeTextTool(session),
		newSwipeTool(session),
		newWaitForTool(session),
		newPressButtonTool(session),
		newRotateTool(session),
		newUnlockTool(session),
		newReleaseTool(session),
	}
}

func toolError(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message, IsError: true}
}

func toolOK(name, message string) domain.ToolResult {
	return domain.ToolResult{Name: name, Content: message}
}

// runError is where the busy device stops being an error. Everything else keeps
// Appium's own wording, which is what tells the agent whether it mistyped a
// selector or the app crashed; only the conditions the agent can do nothing
// about are rewritten.
func runError(name, op string, err error) domain.ToolResult {
	var hubDown *hubUnavailableError
	switch {
	case errors.Is(err, errDeviceBusy):
		return deviceBlock(name)
	case errors.Is(err, errNotConfigured):
		return toolError(name, notConfiguredMsg)
	case errors.As(err, &hubDown):
		return toolError(name, prompt.MobileHubUnavailableText(hubDown.err.Error()))
	}
	return toolError(name, fmt.Sprintf("%s: %v", op, err))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n[truncated at %d bytes]", max)
}

// selectorArgs is the shared locator every element tool takes. Two fields
// rather than a free-form string: Appium's strategy names are not guessable
// ("accessibility id", "-android uiautomator"), and an agent that has to spell
// them exactly gets them wrong.
type selectorArgs struct {
	Text        string `json:"text"`
	ResourceID  string `json:"resource_id"`
	ContentDesc string `json:"content_desc"`
	XPath       string `json:"xpath"`
}

func selectorProperties() map[string]interface{} {
	return map[string]interface{}{
		"text": map[string]interface{}{
			"type": "string",
		},
		"resource_id": map[string]interface{}{
			"type": "string",
		},
		"content_desc": map[string]interface{}{
			"type": "string",
		},
		"xpath": map[string]interface{}{
			"type": "string",
		},
	}
}

// strategy maps the args onto one Appium locator, in the order that survives a
// redesign best: an id outlives a label, a label outlives a position in a tree.
func (a selectorArgs) strategy() (using, value string, ok bool) {
	switch {
	case strings.TrimSpace(a.ResourceID) != "":
		id := strings.TrimSpace(a.ResourceID)
		if !strings.Contains(id, "/") {
			// Appium's "id" strategy matches the fully-qualified resource-id;
			// a bare name matches nothing, which reads to the agent as "the
			// button is missing" rather than "you named it short".
			return "-android uiautomator",
				fmt.Sprintf(`new UiSelector().resourceIdMatches(".*/%s")`, escapeUiSelector(id)), true
		}
		return "id", id, true
	case strings.TrimSpace(a.ContentDesc) != "":
		return "accessibility id", strings.TrimSpace(a.ContentDesc), true
	case strings.TrimSpace(a.Text) != "":
		return "-android uiautomator",
			fmt.Sprintf(`new UiSelector().text("%s")`, escapeUiSelector(strings.TrimSpace(a.Text))), true
	case strings.TrimSpace(a.XPath) != "":
		return "xpath", strings.TrimSpace(a.XPath), true
	}
	return "", "", false
}

func (a selectorArgs) describe() string {
	switch {
	case strings.TrimSpace(a.ResourceID) != "":
		return "resource_id=" + a.ResourceID
	case strings.TrimSpace(a.ContentDesc) != "":
		return "content_desc=" + a.ContentDesc
	case strings.TrimSpace(a.Text) != "":
		return "text=" + a.Text
	default:
		return "xpath=" + a.XPath
	}
}

// escapeUiSelector protects the Java string literal the UiSelector expression
// is built from. Without it a label containing a quote is a syntax error the
// agent reads as a missing element.
func escapeUiSelector(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return r.Replace(s)
}

// findElement resolves a selector to an Appium element id.
func findElement(ctx context.Context, s device, sel selectorArgs) (string, error) {
	using, value, ok := sel.strategy()
	if !ok {
		return "", errors.New(prompt.MobileSelectorMissingText())
	}
	var out struct {
		Value map[string]string `json:"value"`
	}
	if err := s.run(ctx, "POST", "/element", map[string]interface{}{
		"using": using, "value": value,
	}, &out); err != nil {
		return "", err
	}
	for _, id := range out.Value {
		if id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("no element matching %s", sel.describe())
}

func decodeArgs(name, arguments string, into interface{}) *domain.ToolResult {
	if strings.TrimSpace(arguments) == "" {
		arguments = "{}"
	}
	if err := json.Unmarshal([]byte(arguments), into); err != nil {
		res := toolError(name, fmt.Sprintf("invalid arguments: %v", err))
		return &res
	}
	return nil
}

func def(name, description string, props map[string]interface{}, required ...string) domain.ToolDefinition {
	params := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  params,
		},
	}
}
