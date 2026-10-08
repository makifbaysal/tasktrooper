---
name: deep-learning-pytorch
category: ml
description: Use when writing or changing PyTorch code — model, Dataset/DataLoader, training and evaluation loops, determinism, mixed precision, torch.compile, checkpoints with safetensors, Hugging Face models, ONNX export, and tests that run on CPU
tech_stack: PyTorch
source: original; informed by the PyTorch reproducibility, serialization, AMP and torch.compile documentation and the safetensors docs (cited, not reproduced)
---
# Deep Learning with PyTorch

## Overview

Deep learning bugs rarely crash: a softmax before the loss, dropout left on during evaluation, normalisation statistics computed on the test set — the loss still goes down. Structure the code so each piece is a small function you can test on CPU in a second, and prove the wiring by overfitting one tiny batch.

**Core principle:** the eager CPU path is the tested path; GPUs, `torch.compile` and mixed precision are configuration on top of it, never a different code path.

## 1. Structure

```
models/net.py        nn.Module, no data loading, no device logic
data/dataset.py      Dataset returning tensors; normalisation stats passed in, fitted on train only
train/loop.py        train_one_epoch(), evaluate(), fit() driven by a config object
pipelines/train.py   entrypoint: config → seed → data → fit → checkpoint → log
```

```python
def train_one_epoch(model, loader, optimizer, loss_fn, device) -> float:
    model.train()
    total = torch.zeros((), device=device)
    seen = 0
    for xb, yb in loader:
        xb, yb = xb.to(device, non_blocking=True), yb.to(device, non_blocking=True)
        optimizer.zero_grad(set_to_none=True)
        loss = loss_fn(model(xb), yb)
        loss.backward()
        optimizer.step()
        total += loss.detach() * len(xb)
        seen += len(xb)
    return (total / seen).item()

@torch.inference_mode()
def evaluate(model, loader, device) -> tuple[torch.Tensor, torch.Tensor]:
    model.eval()
    logits, targets = [], []
    for xb, yb in loader:
        logits.append(model(xb.to(device)).cpu())
        targets.append(yb)
    return torch.cat(logits), torch.cat(targets)
```

Accumulating `loss.detach()` avoids a GPU sync per step that `loss.item()` forces.

## 2. Classic bugs

| Bug | Fix |
|---|---|
| `softmax` / `sigmoid` before `CrossEntropyLoss` / `BCEWithLogitsLoss` | pass raw logits; the loss applies it |
| `model.eval()` missing at evaluation (dropout, batch norm still training) | `evaluate()` always calls `eval()`; `train_one_epoch()` always calls `train()` |
| Evaluation without `inference_mode`/`no_grad` | memory grows, slower |
| Class labels as float for `CrossEntropyLoss` | `torch.long` class indices |
| Normalisation mean/std from the full dataset | compute on the training split, pass into the Dataset (leakage-safe-feature-engineering) |
| Gradients accumulating across steps | `zero_grad(set_to_none=True)` every step |

## 3. Tests that run on CPU in seconds

```python
def test_model_can_overfit_one_small_batch() -> None:
    torch.manual_seed(0)
    model = Classifier(n_features=8, n_classes=3)
    x, y = torch.randn(16, 8), torch.randint(0, 3, (16,))
    optimizer = torch.optim.Adam(model.parameters(), lr=1e-2)
    for _ in range(200):
        optimizer.zero_grad(set_to_none=True)
        loss = F.cross_entropy(model(x), y)
        loss.backward()
        optimizer.step()
    assert loss.item() < 0.05

def test_every_parameter_receives_a_gradient() -> None:
    model = Classifier(n_features=8, n_classes=3)
    F.cross_entropy(model(torch.randn(4, 8)), torch.tensor([0, 1, 2, 0])).backward()
    assert all(p.grad is not None and p.grad.abs().sum() > 0 for p in model.parameters())
```

Also: output shape for a batch of one and a batch of many; a checkpoint round-trip gives identical outputs; `DataLoader(num_workers=0)` in tests. GPU-only tests are skipped with `pytest.mark.skipif(not torch.cuda.is_available(), reason=...)` and never the only test of a behaviour.

## 4. Determinism

