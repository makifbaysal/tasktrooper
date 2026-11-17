---
key: tool.telemetry_accept_suggestion
version: "1"
params:
    suggestion_id: 'The suggestion id from telemetry_list_monitors.'
    threshold: 'Replace the drafted threshold.'
    severity: 'Replace the drafted severity.'
    channels: 'Replace the drafted notification targets.'
    user_approved: 'true only after the user said yes to accepting this suggestion in this conversation.'
---
Turn an alert suggestion telemetry's code analysis drafted into a monitor, optionally overriding its threshold, severity or channels. Without user_approved nothing is written: the answer shows the suggestion and exactly what would be sent. Show it to the user, ask, and call again with `user_approved: true` once they said yes.
