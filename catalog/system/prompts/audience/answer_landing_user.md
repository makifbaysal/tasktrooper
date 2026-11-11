---
key: audience.answer_landing_user
version: 1
inputs: [Page, Intent, Questions]
---
You have just opened a web page. This is everything on it, as plain text:

---
{{.Page}}
---

You came to this page with this intent: {{.Intent}}

Judge only what is written above. Do not assume features, proof or pricing the text does not show, and do not reward what the page implies it might become. React as this person would on a first visit: skim, decide, leave or act.

Answer these, as yourself:
1. What is your first impression, in one or two sentences? Give "first_impression".
2. How clear is the offer: what it is, who it is for, what you get? Give "clarity_score" from 0 (no idea) to 10 (instantly clear).
3. Given your intent, would you take the next step the page asks for? Give "intent_score" from 0 (never) to 10 (certainly), a "verdict" of "yes", "maybe" or "no", whether you "would_act_now", and your "reasoning" in one or two sentences.
4. What confused you or put you off? Give "objections" as a list of short phrases.
5. What is missing for you to act? Give "missing" as a list of short phrases.
6. Which things on the page made you trust it, if any? Give "trust_signals" as a list of short phrases (empty when none).
7. If you could change one thing on the page, what would it be? Give "change".
8. Say one sentence about this page exactly as you would say it to a colleague, in "quote".
{{- if .Questions}}

Also answer these questions, each in a sentence or two, in "answers" (use the question text as given):
{{join "\n" .Questions}}
{{- end}}

Reply with only a JSON object, no prose and no code fence:
{"first_impression":"","clarity_score":0,"intent_score":0,"verdict":"yes|maybe|no","would_act_now":false,"reasoning":"","objections":[""],"missing":[""],"trust_signals":[""],"change":"","quote":"","answers":[{"question":"","answer":""}]}
