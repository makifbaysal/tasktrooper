package audience

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/audience"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	audienceListName   = "audience_list"
	audienceGetName    = "audience_get"
	audienceCreateName = "audience_create"
	audienceUpdateName = "audience_update"
	audienceBuildName  = "audience_build"
	audienceTalkName   = "audience_talk"
	studyListName      = "study_list"
	studyStatusName    = "study_status"
	studyReportName    = "study_report"
	studyPlanName      = "study_plan"
	studyStartName     = "study_start"
	studyCancelName    = "study_cancel"
	studyFollowUpName  = "study_follow_up"
	studyAskAllName    = "study_ask_everyone"
	studyAskName       = "study_ask_research"
)

func init() {
	for _, name := range ToolNames() {
		prompt.Define[struct{}]("tool."+name, struct{}{})
	}
}

var segmentsSchema = map[string]interface{}{
	"type": "array",
	"items": objectSchema(map[string]interface{}{
		"name": stringSchema(), "description": stringSchema(), "count": intSchema(),
	}, "name"),
}

func briefProps() map[string]interface{} {
	return map[string]interface{}{
		"product":           stringSchema(),
		"segments":          segmentsSchema,
		"locations":         stringSchema(),
		"current_solutions": stringSchema(),
		"pricing":           stringSchema(),
		"topic":             stringSchema(),
		"language":          stringSchema(),
		"persona_count":     intSchema(),
		"repository":        stringSchema(),
	}
}

func withProps(base, extra map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// briefOf reads the brief fields out of a tool call; a nil result means none
// of them was given.
func briefOf(args map[string]any, base domain.AudienceBrief) (domain.AudienceBrief, bool) {
	changed := false
	set := func(dst *string, key string) {
		if _, ok := args[key]; ok {
			*dst = argString(args, key)
			changed = true
		}
	}
	set(&base.Product, "product")
	set(&base.Locations, "locations")
	set(&base.CurrentSolutions, "current_solutions")
	set(&base.Pricing, "pricing")
	set(&base.Topic, "topic")
	set(&base.Language, "language")
	if segs, ok := argSegments(args, "segments"); ok {
		base.Segments = segs
		changed = true
	}
	return base, changed
}

// personaView is a person as the agent needs them: enough to choose who to
// talk to, not the whole sheet.
type personaView struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Segment  string    `json:"segment,omitempty"`
	Role     string    `json:"role,omitempty"`
	Location string    `json:"location,omitempty"`
	Bio      string    `json:"bio,omitempty"`
	Quote    string    `json:"quote,omitempty"`
}

type studyView struct {
	domain.Study
	ReportReady bool `json:"report_ready"`
}

func viewOf(s domain.Study) studyView {
	v := studyView{Study: s, ReportReady: s.Report != nil || s.Status == domain.StudyDone}
	v.Report = nil
	return v
}

