---
key: tool.declare_env_vars
version: "1"
params:
    task_id: Board task UUID or its board key (e.g. "T-1"). Optional in a run that is already about one task — omit it there and the task in context is used.
    vars: Every environment variable your change makes the deployed app read — new, renamed, or newly required. One entry each.
    vars.items.properties.name: The variable's name exactly as the code reads it (e.g. SESSION_SECRET).
    vars.items.properties.kind: 'value: a non-secret literal you know now (owner/repo, a branch name, a public URL) — put it in value. generated: a random secret nobody has to know (a session, signing or encryption key). human_secret: a credential only a person can obtain (an API key, a personal access token). human_bcrypt: the bcrypt hash of a password a person chooses (the app compares a password against it). optional: the deployed app runs without it — leave it unset there.'
    vars.items.properties.value: Only for kind=value, and only a non-secret. Never put a key, token, password or hash here.
    vars.items.properties.description: What the app uses it for, in one line — a person reads it when entering the value.
---
Declare the environment variables your change needs in its deployed environment, in the same run that adds the code reading them. Before the task ships, TaskTrooper sets value and generated variables on the deploy target itself and asks a human on the task for human_secret and human_bcrypt ones; the merge waits until every non-optional variable is set. You never see, choose or handle a secret value — so never generate, write, commit or comment one, and do not ask the human for it yourself. Keep .env.example in step: an undeclared name found there still blocks the ship until a human classifies it. Re-declaring a name updates it; a human's own classification of a name is kept.
