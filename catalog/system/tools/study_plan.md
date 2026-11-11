---
key: tool.study_plan
version: "1"
params:
    audience_id: 'The id of a ready audience.'
    currency: 'A three-letter currency code, default USD.'
    goal: 'What the study should settle, in a sentence.'
    price_points: 'Prices every person reacts to (would they buy at this price), at most 6.'
    questions: 'Extra open questions asked of everyone, at most 5.'
    title: 'A short title; defaults to the audience name.'
---
Plan a study on a ready audience: the questions to ask and the prices to test. It only saves the plan and returns the number of model calls (one per person plus the report); nothing runs yet. Every person is always asked the core purchase questions: would they buy it as it is today (0-10 and yes, maybe or no), the most they would pay, what is missing, what holds them back, what they use instead. Add `questions` only for extra open questions (at most 5), and `price_points` for prices every person must react to (at most 6). Show the user the plan and the call count, then ask before study_start.
