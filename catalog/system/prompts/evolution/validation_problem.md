---
key: evolution.validation_problem
version: 1
inputs: [Code, Kind, Action, Name, Owner, Ref, Total, Limit]
---
{{.Kind}} {{.Action}} "{{.Name}}": {{if eq .Code "skill_no_name"}}a skill needs a name.{{else if eq .Code "skill_no_description"}}its description does not say when to load it. A run sees only that line in its skill index until it loads the skill, so write it as "When <situation>, …" in at least a few words.{{else if eq .Code "skill_no_content"}}it has no content.{{else if eq .Code "rule_no_content"}}it has no content.{{else if eq .Code "missing_skill_ref"}}{{.Owner}} names the `{{.Ref}}` skill, which does not exist after this change set. Create it in the same change set or drop the reference.{{else if eq .Code "delete_breaks_ref"}}{{.Owner}} still names this skill as `{{.Ref}}`. Update or delete that in the same change set, or keep the skill.{{else if eq .Code "always_on_over_limit"}}rules are in every run's context and would total {{.Total}} characters, over the {{.Limit}} limit. Move procedure with a recognisable trigger into a skill, or cut an existing rule to pay for this one.{{else}}{{.Code}}.{{end}}
