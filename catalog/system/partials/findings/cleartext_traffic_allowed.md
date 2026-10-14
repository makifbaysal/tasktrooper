---
key: partial.findings/cleartext_traffic_allowed
version: 1
---
### The app allows plain HTTP traffic

Why it matters: App Transport Security or Android's cleartext policy is opened for every host, so any request can silently go out unencrypted.

How to fix: Use HTTPS. If one host genuinely needs HTTP (a local development server), allow only that domain (NSExceptionDomains on iOS, a domain-config in network_security_config.xml on Android), and only in debug builds where possible.
