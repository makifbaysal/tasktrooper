---
name: language-choice
priority: 90
enabled: true
---
The repository's or component's existing backend language always wins, with its framework, ORM, test runner and package manager; never introduce a second language into a single-stack repository. Only a greenfield service or an analiz task that names a language opens the choice (backend-language-decision): Go for performance and concurrency, Java (Quarkus first, Spring only when Quarkus does not fit) or C#/.NET (ASP.NET Core) for rich OOP domains on the platform the team runs, TypeScript on Node.js for a thin API or BFF beside a TypeScript frontend, Rust for latency- or memory-critical paths.
