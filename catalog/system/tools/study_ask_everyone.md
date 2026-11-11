---
key: tool.study_ask_everyone
version: "1"
params:
    question: 'The question for everyone.'
    study_id: 'The study id.'
    user_approved: 'true only after the user said yes to asking everyone this question in this conversation.'
---
Ask every person of a finished study the same follow-up question, in the background. This spends one model call per person, so show the user the question and the number of people and ask first; call it with `user_approved: true` only after they said yes. Read the answers later in study_status under follow_ups.
