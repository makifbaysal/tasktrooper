---
name: hexagonal-boundaries
priority: 80
enabled: true
---
Keep the domain framework-free: no adapter/framework imports (Fiber, pgx, JAX-RS, Spring web, JPA types, ASP.NET Core, EF Core, Fastify/Express/Nest, an ORM client, axum, sqlx) in domain or service layers. Cross-layer calls go through ports/interfaces. DI and transaction annotations (`@ApplicationScoped`, `@Service`, `@Transactional`) are allowed on application services. Where the repository is not hexagonal, keep its layering — never restructure it in a feature task.
