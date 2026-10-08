---
name: reproducible-runs
priority: 85
enabled: true
---
Every training run and every number you report can be reproduced: one seed from config feeds a single `np.random.default_rng(seed)` passed down and every `random_state` (no global `np.random.seed`), PyTorch runs seed and set deterministic flags when exactness matters, and the run logs its parameters, metrics, data version (DVC hash, snapshot date or query), git SHA and the lockfile the environment came from. Dependencies are pinned through the repository's lockfile; never `pip install` an unpinned package into the workspace to get a result. Pipeline stages are idempotent: re-running one overwrites its own output instead of appending. Load experiment-tracking-and-reproducibility.
