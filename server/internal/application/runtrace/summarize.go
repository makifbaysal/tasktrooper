package runtrace

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	maxCommandBytes = 2000
	maxLineBytes    = 300
	outputEdgeLines = 20
	maxIDs          = 12
)

const (
	EffectPRComment  = "pr_comment"
	EffectPRMerge    = "pr_merge"
	EffectDeploy     = "deploy"
	EffectHTTP       = "http_request"
	EffectBrowser    = "browser"
	EffectDevice     = "device"
	EffectMCP        = "mcp"
	EffectBoard      = "board"
	EffectBoardNote  = "board_comment"
	EffectShellOuter = "shell_external"
)

var (
	readTools = set("read_file", "grep_code", "get_repo_tree", "codebase_search", "get_symbol_skeleton",
		"expand_symbol_context", "search_boilerplate_catalog", "load_skill", "search_memory", "get_design_system")
	changeTools   = set("write_file", "edit_file", "edit_lines", "delete_file", "move_file")
	execTools     = set("run_terminal")
	externalTools = set("http_request", "fetch_url", "web_search", "download_file",
		"comment_on_pull_request", "merge_task_pull_request", "deploy_release", "rollback_release",
		"finish_release", "watch_release", "record_local_deploy", "start_task_preview", "run_smoke_checks",
		"telemetry_api_get", "telemetry_get_service", "telemetry_list_errors", "telemetry_search_logs",
		"telemetry_get_incident", "telemetry_list_deploys", "telemetry_list_incidents", "telemetry_search_traces",
		"query_runtime_logs", "get_deploy_logs", "list_deployments", "list_runtime_errors", "get_environment")
	browserActions = set("browser_navigate", "browser_click", "browser_fill")
	deviceActions  = set("mobile_tap", "mobile_swipe", "mobile_rotate", "mobile_type_text", "mobile_launch_app",
		"mobile_press_button", "mobile_unlock_device")
	boardMutations = set("create_board_task", "update_board_task", "move_board_task", "delete_board_task",
		"claim_board_task", "set_criterion_completed", "cancel_criterion", "review_criterion",
		"record_test_cases", "set_test_case_result", "add_task_document", "update_task_document",
		"attach_task_file", "record_open_questions", "declare_env_vars", "resolve_document_annotations",
		"commit_task_changes")
	boardNotes = set("add_task_comment", "notify_task_agent")
	prEffects  = map[string]string{
		"comment_on_pull_request": EffectPRComment,
		"merge_task_pull_request": EffectPRMerge,
		"deploy_release":          EffectDeploy,
		"rollback_release":        EffectDeploy,
		"finish_release":          EffectDeploy,
	}
	pathKeys    = []string{"path", "file_path", "filePath", "file", "target_file", "filename", "notebook_path", "relative_path"}
	commandKeys = []string{"command", "cmd", "script"}
	urlKeys     = []string{"url", "uri", "href"}
)

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

var shellExternal = regexp.MustCompile(`(?i)(^|[;&|(\s])(git\s+push|gh\s+(pr|release|issue|api|workflow|run)\b|curl\b[^|;&]*(-X\s*(POST|PUT|PATCH|DELETE)|--data|\s-d\s|--upload|-F\s)|wget\b[^|;&]*--post|npm\s+publish|yarn\s+publish|pnpm\s+publish|twine\s+upload|cargo\s+publish|docker\s+push|helm\s+(install|upgrade)|kubectl\s+(apply|delete|create|patch|rollout|scale)|terraform\s+(apply|destroy)|gcloud\s|aws\s|az\s|firebase\s+deploy|vercel\b|netlify\s+deploy|fastlane\b|ssh\s|scp\s|rsync\s[^|;&]*:|psql\b|mysql\b|redis-cli\b)`)

// Classify names the category of a tool: its ledger name first, then the
// native name a CLI used, then the shape of the name.
func Classify(tool, native string) string {
	for _, name := range []string{tool, native} {
		switch {
		case name == "":
		case changeTools[name]:
			return domain.TraceCategoryChange
		case execTools[name]:
			return domain.TraceCategoryExec
		case readTools[name]:
			return domain.TraceCategoryRead
		case externalTools[name], browserActions[name], deviceActions[name]:
			return domain.TraceCategoryExternal
		case strings.HasPrefix(name, "browser_"), strings.HasPrefix(name, "mobile_"):
			return domain.TraceCategoryExternal
		}
	}
	lower := strings.ToLower(native)
	switch lower {
	case "notebookedit", "multiedit", "applypatch", "apply_patch", "patch":
		return domain.TraceCategoryChange
	case "ls", "view", "list", "glob", "notebookread":
		return domain.TraceCategoryRead
	case "bash", "shell":
		return domain.TraceCategoryExec
	case "webfetch", "websearch":
		return domain.TraceCategoryExternal
	}
	if strings.HasPrefix(tool, "mcp__") || strings.HasPrefix(native, "mcp__") {
		return domain.TraceCategoryExternal
	}
	return domain.TraceCategoryBoard
}

