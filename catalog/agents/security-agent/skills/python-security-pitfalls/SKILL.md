---
name: python-security-pitfalls
category: security
description: Use when reviewing Python changes - Django, Flask and FastAPI handlers, subprocess, SQL, Jinja2, format strings, and data/ML code that loads pickles, models, notebooks or remote model code
tech_stack: Python & data
source: original; informed by trailofbits/skills (CC BY-SA 4.0, ideas only, own wording); Bandit rule names, Django/Flask/FastAPI, PyTorch and Hugging Face security documentation cited, not reproduced
---
# Python Security Pitfalls (services, data and ML)

## Overview

Python's dangerous calls are few and well known — `pickle`, `eval`, `shell=True`, f-string SQL — but data and ML code uses them as everyday tools: models are pickles, configs are YAML, notebooks evaluate whatever they are given. The question is always where the bytes come from.

## 1. Model and Data Loading (CWE-502) — the data/ML hot spot

Loading a pickle-based file runs code chosen by whoever wrote the file.

```python
# ❌ an uploaded or downloaded model file executes code on load
model = torch.load(path, weights_only=False)
clf = joblib.load(user_upload)
df = pd.read_pickle(s3_key_from_request)
arr = np.load(path, allow_pickle=True)
cfg = yaml.load(text, Loader=yaml.Loader)

# ✅
model.load_state_dict(safetensors.torch.load_file(path))
arr = np.load(path)                    # allow_pickle defaults to False
cfg = yaml.safe_load(text)
```

- `torch.load` defaults to `weights_only=True` from PyTorch 2.6; on an older pinned torch the plain call is unsafe.
- Hugging Face `from_pretrained(..., trust_remote_code=True)` runs Python from the model repository; with a user-chosen repo id it is remote code execution.
- Keras `load_model(..., safe_mode=False)` allows Lambda-layer code.
- Celery or Kombu with `accept_content=["pickle"]`, pickled values in a shared Redis or cache, `shelve`, `dill`, `cloudpickle` on data another party writes.
- Prefer safetensors, ONNX, skops, JSON, Parquet.

A finding when the file can come from outside the trust boundary: an upload, a URL, a user-chosen hub id, a shared bucket other tenants write. A model the team trains and ships in its own artifact store is not attacker input.

## 2. Code Evaluation

- `eval`, `exec`, `compile`, `__import__`/`importlib.import_module` with input.
- `DataFrame.query(expr)` / `pd.eval(expr)` with a user-built expression — evaluation, not filtering.
- **Format-string injection:** a user-supplied format string can walk attributes.

```python
# ❌ template = "{user.__class__.__init__.__globals__[SECRET_KEY]}"
message = template.format(user=user)
# ✅ user text is the value, never the format
message = "Hello {name}".format(name=user.name)
```

## 3. Commands (CWE-78)

```python
# ❌
os.system(f"zip -r {out} {folder}")
subprocess.run(f"git log {ref}", shell=True)
# ✅
subprocess.run(["git", "log", "--", ref], check=True)
```

`shlex.quote` is acceptable when a shell is truly required; `shell=True` with a constant string is fine.

## 4. SQL (CWE-89)

`cursor.execute(f"… {x}")`, `cursor.execute("… %s" % x)`, SQLAlchemy `text(f"…")`, Django `.raw(f"…")`, `.extra(where=[…])`, `RawSQL`. Safe: `cursor.execute("… %s", (x,))`, ORM filters, `text("… :x").bindparams(x=x)`.

## 5. Templates and XSS (CWE-79, CWE-1336)

- Flask `render_template_string(f"…{user}…")` → server-side template injection.
- `jinja2.Environment()` has **autoescape off by default** outside Flask/Django; rendering HTML with it is XSS unless `autoescape=select_autoescape()`.
- Django `mark_safe`, `|safe`, `{% autoescape off %}`, `format_html` misused with pre-joined strings.

## 6. Framework Specifics

**Django:** `@csrf_exempt` on a cookie-authenticated view; a production settings change to `DEBUG = True` or `ALLOWED_HOSTS = ["*"]` with host-built absolute URLs (reset-link poisoning); `SECRET_KEY` literal in production settings; DRF `permission_classes = [AllowAny]` or a serializer with `fields = "__all__"` on a writable endpoint.

**Flask:** `app.run(debug=True)` reachable beyond localhost — the Werkzeug debugger console executes code; `send_file(os.path.join(...))` with `<path:…>` routes (path-traversal-and-file-handling).

**FastAPI:** a new `APIRouter` included without the `dependencies=[Depends(current_user)]` its siblings carry; an endpoint returning the ORM object with no `response_model`, leaking every column; a Pydantic request model that includes `role`/`is_admin`.

## 7. Files, XML, Crypto, Transport

- `tarfile.extractall()` without `filter="data"` (default only on Python 3.14+); `os.path.join(base, user)` with absolute input.
- `lxml` parsing untrusted XML with entity resolution; prefer `defusedxml`.
- `random` for tokens → `secrets`; `hashlib.md5/sha256` for passwords → argon2/bcrypt.
- `requests.get(..., verify=False)`, `ssl._create_unverified_context()`.
- `tempfile.mktemp()` (race) → `mkstemp`/`NamedTemporaryFile`.
- `assert user.is_admin` as an access check — removed under `python -O` (CWE-617).

## 8. Notebooks and Pipelines

- Notebook injection is reported only with a concrete untrusted-input path (a papermill parameter from a request, a cell that evals fetched text).
- Committed notebook **outputs** that print tokens, connection strings or customer rows are leaked secrets/data — check outputs, not only code cells.
- Orchestrator templates (Airflow `params`/`dag_run.conf` in `bash_command`, dbt Jinja with run-time vars) → injection-and-dangerous-sinks.

## Common Mistakes

- Flagging `pickle.load` on an artifact the pipeline itself produced and stores privately.
- Flagging `subprocess.run([...])` lists as shell injection.
- Flagging Flask/Django templates that autoescape by default.
- Treating `.ipynb` exploration code as production without an input path.

## Red Flags

- `torch.load(` with `weights_only=False`, `trust_remote_code=True`, `allow_pickle=True`, `yaml.load(` without `SafeLoader`.
- `shell=True` with an f-string; `os.system(f"`.
- `.format(` or `%` on a string that came from a user or a database row.
- `render_template_string(` with interpolation.
- A model or dataset path or hub id taken from a request.

## References (names and links only)

[Bandit](https://bandit.readthedocs.io/) · [PyTorch torch.load security note](https://pytorch.org/docs/stable/generated/torch.load.html) · [Hugging Face security](https://huggingface.co/docs/hub/security) · [Django security](https://docs.djangoproject.com/en/stable/topics/security/) · CWE-78, 79, 89, 502, 617, 1336 · [OWASP Top 10:2025](https://owasp.org/Top10/2025/) A08
