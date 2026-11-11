package http

import (
	"bytes"
	"encoding/json"
	"strings"
)

// The remote listener answers ONLY what this table names. Everything else is
// refused before a handler runs, so a route added to the local API tomorrow is
// unreachable from a phone until someone writes it down here (the test
// TestRemoteAllowlistDeniesEveryOtherRegisteredRoute walks every registered
// route to keep it that way).
//
// Request and response shapes of forwarded routes are the local API's own.

type remoteRule struct {
	Method  string
	Pattern string
	// Write marks the routes a viewer device may not call.
	Write bool
	// Loose lets a path parameter be any URL-safe token instead of a UUID, for
	// ids that are not UUIDs (a GitHub device-flow id).
	Loose bool
	// Native routes are answered by the remote listener itself, not forwarded.
	Native bool
	// SelfService routes are open to a viewer although they are not GETs.
	SelfService bool
	// Check validates the request body before the route is forwarded.
	Check func(body []byte) *remoteRefusal

	segments []string
}

type remoteRefusal struct{ Message string }

func R(pattern string) remoteRule { return remoteRule{Method: "GET", Pattern: pattern} }
func W(method, pattern string) remoteRule {
	return remoteRule{Method: method, Pattern: pattern, Write: true}
}

func (r remoteRule) withCheck(f func([]byte) *remoteRefusal) remoteRule { r.Check = f; return r }
func (r remoteRule) loose() remoteRule                                  { r.Loose = true; return r }
func (r remoteRule) native() remoteRule                                 { r.Native = true; return r }
func (r remoteRule) self() remoteRule                                   { r.SelfService = true; return r }

const (
	remoteInfoPath = "/remote/v1/info"
	remotePairPath = "/remote/v1/pair"
)

var remoteRules = compileRemoteRules([]remoteRule{
	// Remote-native (answered by the listener).
	R("/remote/v1/me").native(),
	W("DELETE", "/remote/v1/me").native().self(),
	R("/remote/v1/changes").native(),
	R("/remote/v1/pending").native(),
	R("/remote/v1/sessions/:id/turn").native(),
	W("POST", "/remote/v1/sessions/:id/messages").native().withCheck(checkSessionMessage),

	// Projects, repositories, board.
	R("/v1/projects"),
	R("/v1/projects/overview"),
	R("/v1/projects/:projectId"),
	R("/v1/repositories"),
	R("/v1/repositories/:id"),
	R("/v1/board/config"),
	R("/v1/board/columns"),
	R("/v1/tasks"),
	R("/v1/tasks/lookup"),
	R("/v1/repositories/:id/tasks"),
	R("/v1/repositories/:id/tasks/:taskId"),
	R("/v1/repositories/:id/tasks/:taskId/comments"),
	R("/v1/repositories/:id/tasks/:taskId/events"),
	R("/v1/repositories/:id/tasks/:taskId/runs"),
	R("/v1/repositories/:id/tasks/:taskId/questions"),
	R("/v1/repositories/:id/tasks/:taskId/reviews"),
	R("/v1/repositories/:id/tasks/:taskId/review-history"),
	R("/v1/repositories/:id/tasks/:taskId/documents"),
	R("/v1/repositories/:id/tasks/:taskId/acceptance-criteria"),
	R("/v1/repositories/:id/tasks/:taskId/test-cases"),
	R("/v1/repositories/:id/tasks/:taskId/attachments"),
	R("/v1/attachments/:id"),
	R("/v1/activity"),
	R("/v1/activity/active"),

	// Agents, chats, runs.
	R("/admin/agents"),
	R("/admin/agents/:id"),
	R("/v1/roles"),
	R("/v1/workflows"),
	R("/v1/task-types"),
	R("/v1/sessions"),
	R("/v1/sessions/:id"),
	R("/v1/sessions/:id/activity"),
	R("/v1/runs/:id/steps"),

	// Read-only settings views: status, never a secret.
	R("/v1/agent-cli"),
	R("/v1/llm/providers"),
	R("/v1/models"),
	R("/v1/settings/github"),
	R("/v1/settings/github/owners"),
	R("/v1/settings/github/repos"),
	R("/v1/settings/github/device/:id").loose(),

	// Audience research (capability "audience"): synthetic personas, studies
	// and their reports. The long work (writing the people, the interviews,
	// the report) runs on this computer; a phone starts it and polls.
	R("/v1/audience-research/status"),
	R("/v1/audiences"),
	R("/v1/audiences/:id"),
	R("/v1/audience-studies"),
	R("/v1/audience-studies/:id"),
	R("/v1/audience-personas/:id/messages"),

	// Board work.
	W("POST", "/v1/repositories/:id/tasks").withCheck(onlyKeys(createTaskKeys)),
	W("PATCH", "/v1/repositories/:id/tasks/:taskId").withCheck(onlyKeys(moveTaskKeys)),
	W("POST", "/v1/repositories/:id/tasks/:taskId/comments"),
	W("POST", "/v1/repositories/:id/tasks/:taskId/chat"),
	W("POST", "/v1/repositories/:id/tasks/:taskId/before-deploy/confirm"),
	W("PATCH", "/v1/repositories/:id/tasks/:taskId/questions/:questionId"),
	W("POST", "/v1/repositories/:id/tasks/:taskId/questions/submit"),
	W("POST", "/v1/repositories/:id/tasks/:taskId/runs/:runId/cancel"),
	W("POST", "/v1/repositories/:id/tasks/:taskId/runs/:runId/interrupt"),
	W("POST", "/v1/repositories/:id/tasks/:taskId/runs/:runId/rerun"),

	// Chats.
	W("POST", "/v1/sessions"),
	W("POST", "/v1/sessions/:id/messages").withCheck(checkSessionMessage),
	W("POST", "/v1/sessions/:id/cancel"),
	W("DELETE", "/v1/sessions/:id"),

	// Projects and repositories.
	W("POST", "/v1/projects"),
	W("DELETE", "/v1/projects/:projectId"),
	W("POST", "/v1/repositories/import").withCheck(checkImport),
	W("PUT", "/v1/repositories/:id/projects"),
	W("DELETE", "/v1/repositories/:id"),
	W("POST", "/v1/settings/github/device"),

	// Audience research. Building and every study step spend model calls on
	// this computer; the explicit tap on the phone is the approval, so
	// user_approved stays in the body the server already demands.
	W("POST", "/v1/audiences").withCheck(onlyKeys(audienceKeys)),
	W("PATCH", "/v1/audiences/:id").withCheck(onlyKeys(audienceKeys)),
	W("DELETE", "/v1/audiences/:id"),
	W("POST", "/v1/audiences/:id/build"),
	W("POST", "/v1/audiences/:id/build/cancel"),
	W("POST", "/v1/audiences/:id/studies").withCheck(onlyKeys(studyPlanKeys)),
	W("PATCH", "/v1/audience-studies/:id").withCheck(onlyKeys(studyPlanKeys)),
	W("DELETE", "/v1/audience-studies/:id"),
	W("POST", "/v1/audience-studies/:id/script"),
	W("POST", "/v1/audience-studies/:id/script/approve").withCheck(onlyKeys(scriptApproveKeys)),
	W("POST", "/v1/audience-studies/:id/start").withCheck(onlyKeys(studyRunKeys)),
	W("POST", "/v1/audience-studies/:id/report").withCheck(onlyKeys(studyRunKeys)),
	W("POST", "/v1/audience-studies/:id/retry").withCheck(onlyKeys(studyRunKeys)),
	W("POST", "/v1/audience-studies/:id/cancel"),
	W("POST", "/v1/audience-personas/:id/messages").withCheck(onlyKeys(personaMessageKeys)),
})

