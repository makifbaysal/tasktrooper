"""Make Supertonic 3's files for the voice, as `models-voice-1` holds them, in OUT.

Supertonic 3 (Supertone Inc., OpenRAIL-M) at a pinned revision of
Supertone/supertonic-3, each download checked against the SHA-256 the hub
publishes for it. The four ONNX graphs are quantized here, weight-only: every
Conv, MatMul and Gemm weight and every embedding table of 4096 values or more
becomes int8 with one float32 scale per output channel (per row for an
embedding), dequantized by a DequantizeLinear node in front of its user.
Activations stay float32, so onnxruntime-web's wasm backend runs the same
float kernels as for the original: a quarter of the size at the original's
speed, where onnxruntime's dynamic quantization (ConvInteger, MatMulInteger)
runs half again as slow on that backend.

The OpenRAIL-M license asks that a changed file carry a notice that it was
changed, so each quantized graph says so in its metadata. tts.json,
unicode_indexer.json, the voice styles and LICENSE are copied unchanged.

    uv venv -p 3.12 venv && VIRTUAL_ENV=venv uv pip install onnx==1.23.2 numpy==2.5.4
    venv/bin/python prepare_supertonic.py out    # prints each file's SHA-256 and size
"""

import hashlib
import pathlib
import shutil
import sys
import urllib.request

import numpy as np
import onnx
from onnx import TensorProto, helper, numpy_helper

REVISION = "3cadd1ee6394adea1bd021217a0e650ede09a323"
BASE = f"https://huggingface.co/Supertone/supertonic-3/resolve/{REVISION}"
GRAPHS = {
    "duration_predictor": "c3eb91414d5ff8a7a239b7fe9e34e7e2bf8a8140d8375ffb14718b1c639325db",
    "text_encoder": "c7befd5ea8c3119769e8a6c1486c4edc6a3bc8365c67621c881bbb774b9902ff",
    "vector_estimator": "883ac868ea0275ef0e991524dc64f16b3c0376efd7c320af6b53f5b780d7c61c",
    "vocoder": "085de76dd8e8d5836d6ca66826601f615939218f90e519f70ee8a36ed2a4c4ba",
}
STYLES = ["F1", "F2", "F3", "F4", "F5", "M1", "M2", "M3", "M4", "M5"]
MIN_WEIGHTS = 4096
NOTICE = (
    "Modified by TaskTrooper from Supertone/supertonic-3 (revision "
    + REVISION[:8]
    + "): weights of 4096 values or more quantized to int8 per output channel, "
    "dequantized by DequantizeLinear nodes. Licensed under OpenRAIL-M, see LICENSE-supertonic.txt."
)


def fetch(path: str, dest: pathlib.Path, sha256: str | None = None) -> None:
    with urllib.request.urlopen(f"{BASE}/{path}") as res, dest.open("wb") as out:
        shutil.copyfileobj(res, out)
    if sha256 is not None:
        actual = hashlib.sha256(dest.read_bytes()).hexdigest()
        if actual != sha256:
            raise SystemExit(f"{path}: SHA-256 {actual}, expected {sha256}")


def weight_axis(graph: onnx.GraphProto, name: str, ndim: int) -> int | None:
    """The output-channel axis of a weight only Conv/MatMul/Gemm/Gather read, or None to leave it float."""
    users = [(node, i) for node in graph.node for i, inp in enumerate(node.input) if inp == name]
    if not users:
        return None
    kinds = {(node.op_type, i) for node, i in users}
    if kinds <= {("Conv", 1)}:
        return 0
    if kinds <= {("MatMul", 1)} and ndim == 2:
        return 1
    if kinds <= {("Gemm", 1)} and ndim == 2:
        trans_b = [a.i for node, _ in users for a in node.attribute if a.name == "transB"]
        if len(set(trans_b)) > 1:
            return None
        return 0 if trans_b and trans_b[0] == 1 else 1
    if kinds <= {("Gather", 0)} and ndim == 2:
        return 0
    return None


def quantize(src: pathlib.Path, dest: pathlib.Path) -> int:
    model = onnx.load(str(src))
    graph = model.graph
    dequantize = []
    for init in list(graph.initializer):
        if init.data_type != TensorProto.FLOAT:
            continue
        w = numpy_helper.to_array(init)
        if w.size < MIN_WEIGHTS:
            continue
        axis = weight_axis(graph, init.name, w.ndim)
        if axis is None:
            continue
        others = tuple(i for i in range(w.ndim) if i != axis)
        amax = np.abs(w).max(axis=others)
        scale = np.where(amax > 0, amax / 127.0, 1.0).astype(np.float32)
        shape = [1] * w.ndim
        shape[axis] = -1
        q = np.clip(np.round(w / scale.reshape(shape)), -127, 127).astype(np.int8)
        graph.initializer.remove(init)
        graph.initializer.extend([numpy_helper.from_array(q, f"{init.name}_q"), numpy_helper.from_array(scale, f"{init.name}_s")])
        dequantize.append(helper.make_node("DequantizeLinear", [f"{init.name}_q", f"{init.name}_s"], [init.name], axis=axis, name=f"{init.name}_dq"))
    nodes = list(graph.node)
    del graph.node[:]
    graph.node.extend(dequantize + nodes)
    model.doc_string = NOTICE
    del model.metadata_props[:]
    model.metadata_props.add(key="modified_by", value=NOTICE)
    onnx.checker.check_model(model)
    onnx.save(model, str(dest))
    return len(dequantize)


def main(out: pathlib.Path) -> None:
    out.mkdir(parents=True, exist_ok=True)
    work = out / ".original"
    work.mkdir(exist_ok=True)
    for name, sha256 in GRAPHS.items():
        original = work / f"{name}.onnx"
        fetch(f"onnx/{name}.onnx", original, sha256)
        n = quantize(original, out / f"supertonic_{name}.onnx")
        print(f"{name}: {n} weights quantized", file=sys.stderr)
    shutil.rmtree(work)
    fetch("onnx/tts.json", out / "supertonic_tts.json")
    fetch("onnx/unicode_indexer.json", out / "supertonic_unicode_indexer.json")
    for style in STYLES:
        fetch(f"voice_styles/{style}.json", out / f"supertonic_{style}.json")
    fetch("LICENSE", out / "LICENSE-supertonic.txt")

    for f in sorted(out.iterdir()):
        if f.name.startswith("supertonic_") or f.name == "LICENSE-supertonic.txt":
            print(f.name, hashlib.sha256(f.read_bytes()).hexdigest(), f.stat().st_size)


if __name__ == "__main__":
    main(pathlib.Path(sys.argv[1]))
