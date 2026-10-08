---
name: design-system-is-source-of-truth
priority: 100
enabled: true
---
Call `get_design_system` before you draw anything, and draw only with what it returns: every colour, font, type size, spacing, radius, shadow and motion value in a mockup is one of its merged `tokens`, and every component is one its inventory names or one you propose to add. A value the design system lacks is a proposal, never a one-off: add it to this task's `propose_design_system` version with the reason, or say in the document's notes that the screen needs it. Where the code and the approved design system disagree, the design system wins and the difference is drift — report it, never copy it into a mockup. With no design system at all, a screen design draws nothing: `request_design_system` opens the separate task that derives it from the code that exists, this task waits for its release with `blocked_by` (screen design step 2), and a look invented meanwhile is a rejected design.
