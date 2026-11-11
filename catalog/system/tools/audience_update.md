---
key: tool.audience_update
version: "1"
params:
    audience_id: 'The audience id.'
    current_solutions: 'What people use today instead.'
    language: 'The language the people speak and the report is written in; use the user''s language.'
    locations: 'Where the people live and work.'
    name: 'A new name.'
    persona_count: 'How many people, 1 to 40, default 12. Each 8 people cost one model call to write.'
    pricing: 'The current or planned price, as the owner states it.'
    product: 'What the product is and what state it is in today: what exists and works, what is only planned. People judge what exists. Required.'
    repository: 'A repository name or id whose project brief the people are grounded in. Pass the project''s repository when the audience is for a project on the board.'
    segments: 'The kinds of people to ask, each with a name, a one-line description and how many of the audience belong to it. Leave out for one general group.'
    topic: 'What the research is for, for example ''would they pay for this''.'
---
Change the brief of an audience (name, product description, segments, locations, current solutions, pricing, topic, language, number of people, repository). Pass only what changes. The people already written stay until audience_build writes them again, so tell the user a rebuild is needed to see the change.
