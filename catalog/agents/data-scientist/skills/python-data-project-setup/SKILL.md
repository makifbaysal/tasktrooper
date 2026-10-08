---
name: python-data-project-setup
category: data
description: Use when starting a Python data or ML project, adding its first tooling, or changing pyproject.toml, the lockfile, the package layout or the lint/type/test configuration — detect the repo's tooling first, then uv, ruff, mypy, pytest and a src layout
tech_stack: Python
source: original; informed by the uv, Ruff, mypy and pytest documentation (cited, not reproduced)
---
# Python Data Project Setup

## Overview

A data repository is a software repository: an importable package, a lockfile, a linter, a type checker and a test suite that runs in seconds. Most "the notebook worked on my machine" failures are a missing lockfile, an absolute path or logic that never left a cell.

**Core principle:** detect and follow what the repository already uses; set up the defaults below only for a new project or when the task asks for it — never add a tool just because this skill names it (rule repo-conventions-win).

## 1. Detect before you add

| You find | It means | Commands |
|---|---|---|
| `uv.lock` | uv | `uv sync --locked`, `uv run pytest`, `uv add <pkg>` |
| `poetry.lock` | Poetry | `poetry install`, `poetry run pytest`, `poetry add <pkg>` |
| `pixi.toml` / `pixi.lock` | pixi (conda) | `pixi run <task>` |
| `environment.yml` only | conda | `conda env create -f environment.yml` |
| `requirements*.txt` (+ `.in`) | pip / pip-tools | `python -m pip install -r requirements.txt`, `pip-compile` to change pins |
| `Makefile`, `noxfile.py`, `tox.ini` | a task runner wraps the above | use its targets — they are what CI runs |

Read `requires-python`, the pinned majors of pandas, NumPy, scikit-learn, Polars, PyTorch and MLflow, and the CI workflow before writing code. Those pins are the API you write against — pandas 2 and pandas 3 code differ (see dataframe-performance), and Polars 2.0 shipped in October 2026, so a repository pinned to `polars<2` stays there.

## 2. Layout for a new project

```
pyproject.toml
uv.lock
src/<package>/
    __init__.py
    config.py          settings from env (pydantic-settings) — no hard-coded paths
    data/              loading + schemas (data-validation-contracts)
    features/          pure transforms, sklearn transformers
    models/            training, evaluation, persistence
    pipelines/         entrypoints: train.py, score.py (thin; call the modules above)
tests/
    conftest.py        small synthetic fixtures
    test_*.py
notebooks/             thin consumers of the package (notebook-to-module)
data/                  gitignored; DVC-tracked if the repo versions data
```

A src layout means tests import the installed package, not a file that happens to sit next to them — the same import path the pipeline and the service use.

## 3. pyproject.toml defaults

```toml
[project]
name = "churn"
requires-python = ">=3.13"
dependencies = ["pandas>=3,<4", "scikit-learn>=1.8,<2", "pyarrow"]

[project.scripts]
churn-train = "churn.pipelines.train:main"

[dependency-groups]
dev = ["pytest", "hypothesis", "ruff", "mypy", "pandas-stubs"]

[tool.ruff]
line-length = 100

[tool.ruff.lint]
select = ["E", "F", "I", "B", "UP", "PD", "NPY", "PT", "SIM"]

[tool.mypy]
strict = true

[tool.pytest.ini_options]
testpaths = ["tests"]
addopts = "-q --strict-markers --import-mode=importlib"
```

- `NPY` flags the legacy global `np.random.*` API (use `np.random.default_rng`), `PD` flags pandas anti-patterns such as `inplace=True` and `.values`.
- `pandas-stubs` makes mypy useful on dataframe code; where strict typing of the whole repo is not realistic, follow the repo's existing mypy strictness rather than raising it in a feature task.
- Pin majors with an upper bound in libraries whose majors break APIs (pandas, Polars, NumPy, PyTorch); the lockfile pins the exact versions.

## 4. uv workflow

```bash
uv init --package --python 3.13 churn
uv add "pandas>=3,<4" scikit-learn pyarrow
uv add --dev pytest hypothesis ruff mypy pandas-stubs
uv lock                       # after any manual pyproject edit
uv sync --locked              # CI and fresh workspaces: fail if the lock is stale
uv run pytest
```

Commit `pyproject.toml` and `uv.lock` together in the same commit. A dependency change without its lockfile change is a broken build for the next person.

## 5. Configuration and paths

```python
# ❌ works on one laptop
df = pd.read_parquet("/Users/ana/Downloads/events.parquet")

# ✅ configurable, testable
class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_prefix="CHURN_")
    data_dir: Path = Path("data")
    seed: int = 42

def load_events(settings: Settings) -> pd.DataFrame:
    return pd.read_parquet(settings.data_dir / "events.parquet")
```

Tests pass `Settings(data_dir=tmp_path)` — no monkeypatching of globals. A new environment variable the deployed job reads is declared with declare_env_vars (rule deploy-runbook-fields).

## 6. What never goes in git

`.gitignore`: `data/`, `*.parquet`, `*.csv` outside `tests/fixtures/`, `mlruns/`, `*.ckpt`, `*.pt`, `*.safetensors`, `*.onnx`, `.ipynb_checkpoints/`, `.env`. Fixtures in `tests/` are small (kilobytes) and synthetic. Large data and models go through DVC or the registry (experiment-tracking-and-reproducibility).

## Common Mistakes

- Adding uv to a Poetry repository, or ruff config to a repo that runs flake8 and black, in a feature task.
- `pip install` into the workspace to make something run, leaving pyproject and lockfile unchanged.
- A flat layout where `tests/` imports a sibling module by path, so the test passes while the installed package is broken.
- GPU-only wheels pinned as the default dependency, so CI on a CPU runner cannot install.

## Red Flags

- A `requirements.txt` with no pins next to a `uv.lock` — two sources of truth.
- An absolute path or a username in source.
- A dataset or model binary staged for commit.
- `python -m pytest` passing only because the current directory is on `sys.path`.
