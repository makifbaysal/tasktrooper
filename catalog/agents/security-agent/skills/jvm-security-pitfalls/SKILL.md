---
name: jvm-security-pitfalls
category: security
description: Use when reviewing Java or Kotlin server changes on Spring or Quarkus - security config and method security, JPA/JPQL and JdbcTemplate queries, request binding, deserialization, XML parsers, SpEL, templates, Actuator, JNDI and reflection
tech_stack: Java & Kotlin
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); Spring Security, Spring Data and Quarkus security documentation cited, not reproduced
---
# JVM Security Pitfalls (Java, Kotlin — Spring, Quarkus)

## Overview

On the JVM the dangerous code is often configuration: a matcher order, a missing `@EnableMethodSecurity`, a Jackson typing setting, an Actuator exposure list. Read config and annotation diffs with the same care as handler code.

## 1. Spring Security Configuration (CWE-285, CWE-863)

- `requestMatchers(...)` rules are evaluated in order; a broad `permitAll()` placed above a narrower `hasRole("ADMIN")` wins.
- A security matcher and the MVC mapping must cover the same paths — check trailing slashes, path variables and alternate mappings of the same controller.
- `@PreAuthorize`/`@Secured`/`@RolesAllowed` do nothing without method security enabled (`@EnableMethodSecurity`); a new annotated method in a module that never enables it is unprotected.
- `csrf { disable() }` / `.csrf().disable()` while sessions use cookies → CSRF on every state change.

```kotlin
// ❌ the admin rule is never reached for /api/admin/**
http.authorizeHttpRequests {
    it.requestMatchers("/api/**").permitAll()
    it.requestMatchers("/api/admin/**").hasRole("ADMIN")
}
// ✅ most specific first, deny by default
http.authorizeHttpRequests {
    it.requestMatchers("/api/admin/**").hasRole("ADMIN")
    it.requestMatchers("/api/public/**").permitAll()
    it.anyRequest().authenticated()
}
```

**Quarkus:** a resource method without `@RolesAllowed`/`@Authenticated` where its siblings have one; `@PermitAll` added; `quarkus.http.auth.permission.<name>.policy=permit` widened to a broader `paths=`; `quarkus.security.jaxrs.deny-unannotated-endpoints` turned off.

## 2. Queries (CWE-89)

```java
// ❌ JPQL injection
em.createQuery("SELECT u FROM User u WHERE u.email = '" + email + "'", User.class);
// ✅
em.createQuery("SELECT u FROM User u WHERE u.email = :email", User.class).setParameter("email", email);
```

Also: `createNativeQuery` with concatenation; `JdbcTemplate.query("… " + x)`; `@Query` built with string concatenation in a custom repository; Spring Data `JpaSort.unsafe(userInput)` (plain `Sort.by` validates property names — `unsafe` does not); Panache `find("name = '" + n + "'")` instead of `find("name", n)` or `find("name = ?1", n)`.

## 3. Request Binding (CWE-915)

```java
// ❌ the JPA entity is the request body: role, ownerId, verified are all client-settable
@PostMapping("/users/{id}") User update(@PathVariable UUID id, @RequestBody User body) { … }
// ✅ a request record with only the editable fields
record UpdateProfile(String displayName, String bio) {}
```

`@ModelAttribute` on an entity binds every setter unless `@InitBinder` restricts `setAllowedFields`. Returning the entity serialises fields like `passwordHash`.

## 4. Deserialization and Reflection (CWE-502, CWE-470)

- `ObjectInputStream.readObject` on network, cache, cookie or file input without an `ObjectInputFilter` allowlist.
- Jackson `activateDefaultTyping(...)` / `enableDefaultTyping()`, or `@JsonTypeInfo(use = JsonTypeInfo.Id.CLASS)` on a type bound from requests — the client picks the class.
- `XMLDecoder`, XStream without a type allowlist, SnakeYAML < 2.0 `new Yaml().load(...)`, Kryo without registration required, Hessian/Burlap endpoints.
- `Class.forName(userValue)` + instantiation; `InitialContext.lookup(userValue)` (JNDI → remote class loading) — CRITICAL when reachable.

