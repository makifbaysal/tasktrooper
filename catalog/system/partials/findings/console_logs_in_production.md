---
key: partial.findings/console_logs_in_production
version: 1
---
### Debug console output ships in the production bundle

Why it matters: console.log/debug/info calls run in every user's browser or app: they leak internal data in devtools, cost performance, and drown real errors.

How to fix: Remove leftover debugging output. Where a message is worth keeping, send it through the app's logger or error tracking. Make the production build strip debug console calls (esbuild drop, terser drop_console, babel-plugin-transform-remove-console) while keeping console.error and console.warn.
