"""Gather the English voice's files into OUT, as `models-voice-1` holds them.

Kokoro-82M v1.0 (Apache-2.0) as onnx-community exported it, at a pinned
revision: the 8-bit model, the tokenizer's vocabulary as kokoro.json, and the
voice styles in STYLES. misaki's US and British dictionaries (Apache-2.0),
from a checkout at a pinned commit, one gzipped JSON object per accent with
the silver entries gold already has left out.

Standard library only: python3 prepare_kokoro.py <misaki checkout> <out dir>
"""

import gzip
import hashlib
import json
import pathlib
import shutil
import sys
import urllib.request

REVISION = "1939ad2a8e416c0acfeecc08a694d14ef25f2231"
BASE = f"https://huggingface.co/onnx-community/Kokoro-82M-v1.0-ONNX/resolve/{REVISION}"
STYLES = ["af_heart", "af_bella", "af_nicole", "am_michael", "am_fenrir", "bf_emma", "bm_george"]


def fetch(path: str, dest: pathlib.Path) -> None:
    with urllib.request.urlopen(f"{BASE}/{path}") as res, dest.open("wb") as out:
        shutil.copyfileobj(res, out)


def main(misaki: pathlib.Path, out: pathlib.Path) -> None:
    out.mkdir(parents=True, exist_ok=True)
    fetch("onnx/model_quantized.onnx", out / "kokoro_v1_q8.onnx")
    for style in STYLES:
        fetch(f"voices/{style}.bin", out / f"kokoro_{style}.bin")

    tokenizer = out / "tokenizer.json"
    fetch("tokenizer.json", tokenizer)
    vocab = json.loads(tokenizer.read_text("utf-8"))["model"]["vocab"]
    tokenizer.unlink()
    (out / "kokoro.json").write_text(json.dumps({"vocab": vocab, "rate": 24000}, ensure_ascii=False, separators=(",", ":")), "utf-8")

    data = misaki / "misaki" / "data"
    for accent in ("us", "gb"):
        gold = json.loads((data / f"{accent}_gold.json").read_text("utf-8"))
        silver = json.loads((data / f"{accent}_silver.json").read_text("utf-8"))
        silver = {k: v for k, v in silver.items() if k not in gold}
        raw = json.dumps({"gold": gold, "silver": silver}, ensure_ascii=False, separators=(",", ":")).encode()
        # mtime 0, so the same dictionaries give the same bytes on every run.
        (out / f"misaki_{accent}.json.gz").write_bytes(gzip.compress(raw, 9, mtime=0))

    for f in sorted(out.iterdir()):
        print(f.name, hashlib.sha256(f.read_bytes()).hexdigest(), f.stat().st_size)


if __name__ == "__main__":
    main(pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]))