type argMap map[string]json.RawMessage

func parseArgs(raw string) argMap {
	var m argMap
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return argMap{}
	}
	return m
}

func (a argMap) str(keys ...string) string {
	for _, k := range keys {
		if v, ok := a[k]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil && s != "" {
				return s
			}
		}
	}
	return ""
}

func (a argMap) num(keys ...string) int {
	for _, k := range keys {
		if v, ok := a[k]; ok {
			var n float64
			if json.Unmarshal(v, &n) == nil {
				return int(n)
			}
			var s string
			if json.Unmarshal(v, &s) == nil {
				if i, err := strconv.Atoi(s); err == nil {
					return i
				}
			}
		}
	}
	return 0
}

func (a argMap) path() string { return a.str(pathKeys...) }

func relativeTo(root, p string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		return filepath.ToSlash(filepath.Clean(p))
	}
	if root == "" {
		return filepath.ToSlash(p)
	}
	if rel, err := filepath.Rel(root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(p)
}

func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

func urlWithoutSecrets(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return domain.TruncateHead(s, max)
}

// summary is the structured account of one call: the target the timeline
// shows and the detail object stored beside it.
type summary struct {
	target string
	detail map[string]any
	paths  []string
	effect string
}

func summarizeCall(category, tool, native, arguments, root string) summary {
	a := parseArgs(arguments)
	s := summary{detail: map[string]any{}}
	switch category {
	case domain.TraceCategoryRead:
		p := relativeTo(root, a.path())
		switch {
		case p != "":
			s.target = p
			s.detail["path"] = p
			s.paths = []string{p}
		case a.str("pattern", "query") != "":
			q := Scrub(domain.TruncateHead(a.str("pattern", "query"), 200))
			s.target = q
			s.detail["query"] = q
			if g := a.str("glob"); g != "" {
				s.detail["glob"] = g
			}
		}
		if n := a.num("offset"); n > 0 {
			s.detail["offset"] = n
		}
		if n := a.num("limit"); n > 0 {
			s.detail["limit"] = n
		}
		if n := a.num("start_line"); n > 0 {
			s.detail["start_line"] = n
		}
		if n := a.num("end_line"); n > 0 {
			s.detail["end_line"] = n
		}
	case domain.TraceCategoryChange:
		s.detail["op"] = changeOp(tool, native)
		if tool == "move_file" {
			from, to := relativeTo(root, a.str("from")), relativeTo(root, a.str("to"))
			s.detail["from"], s.detail["to"] = from, to
			s.target = from + " -> " + to
			s.paths = []string{from, to}
			break
		}
		p := relativeTo(root, a.path())
		s.target = p
		s.detail["path"] = p
		if p != "" {
			s.paths = []string{p}
		}
		if n := a.num("start_line"); n > 0 {
			s.detail["start_line"] = n
			s.detail["end_line"] = a.num("end_line")
		}
	case domain.TraceCategoryExec:
		cmd := Scrub(domain.TruncateHead(a.str(commandKeys...), maxCommandBytes))
		s.target = firstLine(cmd, 160)
		s.detail["command"] = cmd
		cwd := relativeTo(root, a.str("working_dir", "cwd", "workdir"))
		if cwd == "" {
			cwd = "."
		}
		s.detail["cwd"] = cwd
		if shellExternal.MatchString(a.str(commandKeys...)) {
			s.effect = EffectShellOuter
		}
	case domain.TraceCategoryExternal:
		summarizeExternal(&s, tool, native, a)
	default:
		summarizeBoard(&s, tool, a)
	}
	if s.effect != "" {
		s.detail["effect"] = s.effect
	}
	return s
}

func changeOp(tool, native string) string {
	switch tool {
	case "write_file":
		return "write"
	case "edit_file", "edit_lines":
		return "edit"
	case "delete_file":
		return "delete"
	case "move_file":
		return "move"
	}
	return strings.ToLower(native)
}

