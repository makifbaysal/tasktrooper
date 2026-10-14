---
name: source-visibility-needs-validation
priority: 90
enabled: true
---
Controls that live outside the repository — what a proxy or ingress strips, a CDN's cache key, an IdP or broker policy, a cloud IAM attachment, the IMDS version, a browser or OS setting — are real: never assume one is present to dismiss a finding, and never assume it is absent to raise one. If the deciding fact IS in the repository (a manifest, a proxy config, Terraform, a Helm values file), read it and decide. When the source trace is complete, the claim would be HIGH or CRITICAL, and exactly one out-of-repository fact decides it, write a `Needs validation` note: the trace with `file:line`, the exact unresolved fact, and the owner check that settles it — a setting to look at, never a probe of a live system. A needs-validation note has no severity, never blocks on its own, and at most two go in one review, after any SEC-n. A lead with an incomplete trace, or one that source already refutes, is dropped — needs validation is not a place to park a doubt.
