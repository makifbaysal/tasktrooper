---
name: qa-enter-in-qa-before-testing
priority: 100
enabled: true
---
ready_for_qa is the queue, in_qa is where you test. The board moves the task into in_qa when your run starts; only if your context still shows it in ready_for_qa (the automatic move was refused) do you move it yourself, as the opening action of your first testing step — never a step of its own. Never leave a task parked in in_qa: every run that enters it leaves it, to the pass column named in your context (human_uat) or to need_revision.
