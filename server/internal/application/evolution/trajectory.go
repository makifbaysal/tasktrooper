package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// The reflection used to see a run as one status word and a 200-character summary; what went wrong lives in the steps. These are read from the run's recorded activity and given to the reflection as a short digest, and in full as files when the reflection is an agentic session that can open them.

const (
	runsInDigest   = 3
	runsInFiles    = 6
	stepsPerRun    = 250
	errorsPerRun   = 5
	finalMsgChars  = 600
	stepTextChars  = 1200
	stepArgsChars  = 400
	stepEventChars = 300
)

type trajectoryStep struct{ Tag, Text string }

type runEvidence struct {
	Title    string
	Status   string
	Request  string
	Feedback string
	Steps    []trajectoryStep
	Errors   []string
	Final    string
}

func (s *Service) recentRunEvidence(ctx context.Context, agentRec domain.Agent, reflection domain.AgentReflection, limit int) []runEvidence {
	if s.runs == nil || limit <= 0 {
		return nil
	}
	runs, err := s.runs.ListRecent(ctx, 200)
	if err != nil {
		return nil
	}
	revised := map[uuid.UUID]bool{}
	if s.perf != nil {
		if events, err := s.perf.EventsInWindow(ctx, agentRec.ID, reflection.WindowStart, reflection.WindowEnd); err == nil {
			for _, e := range events {
				if e.Delta < 0 && e.TaskID != nil {
					revised[*e.TaskID] = true
				}
			}
		}
	}
	var mine []domain.TaskAgentRun
	for _, r := range runs {
		if r.AgentID == agentRec.ID && !r.CreatedAt.Before(reflection.WindowStart) && !r.CreatedAt.After(reflection.WindowEnd) {
			mine = append(mine, r)
		}
	}
	troubled := func(r domain.TaskAgentRun) bool {
		return revised[r.TaskID] || r.Status == "failed" || r.ToolErrors > 0
	}
	sort.SliceStable(mine, func(i, j int) bool {
		if troubled(mine[i]) != troubled(mine[j]) {
			return troubled(mine[i])
		}
		return mine[i].CreatedAt.After(mine[j].CreatedAt)
	})
	var out []runEvidence
	for _, r := range mine {
		if len(out) >= limit {
			break
		}
		ev := runEvidence{Status: r.Status, Title: r.CreatedAt.Format("01-02 15:04")}
		if s.tasks != nil {
			if task, err := s.tasks.GetByID(ctx, r.TaskID); err == nil {
				ev.Title = strings.TrimSpace(task.Key + " " + task.Title)
				ev.Request = probeTaskRequestKey.Render(probeTaskRequestInput{
					Title: task.Title, Description: truncate(task.Description, 4000),
					TechnicalDescription: truncate(task.TechnicalDescription, 3000),
				})
			}
		}
		ev.Feedback = s.taskFeedback(ctx, agentRec, r.TaskID)
		if s.activity != nil && r.SessionRunID != nil {
			if steps, err := s.activity.ListStepsByRun(ctx, *r.SessionRunID); err == nil {
				ev.Steps, ev.Errors, ev.Final = renderSteps(steps)
			}
		}
		if ev.Final == "" {
			ev.Final = truncate(strings.TrimSpace(r.Summary), finalMsgChars)
		}
		out = append(out, ev)
	}
	return out
}

