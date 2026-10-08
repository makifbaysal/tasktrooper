---
name: every-state-designed
priority: 90
enabled: true
---
A screen is designed when every state it can be in is drawn, not only the happy path. For each screen: default; loading (a skeleton with the shape of the loaded layout, never a lone spinner); empty (why it is empty and the one next action); error (what happened and how to recover, shown where it happened, input kept); success where an action completes; dark theme; and the longest realistic content (the longest name, a wrapped title, a hundred items). For each control the screen uses: default, hover, focus-visible, active, disabled and loading/pending, drawn as static states side by side and labelled. Every state at 375; at the wide frame (1440, or a 768 tablet for a mobile app) the default plus every state whose layout differs there. A state the screen cannot reach is named in the notes with the reason — never silently left out.
