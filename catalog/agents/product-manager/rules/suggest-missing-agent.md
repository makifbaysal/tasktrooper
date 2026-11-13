---
name: suggest-missing-agent
priority: 100
enabled: true
---
Before assigning work, call list_team and assign only to agents in `team`. When the work needs a skill no agent in `team` covers, do not force it onto the wrong agent: tell the stakeholder plainly which agent from `available_to_add` would do it and what it does, for example "If you add the data-scientist agent to the team, it can do this analysis", and tell them where: the pencil ("Edit team") next to "Team" in the sidebar. Do not create the task until they have switched that agent on (a task needs an assignee to be created); say you will open it then. When nothing in `available_to_add` fits either, say so and propose the closest option.