func renderSteps(steps []domain.SessionStep) ([]trajectoryStep, []string, string) {
	if len(steps) > stepsPerRun {
		steps = steps[len(steps)-stepsPerRun:]
	}
	var out []trajectoryStep
	var errs []string
	final := ""
	for _, st := range steps {
		var p map[string]any
		_ = json.Unmarshal(st.Payload, &p)
		str := func(k string) string {
			if v, ok := p[k].(string); ok {
				return v
			}
			return ""
		}
		switch st.StepType {
		case "assistant_message":
			text := strings.TrimSpace(str("content"))
			if text == "" {
				continue
			}
			final = truncate(text, finalMsgChars)
			out = append(out, trajectoryStep{Tag: "ASSISTANT", Text: truncate(text, stepTextChars)})
		case "user_message":
			out = append(out, trajectoryStep{Tag: "USER", Text: truncate(str("content"), stepTextChars)})
		case "tool_call_start":
			out = append(out, trajectoryStep{Tag: "ACTION", Text: str("tool") + " " + truncate(str("arguments"), stepArgsChars)})
		case "tool_call_result":
			isErr, _ := p["is_error"].(bool)
			text := str("tool") + ": " + truncate(str("content"), stepTextChars)
			if isErr {
				out = append(out, trajectoryStep{Tag: "ERROR", Text: text})
				if len(errs) < errorsPerRun {
					errs = append(errs, str("tool")+": "+truncate(strings.TrimSpace(str("content")), 200))
				}
				continue
			}
			out = append(out, trajectoryStep{Tag: "OBSERVATION", Text: text})
		case "iteration_start", "llm_request", "budget_warning", "token_budget_warning", "tool_calls_planned", "empty_turn_retry":
		default:
			out = append(out, trajectoryStep{Tag: "EVENT", Text: st.StepType + " " + truncate(compactJSON(st.Payload), stepEventChars)})
		}
	}
	return out, errs, final
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

type evidenceTrajectoryRun struct {
	Title, Status, Final string
	Errors               []string
}

type evidenceTrajectoriesInput struct {
	Runs  []evidenceTrajectoryRun
	Files bool
}

type trajectoryFileInput struct {
	Title, Status, Request, Feedback string
	Steps                            []trajectoryStep
}

type evidenceCatalogFileInput struct {
	Kind, ID, Name, Description string
	Enabled                     bool
	Priority                    int
	Content                     string
}

var (
	evidenceTrajectoriesKey = prompt.Define("evolution.evidence_trajectories", evidenceTrajectoriesInput{
		Runs: []evidenceTrajectoryRun{{
			Title: "ORD-12 Paginate orders", Status: "completed", Final: "Opened the PR.",
			Errors: []string{"run_terminal: exit status 1"},
		}},
		Files: true,
	})
	trajectoryFileKey = prompt.Define("evolution.trajectory_file", trajectoryFileInput{
		Title: "ORD-12 Paginate orders", Status: "completed", Request: "Task: Paginate orders",
		Feedback: "- the page size must be configurable",
		Steps:    []trajectoryStep{{Tag: "ACTION", Text: "read_file {\"path\":\"orders.go\"}"}, {Tag: "OBSERVATION", Text: "read_file: package orders"}},
	})
	evidenceCatalogFileKey = prompt.Define("evolution.evidence_catalog_file", evidenceCatalogFileInput{
		Kind: "skill", ID: "11111111-1111-1111-1111-111111111111", Name: "db-migrations",
		Description: "When a change touches the schema, load this.", Enabled: true, Content: "Write a down migration.",
	})
)

func evidenceTrajectoriesSection(runs []runEvidence, files bool) string {
	if len(runs) == 0 {
		return ""
	}
	in := evidenceTrajectoriesInput{Files: files}
	for i, r := range runs {
		if i >= runsInDigest {
			break
		}
		in.Runs = append(in.Runs, evidenceTrajectoryRun{Title: r.Title, Status: r.Status, Final: r.Final, Errors: r.Errors})
	}
	return evidenceTrajectoriesKey.Render(in)
}

var unsafeFileChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func fileSlug(s string) string {
	slug := strings.Trim(unsafeFileChars.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-.")
	if len(slug) > 60 {
		slug = slug[:60]
	}
	if slug == "" {
		slug = "item"
	}
	return slug
}

// The evidence an agentic reflection can open instead of reading a 24k summary: the report in full, every skill and rule as it stands, and the recent runs step by step. Keyed by slash-separated relative path, so it can seed a CLI's scratch directory or a local one alike.
func evidenceFiles(report string, skills []domain.Skill, rules []domain.OrchestratorRule, runs []runEvidence) map[string]string {
	files := map[string]string{"EVIDENCE.md": report}
	for i, sk := range skills {
		files[fmt.Sprintf("skills/%03d-%s.md", i+1, fileSlug(sk.Name))] = evidenceCatalogFileKey.Render(evidenceCatalogFileInput{
			Kind: "skill", ID: sk.ID.String(), Name: sk.Name, Description: sk.Description, Enabled: sk.Enabled, Content: sk.Content,
		})
	}
	for i, r := range rules {
		files[fmt.Sprintf("rules/%03d-%s.md", i+1, fileSlug(r.Name))] = evidenceCatalogFileKey.Render(evidenceCatalogFileInput{
			Kind: "rule", ID: r.ID.String(), Name: r.Name, Enabled: r.Enabled, Priority: r.Priority, Content: r.Content,
		})
	}
	for i, r := range runs {
		files[fmt.Sprintf("runs/%02d-%s.md", i+1, fileSlug(r.Title))] = trajectoryFileKey.Render(trajectoryFileInput{
			Title: r.Title, Status: r.Status, Request: r.Request, Feedback: r.Feedback, Steps: r.Steps,
		})
	}
	return files
}

func writeEvidenceDir(dir string, files map[string]string) error {
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}
