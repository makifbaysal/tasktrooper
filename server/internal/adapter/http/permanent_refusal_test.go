package http

import (
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	agentcliapp "github.com/makifbaysal/tasktrooper/server/internal/application/agentcli"
	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/application/llmprovider"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The number is the point. Each of these answered 500 with a correct, permanent
// sentence in the body, and 500 is the one answer that tells a client and a
// monitor to send the same refused request again.
func TestPermanentProviderRefusalsAnswer409(t *testing.T) {
	h := &Handler{llmProviderSvc: llmprovider.NewService(fakeLLMProviderStore{}, &fakeLLMEndpointStore{}, nil, 0, nil)}
	app := fiber.New()
	h.registerLLMProviderRoutes(app)

	cases := []struct {
		name   string
		method string
		path   string
		code   string
	}{
		// cursor_agent, antigravity and opencode all have executors now (see
		// internal/adapter/agentcli/{cursor,antigravity,opencode}) and are
		// Available:true, so they fall through to the same HostExecuted check
		// claude_code always has — codeProviderUnavailable no longer applies
		// to any provider this route can be asked about.
		{"cursor_agent activate", "POST", "/v1/llm/providers/cursor_agent/activate", codeHostExecutedProvider},
		{"antigravity activate", "POST", "/v1/llm/providers/antigravity/activate", codeHostExecutedProvider},
		{"cursor_agent connect", "POST", "/v1/llm/providers/cursor_agent/connect", codeHostExecutedProvider},
		{"opencode connect", "POST", "/v1/llm/providers/opencode/connect", codeHostExecutedProvider},
		{"opencode activate", "POST", "/v1/llm/providers/opencode/activate", codeHostExecutedProvider},
		{"claude_code connect", "POST", "/v1/llm/providers/claude_code/connect", codeHostExecutedProvider},
		{"claude_code activate", "POST", "/v1/llm/providers/claude_code/activate", codeHostExecutedProvider},
		{"claude_code test", "POST", "/v1/llm/providers/claude_code/test", codeHostExecutedProvider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != nethttp.StatusConflict {
				t.Fatalf("status = %d, want 409: %s", resp.StatusCode, string(body))
			}
			var out struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
				Code string `json:"code"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				t.Fatalf("decode: %v (%s)", err, string(body))
			}
			// Both places, because tenant-manager used to write the code at the
			// top level and this server's own errors carry it in error.type; a
			// client that learned one must not have to learn the other.
			if out.Code != tc.code || out.Error.Type != tc.code {
				t.Fatalf("code = %q / type = %q, want %q", out.Code, out.Error.Type, tc.code)
			}
			if out.Error.Message == "" {
				t.Fatal("the refusal must still say why")
			}
		})
	}
}

// The stale half of that sentence: in cloud claude_code runs on the assigned
// member's Mac, not on the machine this server is on.
func TestHostExecutedRefusalDoesNotClaimTheServerHost(t *testing.T) {
	h := &Handler{llmProviderSvc: llmprovider.NewService(fakeLLMProviderStore{}, &fakeLLMEndpointStore{}, nil, 0, nil)}
	app := fiber.New()
	h.registerLLMProviderRoutes(app)

	req := httptest.NewRequest("POST", "/v1/llm/providers/claude_code/connect", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, stale := range []string{"local process on the server host", "on the server host"} {
		if strings.Contains(string(body), stale) {
			t.Fatalf("refusal still claims %q: %s", stale, string(body))
		}
	}
	if !strings.Contains(string(body), "Mac") {
		t.Fatalf("the refusal should say where it does run: %s", string(body))
	}
}

// A move blocked by open acceptance criteria used to reach the human mover as
// the agent-facing sentence verbatim — task/criterion ids and all. The 400
// must instead name the count and each criterion's own text, with no id in
// sight, and still 400 (not 500) so the board can show it as a refusal, not a
// crash.
func TestCriteriaGateRefusalIsHumanReadable(t *testing.T) {
	app := fiber.New()
	criterionID := uuid.MustParse("a954ef75-9fe7-4b86-9ac4-f234a1b74569")
	taskID := uuid.MustParse("97f5ff29-6e4c-4fca-801d-d579696e711d")

	app.Get("/unchecked-single", func(c *fiber.Ctx) error {
		err := domain.NewCriteriaGateError(
			domain.TaskColumnPMUAT,
			domain.CriteriaGateReasonUnchecked,
			[]domain.CriteriaGateCriterion{{ID: criterionID, Text: "Given X, When Y, Then Z"}},
			"cannot move to pm_uat: 1 acceptance criteria await your pm verdict: ["+criterionID.String()+"] Given X, When Y, Then Z — call review_criterion with each id above, then retry the move",
		)
		return badRequestErr(c, err)
	})
	app.Get("/unchecked-multi", func(c *fiber.Ctx) error {
		err := domain.NewCriteriaGateError(
			domain.TaskColumnPMUAT,
			domain.CriteriaGateReasonUnchecked,
			[]domain.CriteriaGateCriterion{
				{ID: criterionID, Text: "First criterion"},
				{ID: taskID, Text: "Second criterion"},
			},
			"cannot move to pm_uat: 2 acceptance criteria await your pm verdict: [...] — call review_criterion with each id above, then retry the move",
		)
		return badRequestErr(c, err)
	})
	app.Get("/rejected", func(c *fiber.Ctx) error {
		err := domain.NewCriteriaGateError(
			domain.TaskColumnDone,
			domain.CriteriaGateReasonRejected,
			[]domain.CriteriaGateCriterion{{ID: criterionID, Text: "Rejected criterion"}},
			"cannot move to done: 1 acceptance criteria are rejected by qa: [...] — move the task to need_revision instead",
		)
		return badRequestErr(c, err)
	})
	app.Get("/incomplete", func(c *fiber.Ctx) error {
		err := domain.NewCriteriaGateError(
			domain.TaskColumnDone,
			domain.CriteriaGateReasonIncomplete,
			[]domain.CriteriaGateCriterion{{ID: criterionID, Text: "Unticked criterion"}},
			"cannot move to done: 1 acceptance criteria are still open: [...] — call set_criterion_completed or cancel_criterion, then retry the move",
		)
		return badRequestErr(c, err)
	})

	t.Run("single unchecked criterion", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/unchecked-single", nil))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != nethttp.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", resp.StatusCode, string(body))
		}
		message, typ, code := decodeCoded(t, body)
		if typ != codeCriteriaNotApproved || code != codeCriteriaNotApproved {
			t.Fatalf("type = %q / code = %q, want %q", typ, code, codeCriteriaNotApproved)
		}
		if strings.Contains(message, criterionID.String()) {
			t.Fatalf("message must not contain the criterion id: %s", message)
		}
		if strings.Contains(message, taskID.String()) {
			t.Fatalf("message must not contain the task id: %s", message)
		}
		if !strings.Contains(message, "Given X, When Y, Then Z") {
			t.Fatalf("message must name the criterion: %s", message)
		}
		if !strings.Contains(message, "1") {
			t.Fatalf("message must say how many criteria are open: %s", message)
		}
	})

	t.Run("multiple unchecked criteria are counted and titled", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/unchecked-multi", nil))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		message, _, _ := decodeCoded(t, body)
		if !strings.Contains(message, "2") {
			t.Fatalf("message must say 2 criteria are open: %s", message)
		}
		if !strings.Contains(message, "First criterion") || !strings.Contains(message, "Second criterion") {
			t.Fatalf("message must list both criteria: %s", message)
		}
	})

	t.Run("rejected criteria say so distinctly", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/rejected", nil))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		message, _, _ := decodeCoded(t, body)
		if !strings.Contains(message, "rejected") {
			t.Fatalf("message must say the criterion was rejected: %s", message)
		}
		if strings.Contains(message, criterionID.String()) {
			t.Fatalf("message must not contain the criterion id: %s", message)
		}
	})

	t.Run("incomplete criteria say the implementer hasn't finished, not that it's awaiting approval", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest("GET", "/incomplete", nil))
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		message, _, _ := decodeCoded(t, body)
		if !strings.Contains(message, "not yet completed") {
			t.Fatalf("message must say the criterion is not yet completed: %s", message)
		}
		if strings.Contains(message, "waiting for approval") {
			t.Fatalf("message must not claim it's waiting for approval — nobody is reviewing an incomplete criterion: %s", message)
		}
		if !strings.Contains(message, "Unticked criterion") {
			t.Fatalf("message must name the criterion: %s", message)
		}
		if strings.Contains(message, criterionID.String()) {
			t.Fatalf("message must not contain the criterion id: %s", message)
		}
	})
}

// A manual drag into todo or in_progress with an open `blocks` relation must
// answer a coded 400 naming the blocker, not the raw agent-facing sentence
// stripped of nothing (there is no id in it to strip — RelationLabel already
// renders key + title).
func TestWorkOrderGateRefusalIsHumanReadable(t *testing.T) {
	app := fiber.New()
	app.Get("/todo", func(c *fiber.Ctx) error {
		err := domain.NewWorkOrderGateError(
			domain.TaskColumnTodo,
			[]domain.WorkOrderGateBlocker{{Key: "T-1", Title: "API migration"}},
			"work order: this task is blocked until these are done: T-1 (API migration) [in_progress]",
		)
		return badRequestErr(c, err)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/todo", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, string(body))
	}
	message, typ, code := decodeCoded(t, body)
	if typ != codeTaskBlockedByDependency || code != codeTaskBlockedByDependency {
		t.Fatalf("type = %q / code = %q, want %q", typ, code, codeTaskBlockedByDependency)
	}
	if !strings.Contains(message, "T-1 (API migration)") {
		t.Fatalf("message must name the blocker: %s", message)
	}
}

// decodeCoded reads the two places a code appears. Both, always: tenant-manager
// used to write it at the top level and this server's own errors carry it in
// error.type, and a client that learned one must not have to learn the other.
func decodeCoded(t *testing.T, body []byte) (message, typ, code string) {
	t.Helper()
	var out struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, string(body))
	}
	return out.Error.Message, out.Error.Type, out.Code
}

// An incomplete body is the caller's to fix and never becomes valid on a retry.
func TestAgentValidationAnswers400(t *testing.T) {
	h := &Handler{catalogSvc: catalog.NewService(nil, nil, "")}
	app := fiber.New()
	h.registerOrchestrationRoutes(app)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   string
	}{
		{"update with no name", "PUT", "/admin/agents/6f1c1f7e-0d5e-4a6b-9d1a-2f3c4b5a6d7e", `{"description":"x"}`, "name is required"},
		{"create with no name", "POST", "/admin/agents", `{"description":"x"}`, "name is required"},
		{"create with a bad effort", "POST", "/admin/agents", `{"name":"n","effort":"enormous"}`, "effort must be one of"},
		{"create with a negative turn cap", "POST", "/admin/agents", `{"name":"n","max_turns":-1}`, "max_turns cannot be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != nethttp.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", resp.StatusCode, string(body))
			}
			if !strings.Contains(string(body), tc.want) {
				t.Fatalf("body must still say what is wrong, got %s", string(body))
			}
			// The status was already right; the code is what a client can
			// branch on without matching the sentence it also displays.
			_, typ, code := decodeCoded(t, body)
			if typ != codeInvalidCatalogInput || code != codeInvalidCatalogInput {
				t.Fatalf("type = %q / code = %q, want %q", typ, code, codeInvalidCatalogInput)
			}
		})
	}
}

// A flavor that names no CLI at all. Its 400 was right and its body said only
// prose — and the neighbouring refusal on the same route family (a flavor that
// IS known and not built) has answered a coded 409 since permanent_refusal.go
// landed, so a client could tell the two apart only by reading English.
func TestUnknownAgentCLIFlavorIsACoded400(t *testing.T) {
	h := &Handler{agentCLISvc: agentcliapp.NewService(agentcliapp.Deps{})}
	app := fiber.New()
	h.registerAgentCLIRoutes(app)

	for _, tc := range []struct{ method, path string }{
		{"POST", "/v1/agent-cli/emacs/connect"},
		{"DELETE", "/v1/agent-cli/emacs"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != nethttp.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", resp.StatusCode, string(body))
			}
			message, typ, code := decodeCoded(t, body)
			if typ != codeUnknownAgentCLIFlavor || code != codeUnknownAgentCLIFlavor {
				t.Fatalf("type = %q / code = %q, want %q", typ, code, codeUnknownAgentCLIFlavor)
			}
			if message == "" {
				t.Fatal("the refusal must still say why")
			}
		})
	}
}
