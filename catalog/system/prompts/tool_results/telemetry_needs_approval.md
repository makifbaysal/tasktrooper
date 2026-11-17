---
key: tool_results.telemetry_needs_approval
version: 1
inputs: [Action]
---
Nothing was written. `{{.Action}}` changes the user's telemetry, so it acts only after the user said yes to this exact change in this conversation. Show them what `would_send` holds (and the preview, if there is one) in plain words, ask (with ask_user when you are working a card), and call again with the same arguments and `user_approved: true` only once they answered yes. A yes to an earlier, different change is not approval.
