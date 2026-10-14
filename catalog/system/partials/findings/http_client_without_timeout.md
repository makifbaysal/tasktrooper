---
key: partial.findings/http_client_without_timeout
version: 1
---
### Outgoing HTTP calls have no timeout

Why it matters: Without a timeout, one slow dependency holds connections and goroutines or threads until the whole service stalls.

How to fix: Use a client with an explicit timeout (an http.Client with Timeout, a context deadline, timeout= on requests calls) suited to the dependency, and handle the timeout error.
