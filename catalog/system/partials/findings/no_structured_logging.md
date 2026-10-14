---
key: partial.findings/no_structured_logging
version: 1
---
### The service has no logging library

Why it matters: Without a logger the service cannot emit levelled, structured logs, which is what production debugging and alerting rely on.

How to fix: Adopt the ecosystem's standard: log/slog or zerolog in Go, pino in Node, logging or structlog in Python, SLF4J with Logback on the JVM, Microsoft.Extensions.Logging or Serilog in .NET, tracing in Rust. JSON output in production, level from the environment, then replace ad-hoc prints.
