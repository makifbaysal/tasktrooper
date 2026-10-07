---
key: agent_loop.output_truncated
version: 1
inputs: [Limit]
---
[output limit] Your last reply reached the {{.Limit}}-token output limit while it was still writing a tool call, so the call was cut off and nothing in that reply was run. Do not send it again as one piece. Write large content in smaller parts: create the file with its first part, then add the rest with edit_lines (insert_after the last line) in further calls of a few hundred lines each. Keep each tool call well under the limit.
