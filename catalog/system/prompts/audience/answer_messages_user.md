---
key: audience.answer_messages_user
version: 1
inputs: [Background, Lines, Questions]
---
You are shown {{len .Lines}} candidate lines (headlines or taglines) for the same thing. You see only the words; there is no page around them.
{{- if .Background}}

Background on what the lines are for (you would not know this from the words alone):
{{.Background}}
{{- end}}

The lines:
{{range .Lines}}
Line {{.N}}: {{.Text}}
{{- end}}

Rate every line from 0 to 10 on each of these, as this person, from the words alone:
- "clarity": do you understand what is being offered?
- "relevance": does it speak to your situation and needs?
- "credibility": do you believe it?
- "appeal": would it make you want to know more or act?
Then give "best", the number of the line that lands best with you, "best_reason" in one or two sentences, and "stronger_line": one line you would write that would land better with you than any of these (empty only if none could be better).

Judge the words, not the idea behind them. Be a realistic reader: most lines are forgettable, and a line that sounds clever but says nothing is a low clarity score.
{{- if .Questions}}

Also answer these questions, each in a sentence or two, in "answers" (use the question text as given):
{{join "\n" .Questions}}
{{- end}}

Reply with only a JSON object, no prose and no code fence, with one entry in "ratings" per line:
{"ratings":[{"line":1,"clarity":0,"relevance":0,"credibility":0,"appeal":0,"comment":""}],"best":1,"best_reason":"","stronger_line":"","quote":"","answers":[{"question":"","answer":""}]}