## 5. XML Parsers (CWE-611)

```java
// ❌ DTDs and external entities resolved
DocumentBuilder db = DocumentBuilderFactory.newInstance().newDocumentBuilder();
// ✅
DocumentBuilderFactory f = DocumentBuilderFactory.newInstance();
f.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
f.setXIncludeAware(false);
f.setExpandEntityReferences(false);
```

The same applies to `SAXParserFactory`, `XMLInputFactory` (`SUPPORT_DTD` false), `TransformerFactory`, `SchemaFactory`, `Unmarshaller` built on an unconfigured source.

## 6. Expression and Template Injection (CWE-917, CWE-1336)

```java
// ❌ SpEL from input with a full evaluation context → T(java.lang.Runtime).getRuntime().exec(…)
parser.parseExpression(userFilter).getValue(new StandardEvaluationContext(root));
// ✅ no user-authored expressions; if unavoidable, SimpleEvaluationContext (no type references)
parser.parseExpression(userFilter).getValue(SimpleEvaluationContext.forReadOnlyDataBinding().build(), root);
```

Thymeleaf: a controller returning a view name built from input (`"pages/" + page`) enables expression preprocessing (`__${…}__`); `th:utext` with user content is XSS. FreeMarker templates authored by users with `?new` / `?api` available.

## 7. Operational Exposure

- Actuator: `management.endpoints.web.exposure.include=*` (or adding `env`, `heapdump`, `threaddump`, `jolokia`, `loggers`) on a port reachable without authentication → secrets in `env`/heap dumps, HIGH.
- `server.error.include-stacktrace=always` / `include-message=always` in production profiles → information exposure (note unless it leaks secrets).
- H2 console or Swagger UI enabled in a production profile with no auth.

## 8. Commands, Paths, Randomness, Tokens

- `Runtime.exec(String)` tokenises without a shell; `ProcessBuilder("sh", "-c", cmd)` and `arrayOf("bash", "-c", …)` reintroduce it.
- `Paths.get(base, name).normalize().startsWith(base)` ignores symlinks — compare `toRealPath()` (path-traversal-and-file-handling).
- `java.util.Random` / `kotlin.random.Random` / `Math.random()` for tokens → `SecureRandom`.
- JWT: `JWT.decode(token)` (java-jwt) does not verify; jjwt `parser().unsecured()` or accepting unsecured JWS; algorithm not pinned.
- Trust-all `X509TrustManager`, `HostnameVerifier { _, _ -> true }`, `NoopHostnameVerifier`.

## Common Mistakes

- Reporting Criteria API, `setParameter` or derived Spring Data queries as SQL injection.
- Reporting `Runtime.exec(String)` as shell injection.
- Reporting Jackson without default typing as unsafe deserialization — plain Jackson binding to concrete types is fine.
- Missing that a method annotation is inert because method security is off.

## Red Flags

- `permitAll()` or `@PermitAll` in a diff; `csrf` disabled in a cookie-session app.
- `+` inside `createQuery`, `createNativeQuery`, `JdbcTemplate`, Panache `find(`.
- `readObject(`, `activateDefaultTyping`, `Id.CLASS`, `XMLDecoder`, `InitialContext.lookup`.
- `parseExpression(` on anything from a request.
- `exposure.include=*` outside a dev profile.

## References (names and links only)

[Spring Security reference](https://docs.spring.io/spring-security/reference/) · [Quarkus security](https://quarkus.io/guides/security-overview) · CWE-89, 285, 470, 502, 611, 863, 915, 917, 1336 · [OWASP Top 10:2025](https://owasp.org/Top10/2025/)
