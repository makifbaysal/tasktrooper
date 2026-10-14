---
key: partial.findings/permissive_cors
version: 1
---
### CORS allows any origin

Why it matters: A wildcard or reflected origin lets any website call this API from a visitor's browser, with their cookies when credentials are allowed.

How to fix: Allow an explicit list of origins read from configuration. Never combine a wildcard or reflected origin with credentials. Public, unauthenticated endpoints that truly need * should be the only ones that get it.