func summarizeExternal(s *summary, tool, native string, a argMap) {
	name := tool
	if name == "" {
		name = native
	}
	if strings.HasPrefix(name, "mcp__") || strings.HasPrefix(native, "mcp__") {
		full := native
		if full == "" || !strings.HasPrefix(full, "mcp__") {
			full = name
		}
		parts := strings.SplitN(strings.TrimPrefix(full, "mcp__"), "__", 2)
		s.detail["mcp_server"] = parts[0]
		if len(parts) == 2 {
			s.detail["mcp_tool"] = parts[1]
		}
		s.target = parts[0]
		if host := hostOf(a.str(urlKeys...)); host != "" {
			s.detail["host"] = host
			s.target = host
		}
		s.effect = EffectMCP
		return
	}
	rawURL := a.str(urlKeys...)
	if host := hostOf(rawURL); host != "" {
		s.detail["host"] = host
		s.detail["url"] = urlWithoutSecrets(rawURL)
		s.target = host
	}
	switch {
	case name == "http_request":
		method := strings.ToUpper(a.str("method"))
		if method == "" {
			method = "GET"
		}
		s.detail["method"] = method
		if method != "GET" && method != "HEAD" && method != "OPTIONS" {
			s.effect = EffectHTTP
		}
	case browserActions[name] || strings.HasPrefix(name, "browser_") && !strings.Contains(name, "screenshot") && !strings.Contains(name, "read") && !strings.Contains(name, "wait") && !strings.Contains(name, "viewport"):
		s.effect = EffectBrowser
		if s.target == "" {
			s.target = "browser"
		}
	case deviceActions[name]:
		s.effect = EffectDevice
		s.target = "device"
	case name == "web_search":
		s.detail["query_chars"] = len(a.str("query"))
		if s.target == "" {
			s.target = "web search"
		}
	}
	if eff, ok := prEffects[name]; ok {
		s.effect = eff
	}
	if s.target == "" {
		s.target = name
	}
}

var idKey = regexp.MustCompile(`(^|_)(id|key)$`)

func summarizeBoard(s *summary, tool string, a argMap) {
	ids := map[string]string{}
	for k := range a {
		if !idKey.MatchString(k) || len(ids) >= maxIDs {
			continue
		}
		var v string
		if json.Unmarshal(a[k], &v) == nil && v != "" && len(v) <= 80 {
			ids[k] = v
		}
	}
	if len(ids) > 0 {
		s.detail["ids"] = ids
	}
	for _, k := range []string{"task_id", "task_key", "id", "criterion_id"} {
		if v, ok := ids[k]; ok {
			s.target = v
			break
		}
	}
	switch {
	case boardNotes[tool]:
		s.effect = EffectBoardNote
	case boardMutations[tool]:
		s.effect = EffectBoard
	}
	if s.target == "" {
		s.target = tool
	}
}

// resultIDs lifts the ids a board call returned (the task it created) out of
// its JSON answer without keeping any of the answer.
func resultIDs(content string) map[string]string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "{") {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &m) != nil {
		return nil
	}
	out := map[string]string{}
	for _, k := range []string{"id", "task_id", "key", "task_key", "comment_id"} {
		var v string
		if raw, ok := m[k]; ok && json.Unmarshal(raw, &v) == nil && v != "" && len(v) <= 80 {
			out[k] = v
		}
	}
	return out
}

var (
	exitStatus = regexp.MustCompile(`(?i)exit status (\d+)`)
	exitCode   = regexp.MustCompile(`(?i)^\s*exit code:?\s*(\d+)`)
)

func execOutcome(content string, isError bool) (code *int, output string) {
	output = content
	if strings.HasPrefix(content, "exit error:") || strings.HasPrefix(content, "command timed out") {
		if j := strings.Index(content, "output:\n"); j >= 0 {
			output = content[j+len("output:\n"):]
		}
	}
	if !isError {
		zero := 0
		return &zero, output
	}
	for _, re := range []*regexp.Regexp{exitCode, exitStatus} {
		if m := re.FindStringSubmatch(content); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return &n, output
			}
		}
	}
	return nil, output
}

// edges keeps the first and last lines of an output, scrubbed.
func edges(output string) (head, tail []string, total int) {
	output = strings.TrimRight(Scrub(output), "\r\n ")
	if output == "" {
		return nil, nil, 0
	}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	total = len(lines)
	cut := func(in []string) []string {
		out := make([]string, len(in))
		for i, l := range in {
			out[i] = domain.TruncateHead(l, maxLineBytes)
		}
		return out
	}
	if total <= 2*outputEdgeLines {
		return cut(lines), nil, total
	}
	return cut(lines[:outputEdgeLines]), cut(lines[total-outputEdgeLines:]), total
}
