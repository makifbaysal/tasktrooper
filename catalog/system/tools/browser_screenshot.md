---
key: tool.browser_screenshot
version: "2"
params:
    attach_to_task: 'Also save the screenshot on the board task this run is working, where the human sees it among the task''s attachments (default: false). Use it for evidence the human should see: a design''s self-check, a design review, a QA finding'
    full_page: 'Capture the entire scrollable page instead of just the viewport (default: false)'
    title: 'File name for the saved screenshot when attach_to_task is true, e.g. "export-dialog-375-empty"'
    width: 'Viewport width in CSS pixels (default: 1440). Omit it after browser_set_viewport — passing width re-emulates a bare viewport and discards what browser_set_viewport set'
---
Take a screenshot of the current page of the shared browser and attach it to the result so you can see it. The image is returned inline; it is saved on the task only with attach_to_task. After browser_set_viewport, call this WITHOUT width.