func tools() []*tool {
	return []*tool{
		{name: audienceListName, params: objectSchema(map[string]interface{}{}),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				list, err := k.Service.ListAudiences(ctx)
				return map[string]any{"audiences": list}, err
			}},
		{name: audienceGetName, params: objectSchema(map[string]interface{}{"audience_id": stringSchema()}, "audience_id"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "audience_id")
				if err != nil {
					return nil, err
				}
				d, err := k.Service.Audience(ctx, id)
				if err != nil {
					return nil, err
				}
				people := make([]personaView, 0, len(d.Personas))
				for _, p := range d.Personas {
					people = append(people, personaView{ID: p.ID, Name: p.Name, Segment: p.Segment, Role: p.Role, Location: p.Location, Bio: p.Bio, Quote: p.Quote})
				}
				studies := make([]studyView, 0, len(d.Studies))
				for _, s := range d.Studies {
					studies = append(studies, viewOf(s))
				}
				return map[string]any{"audience": d.Audience, "brief": d.Brief, "personas": people, "studies": studies, "build_calls": d.BuildCalls}, nil
			}},
		{name: audienceCreateName, params: objectSchema(withProps(briefProps(), map[string]interface{}{"name": stringSchema()}), "name", "product"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				brief, _ := briefOf(a, domain.AudienceBrief{})
				in := audience.CreateAudienceInput{Name: argString(a, "name"), Repository: argString(a, "repository"), Brief: brief}
				if n, ok := argInt(a, "persona_count"); ok {
					in.PersonaTarget = n
				}
				created, err := k.Service.CreateAudience(ctx, in)
				if err != nil {
					return nil, err
				}
				return map[string]any{"audience": created, "brief": created.Brief, "build_calls": audience.BuildCalls(created.PersonaTarget)}, nil
			}},
		{name: audienceUpdateName, params: objectSchema(withProps(briefProps(), map[string]interface{}{"audience_id": stringSchema(), "name": stringSchema()}), "audience_id"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "audience_id")
				if err != nil {
					return nil, err
				}
				cur, err := k.Service.Audience(ctx, id)
				if err != nil {
					return nil, err
				}
				in := audience.UpdateAudienceInput{}
				if _, ok := a["name"]; ok {
					n := argString(a, "name")
					in.Name = &n
				}
				if _, ok := a["repository"]; ok {
					r := argString(a, "repository")
					in.Repository = &r
				}
				if brief, changed := briefOf(a, cur.Brief); changed {
					in.Brief = &brief
				}
				if n, ok := argInt(a, "persona_count"); ok {
					in.PersonaTarget = &n
				}
				updated, err := k.Service.UpdateAudience(ctx, id, in)
				if err != nil {
					return nil, err
				}
				note := "The brief changed; the people already written stay until audience_build writes them again."
				return map[string]any{"audience": updated, "brief": updated.Brief, "build_calls": audience.BuildCalls(updated.PersonaTarget), "note": note}, nil
			}},
		{name: audienceBuildName,
			params: objectSchema(map[string]interface{}{"audience_id": stringSchema(), "user_approved": boolSchema()}, "audience_id", "user_approved"),
			approvalCalls: func(ctx context.Context, k *ToolKit, a map[string]any) (int, error) {
				id, err := argID(a, "audience_id")
				if err != nil {
					return 0, err
				}
				d, err := k.Service.Audience(ctx, id)
				return d.BuildCalls, err
			},
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, _ := argID(a, "audience_id")
				started, err := k.Service.Build(ctx, id)
				if err != nil {
					return nil, err
				}
				return map[string]any{"audience": started, "build_calls": audience.BuildCalls(started.PersonaTarget)}, nil
			}},
		{name: audienceTalkName, params: objectSchema(map[string]interface{}{"persona_id": stringSchema(), "message": stringSchema()}, "persona_id", "message"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "persona_id")
				if err != nil {
					return nil, err
				}
				reply, err := k.Service.Chat(ctx, id, argString(a, "message"))
				if err != nil {
					return nil, err
				}
				return map[string]any{"reply": reply.Content}, nil
			}},
		{name: studyListName, params: objectSchema(map[string]interface{}{"audience_id": stringSchema()}),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				var filter *uuid.UUID
				if argString(a, "audience_id") != "" {
					id, err := argID(a, "audience_id")
					if err != nil {
						return nil, err
					}
					filter = &id
				}
				list, err := k.Service.ListStudies(ctx, filter)
				if err != nil {
					return nil, err
				}
				views := make([]studyView, 0, len(list))
				for _, s := range list {
					views = append(views, viewOf(s))
				}
				return map[string]any{"studies": views}, nil
			}},
		{name: studyStatusName, params: objectSchema(map[string]interface{}{"study_id": stringSchema()}, "study_id"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "study_id")
				if err != nil {
					return nil, err
				}
				d, err := k.Service.Study(ctx, id)
				if err != nil {
					return nil, err
				}
				return map[string]any{"study": viewOf(d.Study), "running": d.Running, "follow_ups": d.FollowUps}, nil
			}},
		{name: studyReportName, params: objectSchema(map[string]interface{}{"study_id": stringSchema()}, "study_id"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "study_id")
				if err != nil {
					return nil, err
				}
				d, err := k.Service.Study(ctx, id)
				if err != nil {
					return nil, err
				}
				if d.Report == nil {
					return map[string]any{"study": viewOf(d.Study), "report": nil, "note": "There is no report yet."}, nil
				}
				return map[string]any{"title": d.Title, "audience": d.AudienceName, "report": d.Report}, nil
			}},
		{name: studyPlanName, params: objectSchema(map[string]interface{}{
			"audience_id": stringSchema(), "title": stringSchema(), "goal": stringSchema(), "questions": stringList(),
			"price_points": numberList(), "currency": stringSchema(),
		}, "audience_id"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "audience_id")
				if err != nil {
					return nil, err
				}
				st, err := k.Service.PlanStudy(ctx, id, audience.PlanStudyInput{
					Title: argString(a, "title"), Goal: argString(a, "goal"), Questions: argStrings(a, "questions"),
					PricePoints: argNumbers(a, "price_points"), Currency: argString(a, "currency"),
				})
				if err != nil {
					return nil, err
				}
				return viewOf(st), nil
			}},
		{name: studyStartName,
			params: objectSchema(map[string]interface{}{"study_id": stringSchema(), "user_approved": boolSchema()}, "study_id", "user_approved"),
			approvalCalls: func(ctx context.Context, k *ToolKit, a map[string]any) (int, error) {
				id, err := argID(a, "study_id")
				if err != nil {
					return 0, err
				}
				d, err := k.Service.Study(ctx, id)
				return d.CallsEstimate, err
			},
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, _ := argID(a, "study_id")
				st, err := k.Service.StartStudy(ctx, id)
				if err != nil {
					return nil, err
				}
				return viewOf(st), nil
			}},
		{name: studyCancelName, params: objectSchema(map[string]interface{}{"study_id": stringSchema()}, "study_id"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "study_id")
				if err != nil {
					return nil, err
				}
				if err := k.Service.CancelStudy(ctx, id); err != nil {
					return nil, err
				}
				return map[string]any{"cancelled": true}, nil
			}},
		{name: studyFollowUpName, params: objectSchema(map[string]interface{}{"study_id": stringSchema(), "persona_id": stringSchema(), "question": stringSchema()}, "study_id", "persona_id", "question"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				studyID, err := argID(a, "study_id")
				if err != nil {
					return nil, err
				}
				personaID, err := argID(a, "persona_id")
				if err != nil {
					return nil, err
				}
				reply, err := k.Service.FollowUp(ctx, studyID, personaID, argString(a, "question"))
				if err != nil {
					return nil, err
				}
				return map[string]any{"reply": reply.Content}, nil
			}},
		{name: studyAskAllName,
			params: objectSchema(map[string]interface{}{"study_id": stringSchema(), "question": stringSchema(), "user_approved": boolSchema()}, "study_id", "question", "user_approved"),
			approvalCalls: func(ctx context.Context, k *ToolKit, a map[string]any) (int, error) {
				id, err := argID(a, "study_id")
				if err != nil {
					return 0, err
				}
				return k.Service.FollowUpCalls(ctx, id)
			},
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, _ := argID(a, "study_id")
				group, calls, err := k.Service.AskEveryone(ctx, id, argString(a, "question"))
				if err != nil {
					return nil, err
				}
				return map[string]any{"group_id": group, "calls": calls, "note": "Asking in the background; read the answers in study_status under follow_ups."}, nil
			}},
		{name: studyAskName, params: objectSchema(map[string]interface{}{"study_id": stringSchema(), "question": stringSchema()}, "study_id", "question"),
			run: func(ctx context.Context, k *ToolKit, a map[string]any) (any, error) {
				id, err := argID(a, "study_id")
				if err != nil {
					return nil, err
				}
				answer, err := k.Service.AskResearch(ctx, id, argString(a, "question"))
				if err != nil {
					return nil, err
				}
				return map[string]any{"answer": answer}, nil
			}},
	}
}
