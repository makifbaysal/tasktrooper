---
name: model-serving-and-monitoring
category: ops
description: Use when a model is served online (FastAPI, BentoML) or scored in batch, when changing how a model artifact is loaded or versioned, or when adding drift, quality or prediction monitoring and a rollout plan for a new model
tech_stack: Model serving
source: wshobson/agents ml-pipeline-workflow (MIT), adapted; FastAPI, BentoML, skops and Evidently documentation cited
---
# Model Serving and Monitoring

## Overview

A model in production fails in ways a notebook never shows: the API re-implements preprocessing slightly differently, the artifact loaded is not the one evaluated, inputs drift away from the training data, or an async endpoint blocks on CPU work and the service stalls. Serving code is ordinary backend code with typed inputs, tests and a health check — plus a version on every prediction.

**Core principle:** serve the exact fitted pipeline you evaluated, identified by an immutable version, and log enough with every prediction to notice when the world stops looking like the training data.

## 1. Batch or online?

| Need | Choose |
|---|---|
| Scores consumed on a schedule (daily campaign, nightly risk list) | batch scoring as a pipeline stage (data-pipeline-orchestration) |
| A decision inside a user request (fraud check, ranking) | online endpoint with a latency budget |
| Both | one fitted pipeline, two thin callers — never two feature implementations |

Batch is simpler to operate, test and roll back; choose online only when the decision needs it.

## 2. Online endpoint (FastAPI)

```python
class Features(BaseModel):
    model_config = ConfigDict(extra="forbid")
    days_since_order: int = Field(ge=0, le=3650)
    orders_90d: int = Field(ge=0)
    plan: Literal["free", "pro", "team"]

class ScoreRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    rows: list[Features] = Field(min_length=1, max_length=1000)

class ScoreResponse(BaseModel):
    model_version: str
    scores: list[float]

def create_app(settings: Settings) -> FastAPI:
    @asynccontextmanager
    async def lifespan(app: FastAPI):
        app.state.model = load_model(settings.model_path, settings.trusted_types)
        app.state.model_version = settings.model_version
        yield

    app = FastAPI(lifespan=lifespan)

    @app.get("/health")
    def health() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/ready")
    def ready(request: Request) -> dict[str, str]:
        if getattr(request.app.state, "model", None) is None:
            raise HTTPException(status_code=503, detail="model not loaded")
        return {"status": "ready", "model_version": request.app.state.model_version}

    @app.post("/score")
    def score(body: ScoreRequest, request: Request) -> ScoreResponse:
        frame = pd.DataFrame([row.model_dump() for row in body.rows])
        proba = request.app.state.model.predict_proba(frame)[:, 1]
        return ScoreResponse(model_version=request.app.state.model_version, scores=proba.tolist())

    return app
```

- **Load once** at startup (lifespan), never per request. `/health` says the process is alive; `/ready` says the model is loaded.
- **Typed, bounded input:** unknown fields rejected, ranges and categories enforced, a maximum batch size. Invalid input is a 422 in the framework's error shape, never a 500.
- **`def`, not `async def`, for CPU-bound inference:** FastAPI runs sync endpoints in a thread pool; an `async def` calling `predict_proba` blocks the event loop for every other request.
- **Every response carries the model version**, so a complaint can be traced to the artifact that produced it.
- BentoML (`@bentoml.service` classes) or another server is fine where the repository already uses it; the same rules apply (rule stack-choice).

## 3. Artifacts

- The served artifact is the fitted pipeline (preprocessing + model) evaluated in training — never a model whose preprocessing is re-coded in the handler (leakage-safe-feature-engineering).
- Identified by an immutable version: a registry version or alias resolved at deploy time (`models:/churn@champion` → version 12), or a content hash. Configuration names the version; the service logs it at startup.
- Stored in a safe format and loaded only from the repository's own registry or bucket: skops with a reviewed trusted-types list, ONNX, the booster's native format, safetensors (rule safe-model-serialization).

## 4. Tests

