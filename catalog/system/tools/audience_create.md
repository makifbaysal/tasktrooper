---
key: tool.audience_create
version: "1"
params:
    current_solutions: 'What people use today instead.'
    language: 'The language the people speak and the report is written in; use the user''s language.'
    locations: 'Where the people live and work.'
    name: 'A short name, for example ''Freelance designers for TaskTrooper''.'
    persona_count: 'How many people, 1 to 40, default 12. Each 8 people cost one model call to write.'
    pricing: 'The current or planned price, as the owner states it.'
    product: 'What the product is and what state it is in today: what exists and works, what is only planned. People judge what exists. Required.'
    repository: 'A repository name or id whose project brief the people are grounded in. Pass the project''s repository when the audience is for a project on the board.'
    segments: 'The kinds of people to ask, each with a name, a one-line description and how many of the audience belong to it. Leave out for one general group.'
    topic: 'What the research is for, for example ''would they pay for this''.'
---
Write down an audience for a product or project: a brief the people are written from. This only saves the brief; no model call runs and nothing is spent until audience_build. Ground it in the project: call get_project_brief and read the product's README, features and current state first, then describe honestly what exists today and who it is for. Show the user the brief and the number of calls building costs, then ask before audience_build.
