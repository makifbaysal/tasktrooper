---
key: tool.propose_error_definitions
version: "1"
params:
    definitions: One entry per error definition; at most 50 in one call.
    definitions.items.properties.action: 'What a matching group sets off: none (only labelled), ignore (hidden from the inbox), alert (an incident while the count in alert_threshold''s window is at or above its count, resolved when it drops), incident (an incident each time the threshold is crossed, never resolved automatically), task (one board task for the component''s developer per new matching group).'
    definitions.items.properties.alert_threshold: 'For alert and incident only: {count, window_seconds}. Defaults: alert 10 in 3600 s, incident 1 in 3600 s. window_seconds 60–604800.'
    definitions.items.properties.category: 'bug (this code behaves wrongly), expected (a designed outcome logged as an error), dependency (a downstream service or API failing), infrastructure (memory, disk, network, the database itself), user_error (bad input the code rejects correctly).'
    definitions.items.properties.component: 'Component path this definition is limited to. Omit for every component of the repository — or, inside an AI job, the job''s own component.'
    definitions.items.properties.evidence: 'What the definition rests on: group fingerprints and file:line references. At most 20 entries of 500 characters.'
    definitions.items.properties.match: 'What a group must show; every field you set must hold (AND), and at least one is required.'
    definitions.items.properties.match.properties.attributes: 'Exact key → value pairs: "source" and "provider" of the group, plus key=value pairs printed in its message.'
    definitions.items.properties.match.properties.endpoint: 'The request path printed in the message or sample, optionally with its method ("POST /api/orders"). A trailing * matches any rest of the path; a *, :id or {id} segment matches any one segment.'
    definitions.items.properties.match.properties.exception_type: 'The exception or error type, as printed before the first ":" of the message (or of the sample''s first or last line). A bare class name also matches its qualified form ("NullPointerException" matches "java.lang.NullPointerException").'
    definitions.items.properties.match.properties.fingerprint: A group's exact fingerprint from list_error_groups. Use it for one specific group only.
    definitions.items.properties.match.properties.message_regex: 'RE2 regular expression tested against the message, then the sample. Anchor it on the stable words; never an id, a timestamp or a number that varies. At most 512 characters.'
    definitions.items.properties.match.properties.status: HTTP status printed in the message or sample (100–599).
    definitions.items.properties.name: Short human name of this kind of error, e.g. "Payment provider timeout".
    definitions.items.properties.rationale: One sentence a person reads before accepting it - why this category and this action.
    definitions.items.properties.severity: Severity of an incident or task it opens (default medium).
    repository_id: Repository UUID. Omit to use the AI job's or the current task's repository; inside an AI job only the job's own is accepted, and a run bound to a repository may name only that one.
    source: '"ai" when you read it off error groups (the default), "code" when you derived it from an error type or code declared in the code.'
---
Store error definitions you propose for a repository. Every one is saved as a suggestion — status `suggested`, never active — and a person accepts or dismisses it on the Errors page; nothing hides, alerts or opens a task until they do. A proposal whose component and match equal an existing definition's (in any status, a dismissed one included) is not stored again and comes back under `duplicates`; an invalid one comes back under `rejected` with its field errors while the others are still stored. Returns `stored` (`{id, name, category, action}`), `duplicates` (`{name, existing_id}`) and `rejected` (`{name, fields}`). It never changes code.
