---
key: guard.board_assignee_disabled
version: 2
inputs: [Agents, Role]
---
{{if .Role}}the role "{{.Role}}" is held only by agents that are switched off: {{join ", " .Agents}}.{{else}}{{join ", " .Agents}} is switched off and is not part of the team right now.{{end}} Do not assign this task to {{if .Role}}them{{else}}it{{end}} and do not pick a different agent that cannot do this work. Tell the stakeholder that adding {{if .Role}}one of these agents{{else}}this agent{{end}} to the team lets it do this work: they switch it on with the pencil ("Edit team") next to "Team" in the sidebar. Until then keep the task in backlog with no assignee.
