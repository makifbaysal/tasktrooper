---
key: tool.record_test_cases
version: "1"
params:
    cases.items.properties.actual: 'Required for failed: what actually happened.'
    cases.items.properties.category: Which dimension this case covers.
    cases.items.properties.criterion_id: 'Optional: the acceptance criterion this case exercises. Leave empty for a case no criterion states — those are the ones worth writing down.'
    cases.items.properties.evidence: Command + output, request/response, or — for a screenshot — the url and viewport browser_screenshot reported and what it showed, plus the file name it was saved under when you passed attach_to_task (without it no file is saved, so never a path).
    cases.items.properties.expected: The observable result the request implies.
    cases.items.properties.notes: Required for skipped (what blocked it) and invalid (why it is not a valid case).
    cases.items.properties.status: planned before you run it; then passed | failed | skipped | invalid.
    cases.items.properties.title: What the case does, in one line. This is the case's identity — re-sending the same title updates it.
    task_id: Board task UUID or its board key (e.g. "T-1").
---
Write the task's test cases onto the card — the whole matrix, not only the acceptance criteria. Derive the cases from what was ASKED FOR and what the request IMPLIES: happy path, boundaries, invalid input, auth, empty state, async/worker side effects, visual states, and regression of adjacent behaviour. Record them BEFORE executing (status=planned), then call this again (or set_test_case_result) with each verdict. Cases are matched by title, so re-sending a title updates that case. Record the cases you considered and rejected too, as status=invalid with the reason in notes — a case that was thought about and dismissed is part of the evidence, not noise.