```python
def test_score_matches_the_training_pipeline(model_settings, churn_fixture) -> None:
    X, _ = churn_fixture
    with TestClient(create_app(model_settings)) as client:
        response = client.post("/score", json={"rows": X.head(3).to_dict("records")})
    assert response.status_code == 200
    expected = load_model(model_settings.model_path, model_settings.trusted_types).predict_proba(X.head(3))[:, 1]
    np.testing.assert_allclose(response.json()["scores"], expected)

def test_score_rejects_an_unknown_plan(client) -> None:
    response = client.post("/score", json={"rows": [{"days_since_order": 3, "orders_90d": 1, "plan": "gold"}]})
    assert response.status_code == 422
```

- The fixture model is trained on the synthetic fixture in `conftest.py` and saved to `tmp_path` — no registry, no network.
- `TestClient` used as a context manager runs the lifespan, so the load path is tested too.
- Also: `/ready` before and after load, an empty `rows` list and an oversized batch rejected, the version present in every response.

## 5. Batch scoring

```python
def score_partition(day: date, model: Pipeline, model_version: str, out_root: Path) -> Path:
    features = load_features(day)
    scores = features[["customer_id"]].assign(
        score=model.predict_proba(features)[:, 1],
        model_version=model_version,
        scored_for=day,
    )
    target = out_root / f"day={day:%Y-%m-%d}" / "scores.parquet"
    target.parent.mkdir(parents=True, exist_ok=True)
    tmp = target.with_suffix(".tmp")
    scores.to_parquet(tmp, index=False)
    tmp.replace(target)
    return target
```

Idempotent per partition, written atomically, every row stamped with the model version. Validate the output before publishing it: no NaN scores, scores within [0, 1], one row per entity (data-validation-contracts).

## 6. Rollout

1. **Shadow:** the new model scores the same traffic or partitions as the current one; only the current one's output is used. Compare score distributions and, where labels exist, metrics.
2. **Canary:** a small share of traffic or one segment uses the new model, with explicit rollback triggers (error rate, latency, a guardrail metric, score distribution shift).
3. **Full rollout**, keeping the previous version deployable.

Write the steps, the triggers and the way back into `after_deploy` and `rollback_plan` (rule deploy-runbook-fields): rolling back a model is pointing the alias or config back at the previous version — record which.

## 7. Monitoring

Log per prediction (or per batch): model version, timestamp, the input features or a hash of them, the score — within what the privacy policy allows; no raw PII the task did not approve. Then watch:

| Signal | How | Why |
|---|---|---|
| Input drift | PSI or a statistical test per feature against the training reference; Evidently where the repo uses it | the world changed |
| Prediction drift | score distribution vs the validation period | often the first visible symptom |
| Data quality | null rates, unseen categories, out-of-range values on live inputs | an upstream break |
| Performance | the real metric once labels arrive (often weeks later) | the only proof the model still works |
| Service | latency, error rate, readiness | it is still an API |

```python
def psi(expected: np.ndarray, actual: np.ndarray, bins: int = 10) -> float:
    edges = np.unique(np.quantile(expected, np.linspace(0, 1, bins + 1)))
    e = np.histogram(np.clip(expected, edges[0], edges[-1]), edges)[0] / len(expected)
    a = np.histogram(np.clip(actual, edges[0], edges[-1]), edges)[0] / len(actual)
    e, a = np.clip(e, 1e-6, None), np.clip(a, 1e-6, None)
    return float(np.sum((a - e) * np.log(a / e)))
```

A common reading: below 0.1 stable, 0.1–0.25 worth a look, above 0.25 a real shift. Thresholds and alerts are agreed with the people who own the decision; Evidently's API changed across versions — follow the pinned one.

## Common Mistakes

- Loading the model inside the request handler.
- `async def` endpoints doing CPU-bound inference.
- Preprocessing duplicated in the API, drifting from training.
- Predictions with no model version attached.
- Monitoring only service health, never the inputs or the scores.

## Red Flags

- A serving test that mocks the model, so parity with training is never checked.
- `latest` as the model reference in production configuration.
- A rollout with no written rollback trigger.
- A 500 from the endpoint on a malformed payload.