var (
	createTaskKeys = setOf("title", "task_type", "description", "technical_description", "initiative_project_id",
		"component_id", "column", "priority", "assignee_agent_id", "acceptance_criteria", "relations", "blocked_by")
	moveTaskKeys = setOf("column", "position", "assignee_agent_id", "priority", "title", "description")

	audienceKeys       = setOf("name", "repository", "brief", "persona_target")
	studyPlanKeys      = setOf("title", "goal", "kind", "inputs", "questions", "script", "price_points", "currency")
	scriptApproveKeys  = setOf("version")
	studyRunKeys       = setOf("user_approved", "manual_report")
	personaMessageKeys = setOf("message")
)

func setOf(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func compileRemoteRules(rules []remoteRule) []remoteRule {
	for i := range rules {
		rules[i].segments = strings.Split(strings.TrimPrefix(rules[i].Pattern, "/"), "/")
	}
	return rules
}

// matchRemoteRule finds the rule for an exact method and path. Paths are
// matched byte for byte: no case folding, no trailing slash, no escapes, so
// the router behind the allowlist can only read the path the way this did.
func matchRemoteRule(method, path string) (*remoteRule, bool) {
	if !canonicalRemotePath(path) {
		return nil, false
	}
	segs := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i := range remoteRules {
		r := &remoteRules[i]
		if r.Method != method || len(r.segments) != len(segs) {
			continue
		}
		ok := true
		for j, want := range r.segments {
			got := segs[j]
			if strings.HasPrefix(want, ":") {
				if r.Loose {
					ok = urlSafeToken(got)
				} else {
					ok = isUUIDText(got)
				}
			} else {
				ok = want == got
			}
			if !ok {
				break
			}
		}
		if ok {
			return r, true
		}
	}
	return nil, false
}

func canonicalRemotePath(p string) bool {
	if p == "" || p[0] != '/' || len(p) > 300 || strings.HasSuffix(p, "/") && p != "/" {
		return false
	}
	if strings.Contains(p, "//") {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c <= 0x20 || c >= 0x7f || c == '%' || c == '\\' || c == ';' || c == '?' || c == '#' {
			return false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func urlSafeToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func isUUIDText(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

func onlyKeys(allowed map[string]bool) func([]byte) *remoteRefusal {
	return func(body []byte) *remoteRefusal {
		obj, bad := decodeObject(body)
		if bad != nil {
			return bad
		}
		for k := range obj {
			if !allowed[k] {
				return &remoteRefusal{Message: "field not allowed over remote access: " + k}
			}
		}
		return nil
	}
}

func decodeObject(body []byte) (map[string]json.RawMessage, *remoteRefusal) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, &remoteRefusal{Message: "request body must be a JSON object"}
	}
	return obj, nil
}

func checkSessionMessage(body []byte) *remoteRefusal {
	obj, bad := decodeObject(body)
	if bad != nil {
		return bad
	}
	if raw, ok := obj["role"]; ok {
		var role string
		if json.Unmarshal(raw, &role) != nil || (role != "" && role != "user") {
			return &remoteRefusal{Message: "field not allowed over remote access: role must be user"}
		}
	}
	return nil
}

func checkImport(body []byte) *remoteRefusal {
	obj, bad := decodeObject(body)
	if bad != nil {
		return bad
	}
	if raw, ok := obj["clone_url"]; ok {
		var u string
		if json.Unmarshal(raw, &u) != nil || !strings.HasPrefix(u, "https://github.com/") {
			return &remoteRefusal{Message: "field not allowed over remote access: clone_url must be an https://github.com/ address"}
		}
	}
	return nil
}
