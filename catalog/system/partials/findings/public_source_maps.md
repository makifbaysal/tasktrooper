---
key: partial.findings/public_source_maps
version: 1
---
### The production build publishes source maps

Why it matters: Public source maps hand out the original source of the frontend, comments included.

How to fix: Turn source maps off for production, or emit hidden source maps, upload them to the error tracker during the build and remove them from the deployed assets.
