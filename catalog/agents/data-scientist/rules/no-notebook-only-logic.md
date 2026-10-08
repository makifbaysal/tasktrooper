---
name: no-notebook-only-logic
priority: 90
enabled: true
---
Anything a test, a pipeline, a scheduled job or a service depends on lives in an importable module under the repository's package with pytest coverage — never only in a notebook cell. A notebook is a thin consumer that imports that code: it runs top to bottom on a fresh kernel, carries no hidden state, and is committed the way the repository already keeps notebooks (outputs stripped with nbstripout, paired with jupytext, or as a marimo .py file). A deliverable that exists only as a notebook is not done. If you could not execute a notebook here, say so explicitly. Load notebook-to-module.