```python
def seed_everything(seed: int) -> torch.Generator:
    random.seed(seed)
    np.random.seed(seed)
    torch.manual_seed(seed)
    return torch.Generator().manual_seed(seed)

def seed_worker(worker_id: int) -> None:
    worker_seed = torch.initial_seed() % 2**32
    np.random.seed(worker_seed)
    random.seed(worker_seed)

g = seed_everything(cfg.seed)
loader = DataLoader(ds, batch_size=64, shuffle=True, num_workers=4, worker_init_fn=seed_worker, generator=g)
```

When bit-exact repeatability matters: `torch.use_deterministic_algorithms(True)`, `torch.backends.cudnn.benchmark = False`, and `CUBLAS_WORKSPACE_CONFIG=:4096:8` in the environment for CUDA. Deterministic kernels are slower; make it a config flag. Third-party code that still uses the global NumPy state is why `np.random.seed` appears here — your own code uses a passed `np.random.Generator`.

## 5. Device, precision, compile

```python
def pick_device() -> torch.device:
    if torch.cuda.is_available():
        return torch.device("cuda")
    if torch.backends.mps.is_available():
        return torch.device("mps")
    return torch.device("cpu")
```

- Mixed precision: `with torch.autocast(device_type=device.type, dtype=torch.bfloat16, enabled=cfg.amp):` around the forward and loss. float16 on CUDA also needs `torch.amp.GradScaler("cuda")`.
- `torch.compile(model)` is an optimisation flag in config. The first calls compile (seconds to minutes) — exclude them from timing; unit tests run eager; if compile fails on a platform, the run falls back to eager and logs it instead of crashing.
- No GPU in this workspace: train on the fixture on CPU, and say in the closing message that GPU behaviour and speed were not exercised.

## 6. Checkpoints and artifacts

```python
def save_checkpoint(path: Path, model, optimizer, scheduler, epoch: int) -> None:
    tmp = path.with_suffix(".tmp")
    torch.save({
        "model": model.state_dict(),
        "optimizer": optimizer.state_dict(),
        "scheduler": scheduler.state_dict(),
        "epoch": epoch,
        "torch_rng": torch.get_rng_state(),
    }, tmp)
    tmp.replace(path)

state = torch.load(path, weights_only=True, map_location="cpu")
```

- Write to a temporary file and rename, so a crash never leaves a half-written checkpoint.
- `weights_only=True` is the default since PyTorch 2.6; never pass `weights_only=False` for a file this pipeline did not write (rule safe-model-serialization).
- Ship inference weights as safetensors (`safetensors.torch.save_file(model.state_dict(), "model.safetensors")`; `save_model`/`load_model` when weights are tied) plus a small JSON config — not a pickled `nn.Module`.

## 7. Hugging Face and ONNX

- Pin pretrained models by commit: `from_pretrained(name, revision="<commit sha>")`; keep `trust_remote_code=False` unless the task names the model and a person accepted the risk.
- `save_pretrained` writes safetensors by default — keep it that way.
- ONNX export: the exporter API changed during 2.x (the dynamo-based exporter is the default in recent releases); follow the pinned version. Whatever the exporter, prove parity:

```python
session = onnxruntime.InferenceSession("model.onnx")
onnx_out = session.run(None, {"x": example.numpy()})[0]
np.testing.assert_allclose(onnx_out, model.eval()(example).detach().numpy(), rtol=1e-4, atol=1e-5)
```

## Common Mistakes

- Code paths that only work on CUDA (`.cuda()` calls instead of `.to(device)`).
- Validating on batches the model trained on.
- `DataLoader` workers started without a `if __name__ == "__main__":` guard on spawn platforms (macOS, Windows).
- A learning-rate schedule stepped per batch when it was designed per epoch, or the reverse.
- Comparing two architectures on different seeds and one run each.

## Red Flags

- Training loss falls but validation loss never moves — check `eval()`, the labels and the split.
- A test suite that needs a GPU to pass.
- `torch.load(..., weights_only=False)` anywhere outside a migration of the repo's own legacy checkpoints.
- Loss of exactly 0 or NaN after a few steps — check for leakage or a missing log-softmax/clamp.
