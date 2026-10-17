---
name: otel-instrumentation
category: observability
description: Use when an "enable observability" card asks for metrics, traces or logs - what the developer task must add per language (Go, Node/TS, Python, Java/Kotlin, .NET, Ruby, PHP), the zero-code agents, the OTEL_* environment variables and resource attributes, and how to reach GCP, AWS, Vercel or a plain OTLP endpoint
source: open-telemetry/opentelemetry.io docs (CC BY 4.0), adapted
---

# OpenTelemetry instrumentation

You never write this code yourself. This skill is what you put into the developer's task: exact packages, where the SDK starts, the environment it reads, and acceptance criteria that can be observed in the target environment.

## Always the same

Every service gets the same identity, set through the environment rather than in code:

| Variable | Value |
|---|---|
| `OTEL_SERVICE_NAME` | the component's name, stable across deploys (`checkout-api`) |
| `OTEL_RESOURCE_ATTRIBUTES` | `service.version=<release tag or commit>,deployment.environment.name=<production|staging>` — also set the older `deployment.environment` key while the backend's dashboards still read it |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | the collector or backend base URL (`http://otel-collector:4318`) |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` (default for most SDKs) or `grpc` (port 4317) |
| `OTEL_EXPORTER_OTLP_HEADERS` | auth for a hosted endpoint (`authorization=Bearer …`) — a secret, declared with `declare_env_vars`, never committed |
| `OTEL_TRACES_SAMPLER` / `_ARG` | `parentbased_traceidratio` / `0.1` for a busy production service; `parentbased_always_on` otherwise |
| `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER` | `otlp` (or `none` when the platform already ships stdout logs) |

`service.version` is what lets a deploy-impact read tell the release apart; leaving it out makes every release look the same.

## Per language

