---
name: test-data-and-stubs
category: qa
description: Use when a case needs data, a user/role, or a third-party dependency - seeding through the product first, test credentials, namespacing on shared environments, and stubbing an external HTTP service by hand
---
# Test Data and Stubs

## Seed order

1. **Through the product's own API/UI** (black-box; this exercises validation as a side effect).
2. **The repo's documented seed** (`make seed`, `npm run db:seed`, `prisma db seed` — find it via `get_project_brief` or the README).
3. **SQL last, and only on a local DB.**

Record the seed commands you ran in the case's `evidence`.

## Database

The repo's docker-compose service (`docker compose up -d db` returns), else its documented local DB. No container runtime available → say so as a blocker (qa-verify-before-verdict's triage table), don't improvise a different engine — a SQLite stand-in for Postgres hides real dialect bugs.

## Credentials

Pull from `.env.example` or the repo's own seed fixtures. Never a real person's account. A third-party sandbox key you don't have is a human blocker (`ask_user`), not something to work around.

## Namespacing on shared environments

On stage: prefix everything you create with `qa-<task-key>-…`, clean it up when you're done, never run a destructive scenario there.

## Stubbing a third-party HTTP dependency by hand

```bash
docker run -d --rm -p 8089:8080 wiremock/wiremock:3.13.2
curl -X POST localhost:8089/__admin/mappings -d '{"request":{"method":"POST","url":"/charge"},"response":{"status":402,"jsonBody":{"error":"declined"}}}'
# point the app's dependency URL env var at http://localhost:8089, drive the scenario, then:
curl -s localhost:8089/__admin/requests | jq '.requests[].request.url'  # no jq: node -e 'let d="";process.stdin.on("data",c=>d+=c).on("end",()=>JSON.parse(d).requests.forEach(r=>console.log(r.request.url)))'
```

Or use the repo's own mock server if it already has one — prefer that over standing up a second one.

## After the round

Save a working boot+seed recipe to project memory (`save_memory`, scope=project) once it cost you real effort — the next QA round on this repo should not rediscover it.
