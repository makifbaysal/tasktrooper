---
key: tool.audience_build
version: "1"
params:
    audience_id: 'The audience id.'
    user_approved: 'true only after the user said yes to building this audience in this conversation.'
---
Write the audience's people from its brief, in the background (it writes them again if they exist). It returns at once and takes a few minutes. This spends model calls on the user's own agent runtime, so show the user the brief and the number of calls and ask first; call it with `user_approved: true` only after they said yes. Then tell them it is building and check audience_get later; never wait in a loop.