- **Go** — no general zero-code agent (the eBPF `opentelemetry-go-instrumentation` is not production-grade for most stacks). Add `go.opentelemetry.io/otel`, `go.opentelemetry.io/otel/sdk`, `.../exporters/otlp/otlptrace/otlptracehttp` and `.../otlpmetric/otlpmetrichttp`; build the tracer and meter providers in `main` from `resource.Default()` merged with the env, register them globally, and `Shutdown` them on exit. Wrap the router with `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` (or `otelgin`, `otelecho`, `otelgrpc`), outbound clients with `otelhttp.NewTransport`, the database with `github.com/XSAM/otelsql`. Logs: the `go.opentelemetry.io/contrib/bridges/otelslog` handler, or keep `slog` JSON on stdout with `trace_id`/`span_id` fields.
- **Node.js / TypeScript** — zero-code: `@opentelemetry/api` + `@opentelemetry/auto-instrumentations-node`, started with `NODE_OPTIONS="--require @opentelemetry/auto-instrumentations-node/register"`. With code: `@opentelemetry/sdk-node` and `getNodeAutoInstrumentations()` in an `instrumentation.ts` loaded before anything else (`--import ./instrumentation.mjs` for ESM). Next.js: `@vercel/otel` with `registerOTel({ serviceName })` in `instrumentation.ts`.
- **Python** — zero-code: `opentelemetry-distro` and `opentelemetry-exporter-otlp`, then `opentelemetry-bootstrap -a install` in the image build, and run under `opentelemetry-instrument` (`opentelemetry-instrument gunicorn app:app`; with gunicorn's `--preload` or forking servers, initialise in `post_fork`). Logs: `OTEL_PYTHON_LOGGING_AUTO_INSTRUMENTATION_ENABLED=true`.
- **Java / Kotlin** — zero-code: the `opentelemetry-javaagent.jar` from the opentelemetry-java-instrumentation releases, attached with `JAVA_TOOL_OPTIONS=-javaagent:/otel/opentelemetry-javaagent.jar`. Spring Boot without an agent (native images): `io.opentelemetry.instrumentation:opentelemetry-spring-boot-starter`. Micrometer metrics: `micrometer-registry-otlp`.
- **.NET** — zero-code: OpenTelemetry .NET Automatic Instrumentation (its install script sets the `CORECLR_*` profiler variables). With code: `OpenTelemetry.Extensions.Hosting`, `OpenTelemetry.Instrumentation.AspNetCore`, `OpenTelemetry.Instrumentation.Http`, `OpenTelemetry.Exporter.OpenTelemetryProtocol`, then `builder.Services.AddOpenTelemetry().WithTracing(t => t.AddAspNetCoreInstrumentation().AddHttpClientInstrumentation()).WithMetrics(m => m.AddAspNetCoreInstrumentation()).UseOtlpExporter();` and `builder.Logging.AddOpenTelemetry()`.
- **Ruby** — `opentelemetry-sdk`, `opentelemetry-exporter-otlp`, `opentelemetry-instrumentation-all`; in an initializer `OpenTelemetry::SDK.configure { |c| c.use_all }`.
- **PHP** — the `opentelemetry` PECL extension plus `open-telemetry/sdk` and `open-telemetry/exporter-otlp`, with `OTEL_PHP_AUTOLOAD_ENABLED=true`; framework packages such as `open-telemetry/opentelemetry-auto-laravel` or `-auto-symfony` do the instrumentation.

Prefer the zero-code route wherever it exists: it is one image or start-command change the developer can verify, instead of edits through the codebase.

## Where it goes

- **Google Cloud (`own_cloud`, gcp).** Traces go by OTLP to the Cloud Telemetry API, `https://telemetry.googleapis.com` (enable `telemetry.googleapis.com` on the project); it needs Google credentials, so the service exports to an OpenTelemetry Collector (a Cloud Run sidecar, or a GKE deployment) whose `otlphttp` exporter uses the `googleclientauth` extension. Metrics go through that collector's `googlemanagedprometheus` exporter (Managed Service for Prometheus), read back by a `gcp_monitoring` data source. Logs: JSON on stdout — Cloud Run and GKE ship it to Cloud Logging; put the trace id in `logging.googleapis.com/trace`. The runtime service account needs `roles/cloudtrace.agent` (or the Telemetry API's traces writer role), `roles/monitoring.metricWriter` and `roles/logging.logWriter` — granting them is a `before_deploy` step for a person.
- **AWS (`own_cloud`, aws).** Run the AWS Distro for OpenTelemetry (ADOT) collector: an ECS sidecar, the EKS add-on, or the ADOT Lambda layer (`AWS_LAMBDA_EXEC_WRAPPER=/opt/otel-instrument`). Its `awsxray` exporter sends traces to X-Ray, `awsemf` sends metrics to CloudWatch (read back by an `aws_cloudwatch` data source), `awscloudwatchlogs` sends logs. The task role needs `AWSXRayDaemonWriteAccess` and `CloudWatchAgentServerPolicy`.
- **Vercel.** `@vercel/otel`, exporting to the OTLP endpoint of the team's backend (`OTEL_EXPORTER_OTLP_ENDPOINT` in the project's environment variables) or through a Vercel observability integration.
- **`otlp_endpoint`.** Set `OTEL_EXPORTER_OTLP_ENDPOINT` to it; its credentials go in `OTEL_EXPORTER_OTLP_HEADERS`.
- **`self_hosted`.** See `self-hosted-stack`; the service exports to that stack's collector.

## Acceptance criteria that can be checked

- "Given the service is deployed to production, when it serves a request, then a span with `service.name=checkout-api` and `deployment.environment.name=production` reaches <target> within a minute."
- "Given the service runs, then `http.server.request.duration` is exported with `http.route` and `http.response.status_code`" (the route label is what makes per-endpoint golden signals possible).
- "Given a request fails, then its log line carries the `trace_id` of that request's span."

Never "the SDK is installed" or "it compiles" — those pass without one span arriving.

## After it ships

`list_data_source_metrics` shows whether the new metric names (`http_server_request_duration_seconds_*` in Prometheus naming) are there, and `propose_signal_binding` binds the golden signals to them. No data source reads the target yet: say so on the card — a person adds it under Operations → Data sources.
