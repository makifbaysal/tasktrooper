---
key: audience.answer_ab_user
version: 2
inputs: [First, Second, Intent, Questions]
---
You are shown two versions of a web page for the same thing, one after the other. Each is given as plain text. You came with this intent: {{.Intent}}

Version 1:
---
{{.First}}
---

Version 2:
---
{{.Second}}
---

Judge only the text above. First score each version on its own, as if you had seen only that one, with the same questions for both. Do not let one version colour your answers about the other; only after both scorecards do you say which you prefer.

For each version, answer these, as yourself:
1. What is your first impression, in one or two sentences? Give "first_impression".
2. How clear is the offer: what it is, who it is for, what you get? Give "clarity_score" from 0 (no idea) to 10 (instantly clear).
3. Given your intent, would you take the next step the page asks for? Give "intent_score" from 0 (never) to 10 (certainly) and a "verdict" of "yes", "maybe" or "no".
4. What confused you or put you off? Give "objections" as a list of short phrases.
5. What is missing for you to act? Give "missing" as a list of short phrases.
6. Which things made you trust it, if any? Give "trust_signals" as a list of short phrases (empty when none).
7. If you could change one thing, what would it be? Give "change".

Then give "preferred" as 1 or 2 (there is no tie), "preference_reason" in one or two sentences, and one sentence about your choice exactly as you would say it to a colleague in "quote".
{{- if .Questions}}

Also answer these questions, each in a sentence or two, in "answers" (use the question text as given):
{{join "\n" .Questions}}
{{- end}}

Reply with only a JSON object, no prose and no code fence:
{"page_1":{"first_impression":"","clarity_score":0,"intent_score":0,"verdict":"yes|maybe|no","objections":[""],"missing":[""],"trust_signals":[""],"change":""},"page_2":{"first_impression":"","clarity_score":0,"intent_score":0,"verdict":"yes|maybe|no","objections":[""],"missing":[""],"trust_signals":[""],"change":""},"preferred":1,"preference_reason":"","quote":"","answers":[{"question":"","answer":""}]}
