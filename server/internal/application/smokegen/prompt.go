package smokegen

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const systemPrompt = `You draft post-deploy smoke checks for one component of a software project.

A smoke check is a single read-only HTTP request sent to PRODUCTION right after every deploy. If it fails, the release is rolled back. So every check must be safe to send to production at any time, and must pass whenever the deploy is healthy.

Rules:
- Only GET or HEAD.
- Only public, unauthenticated, side-effect-free endpoints: the home page, health / readiness / liveness / version / status endpoints, key public pages, public read-only API GETs, a static asset. Never anything that needs a login, a cookie, an API key or a token, and never anything that writes, sends mail, charges, enqueues work or triggers a job.
- "path" starts with "/" and is relative to the production URL. Use an absolute https URL only for a different public host this component itself serves.
- Set "expect_status" only when the code makes the status certain (for example a health handler that returns 200). Leave it out otherwise; any 2xx/3xx then passes.
- Set "contains" only for short text the code guarantees in the response body (for example a health JSON field such as "\"status\":\"ok\""). Never for HEAD. Never for content that changes (dates, counts, user data).
- Optionally set "max_latency_ms" (1-10000), e.g. 3000 for a health endpoint.
- Give each check a short human "name".
- Propose 3 to 8 checks, most valuable first. Fewer is fine when the code offers fewer safe endpoints.

Read the code before answering: the router / route definitions, the framework's conventions (file-based routes, controllers, handlers), health handlers, middleware that requires auth. Do not guess endpoints that do not exist in the code.

Reply with ONLY a JSON object, no prose, no code fences:
{"checks":[{"name":"...","method":"GET","path":"/...","expect_status":200,"contains":"...","max_latency_ms":3000}]}`

func userPrompt(comp domain.Component, repo domain.Repository, brief string, existing []domain.SmokeCheck) string {
	var b strings.Builder
	dir := comp.Path
	if dir == "." {
		dir = "the repository root"
	}
	fmt.Fprintf(&b, "Repository: %s. The component lives in %s — read the code there.\n", repo.Name, dir)
	if strings.TrimSpace(brief) != "" {
		b.WriteString("\nProject brief (stack, commands, where it runs — the production URL is listed under \"Runs on\" when one is bound):\n")
		b.WriteString(brief)
		b.WriteString("\n")
	} else {
		b.WriteString("\nNo project brief is available; work it out from the code.\n")
	}
	if len(existing) > 0 {
		b.WriteString("\nThese checks already exist — do not propose them again:\n")
		for _, c := range existing {
			fmt.Fprintf(&b, "- %s %s\n", c.Method, c.Path)
		}
	}
	b.WriteString("\nReply with only the JSON object.")
	return b.String()
}

// parseChecks accepts the object the prompt asks for, and tolerates the two
// ways models drift from it: prose or code fences around the object, and a
// bare array instead of {"checks": [...]}.
func parseChecks(raw string) ([]domain.SmokeCheck, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, errors.New("empty answer")
	}
	start := strings.IndexAny(text, "{[")
	if start < 0 {
		return nil, errors.New("no JSON in the answer")
	}
	if text[start] == '[' {
		end := strings.LastIndex(text, "]")
		if end < start {
			return nil, errors.New("unterminated JSON array")
		}
		var checks []domain.SmokeCheck
		if err := json.Unmarshal([]byte(text[start:end+1]), &checks); err != nil {
			return nil, err
		}
		return checks, nil
	}
	end := strings.LastIndex(text, "}")
	if end < start {
		return nil, errors.New("unterminated JSON object")
	}
	var out struct {
		Checks *[]domain.SmokeCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return nil, err
	}
	if out.Checks == nil {
		return nil, errors.New(`the object has no "checks" field`)
	}
	return *out.Checks, nil
}

// filterChecks keeps the proposals that would pass the delivery profile's own
// validation, are not already present, and fit under limit; everything else
// is counted as dropped so the UI can say the agent over-delivered.
func filterChecks(proposed, existing []domain.SmokeCheck, limit int) ([]domain.SmokeCheck, int) {
	seen := make(map[string]bool, len(existing)+len(proposed))
	for _, c := range existing {
		seen[checkKey(c.Normalized())] = true
	}
	kept := make([]domain.SmokeCheck, 0, len(proposed))
	dropped := 0
	for _, c := range proposed {
		c = c.Normalized()
		if c.Method == "HEAD" {
			c.Contains = ""
		}
		if domain.ValidateSmokeChecks([]domain.SmokeCheck{c}) != nil {
			dropped++
			continue
		}
		key := checkKey(c)
		if seen[key] || len(kept) >= limit {
			dropped++
			continue
		}
		seen[key] = true
		kept = append(kept, c)
	}
	return kept, dropped
}

func checkKey(c domain.SmokeCheck) string {
	return c.Method + " " + strings.TrimRight(c.Path, "/")
}
