---
key: audience.answer_user
version: 1
inputs: [Product, Context, Currency, PricePoints, Questions]
---
The product you are being asked about, as it stands today:
{{.Product}}
{{- if .Context}}

What the project actually contains right now:
{{.Context}}
{{- end}}

Answer these, as yourself:
1. Would you buy or pay for this product as it is today? Give "intent_score" from 0 (never) to 10 (certainly) and a "verdict" of "yes", "maybe" or "no", whether you "would_pay_now", and your "reasoning" in one or two sentences.
2. What is the most you would pay for it, in {{.Currency}}? Give "max_price" as a number (0 when you would not pay anything).
{{- if .PricePoints}}
3. Would you buy it at each of these prices ({{.Currency}})? Fill "price_reactions" with one entry per price: {{join ", " .PricePoints}}.
{{- end}}
4. What is missing for you to buy it, or to pay more? Give "missing" as a list of short phrases.
5. What holds you back or worries you? Give "objections" as a list of short phrases.
6. What would you use instead today? Give "alternatives" as a list of short phrases.
7. Say one sentence about it exactly as you would say it to a colleague, in "quote".
{{- if .Questions}}

Also answer these questions, each in a sentence or two, in "answers" (use the question text as given):
{{join "\n" .Questions}}
{{- end}}

Reply with only a JSON object, no prose and no code fence:
{"intent_score":0,"verdict":"yes|maybe|no","would_pay_now":false,"reasoning":"","max_price":0,"price_reactions":[{"price":0,"would_buy":false,"comment":""}],"missing":[""],"objections":[""],"alternatives":[""],"quote":"","answers":[{"question":"","answer":""}]}
