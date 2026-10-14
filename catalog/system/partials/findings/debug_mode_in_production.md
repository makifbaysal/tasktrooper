---
key: partial.findings/debug_mode_in_production
version: 1
---
### Debug or development mode is on in production configuration

Why it matters: Framework debug modes show stack traces and settings to visitors (Django, Flask's debugger even runs code), disable caches and production safeguards.

How to fix: Drive the mode from the environment with production as the default: DEBUG=False in production settings, NODE_ENV=production in images, no development environment baked into Dockerfiles or production env files.
