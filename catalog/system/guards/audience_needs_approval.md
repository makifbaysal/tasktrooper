---
key: guard.audience_needs_approval
version: 1
inputs: [Action, Calls]
---
`{{.Action}}` would spend about {{.Calls}} model calls on the user's own agent runtime (their subscription or API budget), so it only runs after the user said yes in this conversation. Show them what will run (the audience brief or the study's questions and prices, who it asks) and the number of calls, ask, and call it again with `user_approved: true` only once they answered yes. A yes to an earlier, different step is not approval.
