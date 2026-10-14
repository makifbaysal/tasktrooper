---
key: partial.findings/tls_verification_disabled
version: 1
---
### TLS certificate verification is turned off

Why it matters: With verification off, anyone on the network path can impersonate the server and read or change the traffic.

How to fix: Remove the override. If the server uses a private CA, add that CA to the trust store the client uses instead. A local-development exception belongs behind explicit development configuration, never on by default.
