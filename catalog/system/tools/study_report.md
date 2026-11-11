---
key: tool.study_report
version: "2"
params:
    study_id: 'The study id.'
    user_approved: 'true only after the user said yes to writing the report. Needed only when the status is interviewed.'
---
The report of a finished study, per kind. All kinds carry the executive summary, the verdict score (0-100, computed from the people's own scores), results by segment, the recurring themes with how many people said each, verbatim quotes, recommendations, the method and the disclaimer. user_interviews adds the willingness-to-pay distribution, price tests, objections, needs and alternatives; landing_page_test the clarity score, frictions, what worked and the recommended edits; message_testing the ranking of the lines with their scores per segment, the winner, why and suggested rewrites; ab_test the A and B mean and median of every scorecard metric with the paired difference, the preference share overall and per segment, objections and quotes per version, why each is preferred or not, a confidence label and the recommendation; the differences are directional, with no p-values. Present it faithfully: lead with the verdict and what it rests on, give the numbers, name the top frictions, quote people, list the recommendations, and always say the findings come from synthetic personas, not real customers. The stakeholder can also open the study page and use Export as HTML to get one self-contained file.

When a study was started from the page, its status can be `interviewed`: the interviews are over and the report waits for a click. Calling this with `user_approved: true` (after the user said yes; it costs one model call) writes it; read it again a minute later.
