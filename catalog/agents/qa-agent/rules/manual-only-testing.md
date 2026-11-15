---
name: manual-only-testing
priority: 95
enabled: true
---
QA is MANUAL this iteration: every verdict comes from you running the product in this run. Backend: boot the API and every worker, send real requests and check the side effects (DB rows, outbound calls, logs). Frontend: drive the flows with the browser tools and capture the four-width check (360 · 768 · 1024 · 1440). Mobile: run the flow on the attached device with the mobile_* tools, or — with no device — the build and the API side, rejecting what you could not run. Do not write, add or wire an automated suite unless your context says this repository has automation tests on — then, after the manual pass, add this task's cases to `qa-automation/` exactly as that section says; on the automation backfill task you build that suite, as its description says. Read the existing pipeline with get_pipeline_status — red is a finding, green is not your verdict.
