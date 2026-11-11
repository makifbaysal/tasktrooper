---
key: tool.study_plan
version: "2"
params:
    audience_id: 'The id of a ready audience.'
    currency: 'A three-letter currency code, default USD (user_interviews only).'
    goal: 'What the study should settle, in a sentence.'
    intent: 'landing_page_test and ab_test: what the visitor came to do (evaluate, buy, compare, sign up, or any short phrase). Default: evaluate.'
    kind: 'user_interviews (default), landing_page_test, message_testing or ab_test.'
    pages: 'landing_page_test: exactly one page; ab_test: exactly two pages (A then B). Each is {url} (the text is read from it) or {copy} (the page text pasted).'
    price_points: 'user_interviews: prices every person reacts to (would they buy at this price), at most 6.'
    questions: 'Your own open questions, at most 5. Giving them means no interview script is written for the study.'
    title: 'A short title; defaults to the audience name and the kind.'
    variants: 'message_testing: 2 to 5 different candidate lines (headlines, taglines or messages).'
---
Plan a study on a ready audience and write its interview script. Four kinds: `user_interviews` (would they buy the product as it is today: score, the most they would pay, what is missing, what holds them back, what they use instead), `landing_page_test` (each person reads one page's text as a first-time visitor with the given intent: first impression, clarity, would they act, frictions, trust signals, the one change they would make), `message_testing` (each person rates 2-5 candidate lines on clarity, relevance, credibility and appeal, picks the best and writes a stronger one) and `ab_test` (every person evaluates both pages, one after the other in a balanced order, scoring each on its own with the same scorecard, and then says which they prefer). Pick the kind from what the stakeholder wants to learn, collect that kind's inputs first, then call this.

Planning starts one model call in the background: the person's agent writes the interview script, grouped open questions that every person will be asked (skipped when you pass `questions`). Nothing else is asked yet. It returns the study with `script_state` (generating, then ready) and `calls_estimate` (one call per person, the report and the script). Read the script with study_script once it is written, show the stakeholder the questions, the kind's inputs and the call count, let them change questions, and wait for their OK to this version of the script (study_script with approve) before study_start. Nothing starts on its own after the script is written; even questions you pass in `questions` wait for the approval. A URL page is read by the app (public pages only); if it cannot be read, ask the stakeholder to paste the text.
